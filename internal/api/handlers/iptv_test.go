package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/auth"
	"hubplay/internal/db"
	"hubplay/internal/iptv"
	"hubplay/internal/testutil"
)

// ─── Fake IPTVService ───────────────────────────────────────────────────────

type iptvFakeService struct {
	mu sync.Mutex

	channels     map[string][]*db.Channel      // by libraryID
	channelByID  map[string]*db.Channel        // by channelID
	groups       map[string][]string           // by libraryID
	nowPlayingFn func(ctx context.Context, channelID string) (*db.EPGProgram, error)
	scheduleFn   func(ctx context.Context, channelID string, from, to time.Time) ([]*db.EPGProgram, error)
	bulkFn       func(ctx context.Context, ids []string, from, to time.Time) (map[string][]*db.EPGProgram, error)

	refreshM3UCount int
	refreshEPGCount int
	refreshM3UErr   error
	refreshEPGErr   error
	refreshM3UCalls []string

	// Tracks the per-library refresh slot exposed by the new
	// TryAcquireRefresh / RunRefreshM3U / PublishRefreshFailed
	// surface. busyLibraries is the set of libraries whose slot is
	// currently held; runRefreshCalls / publishFailedCalls capture
	// async invocations so tests can wait on them deterministically.
	busyLibraries       map[string]bool
	tryAcquireErr       error
	runRefreshM3UErr    error
	runRefreshM3UCount  int
	runRefreshM3UCalls  []string
	publishFailedCalls  []struct{ LibraryID, Error string }
	runRefreshM3UDoneCh chan string

	// Per-user channel-favorites set, keyed by userID → set of channelIDs.
	// Nil until the first write; methods lazily initialize.
	favoritesByUser map[string]map[string]struct{}

	// EPG source fixtures, keyed by libraryID.
	epgSources map[string][]*db.LibraryEPGSource
	epgCatalog []iptv.PublicEPGSource

	// Channel health fixtures, keyed by libraryID. Tests append Channel
	// pointers with the health fields set; ListUnhealthyChannels filters
	// on consecutive_failures >= threshold.
	unhealthyByLibrary map[string][]*db.Channel
	resetHealthCalls   []string
	setActiveCalls     []struct {
		ID     string
		Active bool
	}

	// Channels-without-EPG fixtures + edit capture.
	withoutEPGByLibrary map[string][]*db.Channel
	tvgIDEdits          []struct {
		ChannelID string
		TvgID     string
	}

	// Watch-history fixtures + capture.
	watchedByUser    map[string][]*db.Channel
	recordWatchCalls []struct {
		UserID    string
		ChannelID string
	}
	recordWatchErr error

	// Playback-failure beacon capture.
	recordProbeFailureCalls []struct {
		ChannelID string
		Err       error
	}
}

func newIPTVFakeService() *iptvFakeService {
	return &iptvFakeService{
		channels:    map[string][]*db.Channel{},
		channelByID: map[string]*db.Channel{},
		groups:      map[string][]string{},
	}
}

func (s *iptvFakeService) GetChannels(_ context.Context, libraryID string, activeOnly bool) ([]*db.Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*db.Channel{}
	for _, ch := range s.channels[libraryID] {
		if activeOnly && !ch.IsActive {
			continue
		}
		out = append(out, ch)
	}
	return out, nil
}

func (s *iptvFakeService) GetChannel(_ context.Context, id string) (*db.Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch, ok := s.channelByID[id]; ok {
		return ch, nil
	}
	return nil, errors.New("channel not found")
}

func (s *iptvFakeService) GetGroups(_ context.Context, libraryID string) ([]string, error) {
	return s.groups[libraryID], nil
}

func (s *iptvFakeService) GetSchedule(ctx context.Context, channelID string, from, to time.Time) ([]*db.EPGProgram, error) {
	if s.scheduleFn != nil {
		return s.scheduleFn(ctx, channelID, from, to)
	}
	return nil, nil
}

func (s *iptvFakeService) GetBulkSchedule(ctx context.Context, ids []string, from, to time.Time) (map[string][]*db.EPGProgram, error) {
	if s.bulkFn != nil {
		return s.bulkFn(ctx, ids, from, to)
	}
	return map[string][]*db.EPGProgram{}, nil
}

func (s *iptvFakeService) NowPlaying(ctx context.Context, channelID string) (*db.EPGProgram, error) {
	if s.nowPlayingFn != nil {
		return s.nowPlayingFn(ctx, channelID)
	}
	return nil, nil
}

func (s *iptvFakeService) RefreshM3U(_ context.Context, libraryID string) (int, error) {
	s.mu.Lock()
	s.refreshM3UCalls = append(s.refreshM3UCalls, libraryID)
	err := s.refreshM3UErr
	n := s.refreshM3UCount
	s.mu.Unlock()
	return n, err
}

func (s *iptvFakeService) TryAcquireRefresh(libraryID string) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tryAcquireErr != nil {
		return nil, s.tryAcquireErr
	}
	if s.busyLibraries == nil {
		s.busyLibraries = map[string]bool{}
	}
	if s.busyLibraries[libraryID] {
		return nil, fmt.Errorf("library %s: %w", libraryID, iptv.ErrRefreshInProgress)
	}
	s.busyLibraries[libraryID] = true
	return func() {
		s.mu.Lock()
		delete(s.busyLibraries, libraryID)
		s.mu.Unlock()
	}, nil
}

func (s *iptvFakeService) RunRefreshM3U(_ context.Context, libraryID string) (int, error) {
	s.mu.Lock()
	s.runRefreshM3UCalls = append(s.runRefreshM3UCalls, libraryID)
	err := s.runRefreshM3UErr
	n := s.runRefreshM3UCount
	doneCh := s.runRefreshM3UDoneCh
	s.mu.Unlock()
	if doneCh != nil {
		doneCh <- libraryID
	}
	return n, err
}

func (s *iptvFakeService) PublishRefreshFailed(libraryID string, err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	s.mu.Lock()
	s.publishFailedCalls = append(s.publishFailedCalls, struct{ LibraryID, Error string }{libraryID, msg})
	s.mu.Unlock()
}

func (s *iptvFakeService) RefreshEPG(_ context.Context, _ string) (int, error) {
	return s.refreshEPGCount, s.refreshEPGErr
}

func (s *iptvFakeService) PreflightCheck(_ context.Context, m3uURL string, _ bool) iptv.PreflightResult {
	return iptv.PreflightResult{Status: iptv.PreflightOK, Message: "fake ok", BodyHint: m3uURL}
}

// ─── Favorites (fake) ───────────────────────────────────────────────────────
//
// Minimal in-memory map keyed by user: covers the handler contract without
// needing a DB. Tests that exercise favorites populate `favoritesByUser`
// directly or call the Add/Remove methods.

func (s *iptvFakeService) AddFavorite(_ context.Context, userID, channelID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.favoritesByUser == nil {
		s.favoritesByUser = map[string]map[string]struct{}{}
	}
	set, ok := s.favoritesByUser[userID]
	if !ok {
		set = map[string]struct{}{}
		s.favoritesByUser[userID] = set
	}
	set[channelID] = struct{}{}
	return nil
}

func (s *iptvFakeService) RemoveFavorite(_ context.Context, userID, channelID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if set, ok := s.favoritesByUser[userID]; ok {
		delete(set, channelID)
	}
	return nil
}

func (s *iptvFakeService) IsFavorite(_ context.Context, userID, channelID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.favoritesByUser[userID][channelID]
	return ok, nil
}

func (s *iptvFakeService) ListFavoriteIDs(_ context.Context, userID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.favoritesByUser[userID]))
	for id := range s.favoritesByUser[userID] {
		out = append(out, id)
	}
	return out, nil
}

func (s *iptvFakeService) ListFavoriteChannels(_ context.Context, userID string) ([]*db.Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*db.Channel{}
	for id := range s.favoritesByUser[userID] {
		if ch, ok := s.channelByID[id]; ok && ch.IsActive {
			out = append(out, ch)
		}
	}
	return out, nil
}

// ─── EPG catalog & sources (fake) ───────────────────────────────────────────

func (s *iptvFakeService) PublicEPGCatalog() []iptv.PublicEPGSource {
	if s.epgCatalog != nil {
		return s.epgCatalog
	}
	return []iptv.PublicEPGSource{
		{ID: "davidmuma-guiatv", Name: "davidmuma", Language: "es", URL: "http://example/guiatv.xml"},
	}
}

func (s *iptvFakeService) ListEPGSources(_ context.Context, libraryID string) ([]*db.LibraryEPGSource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*db.LibraryEPGSource, 0, len(s.epgSources[libraryID]))
	out = append(out, s.epgSources[libraryID]...)
	return out, nil
}

func (s *iptvFakeService) AddEPGSource(_ context.Context, libraryID, catalogID, customURL string) (*db.LibraryEPGSource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if catalogID == "" && customURL == "" {
		return nil, errors.New("either catalog_id or url required")
	}
	src := &db.LibraryEPGSource{
		ID:        "src-" + fmt.Sprintf("%d", len(s.epgSources[libraryID])+1),
		LibraryID: libraryID,
		CatalogID: catalogID,
		URL:       customURL,
		Priority:  len(s.epgSources[libraryID]),
	}
	if catalogID != "" {
		src.URL = "http://catalog/" + catalogID + ".xml"
	}
	if s.epgSources == nil {
		s.epgSources = map[string][]*db.LibraryEPGSource{}
	}
	// Mirror the repo's UNIQUE(library_id, url) so handler-level
	// tests can exercise the 409 mapping without a real DB.
	for _, existing := range s.epgSources[libraryID] {
		if existing.URL == src.URL {
			return nil, db.ErrEPGSourceAlreadyAttached
		}
	}
	s.epgSources[libraryID] = append(s.epgSources[libraryID], src)
	return src, nil
}

func (s *iptvFakeService) RemoveEPGSource(_ context.Context, libraryID, sourceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.epgSources[libraryID]
	for i, src := range list {
		if src.ID == sourceID {
			s.epgSources[libraryID] = append(list[:i], list[i+1:]...)
			return nil
		}
	}
	return errors.New("not found")
}

// ─── Channel health (fake) ──────────────────────────────────────────────────

func (s *iptvFakeService) ListUnhealthyChannels(_ context.Context, libraryID string, threshold int) ([]*db.Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if threshold <= 0 {
		threshold = db.UnhealthyThreshold
	}
	out := []*db.Channel{}
	for _, ch := range s.unhealthyByLibrary[libraryID] {
		if ch.ConsecutiveFailures >= threshold {
			out = append(out, ch)
		}
	}
	return out, nil
}

func (s *iptvFakeService) SetChannelActive(_ context.Context, id string, active bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setActiveCalls = append(s.setActiveCalls, struct {
		ID     string
		Active bool
	}{id, active})
	if ch, ok := s.channelByID[id]; ok {
		ch.IsActive = active
	}
	return nil
}

func (s *iptvFakeService) ListChannelsWithoutEPG(_ context.Context, libraryID string) ([]*db.Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*db.Channel{}
	out = append(out, s.withoutEPGByLibrary[libraryID]...)
	return out, nil
}

func (s *iptvFakeService) SetChannelTvgID(_ context.Context, channelID, tvgID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tvgIDEdits = append(s.tvgIDEdits, struct {
		ChannelID string
		TvgID     string
	}{channelID, tvgID})
	if ch, ok := s.channelByID[channelID]; ok {
		ch.TvgID = tvgID
	}
	return nil
}

func (s *iptvFakeService) ResetChannelHealth(_ context.Context, channelID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resetHealthCalls = append(s.resetHealthCalls, channelID)
	return nil
}

// RecordProbeFailure captures synthetic failures forwarded by the
// playback-failure beacon and lets tests drive the consecutive-
// failures counter on the fake channel row.
func (s *iptvFakeService) RecordProbeFailure(_ context.Context, channelID string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordProbeFailureCalls = append(s.recordProbeFailureCalls, struct {
		ChannelID string
		Err       error
	}{channelID, err})
	if ch, ok := s.channelByID[channelID]; ok {
		ch.ConsecutiveFailures++
	}
}

func (s *iptvFakeService) RecordWatch(_ context.Context, userID, channelID string) (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.recordWatchErr != nil {
		return time.Time{}, s.recordWatchErr
	}
	s.recordWatchCalls = append(s.recordWatchCalls, struct {
		UserID    string
		ChannelID string
	}{userID, channelID})
	if s.watchedByUser == nil {
		s.watchedByUser = map[string][]*db.Channel{}
	}
	ch, ok := s.channelByID[channelID]
	if !ok {
		return time.Time{}, errors.New("channel not found")
	}
	// Put the freshly-watched channel at the head of the user's list.
	existing := s.watchedByUser[userID]
	filtered := make([]*db.Channel, 0, len(existing)+1)
	filtered = append(filtered, ch)
	for _, c := range existing {
		if c.ID != channelID {
			filtered = append(filtered, c)
		}
	}
	s.watchedByUser[userID] = filtered
	ts := time.Now().UTC()
	return ts, nil
}

func (s *iptvFakeService) ListContinueWatching(_ context.Context, userID string, limit int, accessible map[string]bool) ([]*db.Channel, []time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := s.watchedByUser[userID]
	var channels []*db.Channel
	var watched []time.Time
	for _, ch := range entries {
		if accessible != nil && !accessible[ch.LibraryID] {
			continue
		}
		channels = append(channels, ch)
		watched = append(watched, time.Now().UTC())
		if len(channels) >= limit {
			break
		}
	}
	return channels, watched, nil
}

func (s *iptvFakeService) ReorderEPGSources(_ context.Context, libraryID string, orderedIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.epgSources[libraryID]
	if len(orderedIDs) != len(list) {
		return errors.New("mismatched id list")
	}
	byID := map[string]*db.LibraryEPGSource{}
	for _, src := range list {
		byID[src.ID] = src
	}
	out := make([]*db.LibraryEPGSource, 0, len(orderedIDs))
	for i, id := range orderedIDs {
		src, ok := byID[id]
		if !ok {
			return errors.New("unknown id")
		}
		src.Priority = i
		out = append(out, src)
	}
	s.epgSources[libraryID] = out
	return nil
}

// ─── Fake proxy ─────────────────────────────────────────────────────────────

type iptvFakeProxy struct {
	streamFn func(w http.ResponseWriter, channelID, streamURL string) error
	urlFn    func(w http.ResponseWriter, channelID, url string) error
}

func (p *iptvFakeProxy) ProxyStream(_ context.Context, w http.ResponseWriter, channelID, streamURL string) error {
	if p.streamFn != nil {
		return p.streamFn(w, channelID, streamURL)
	}
	_, _ = w.Write([]byte("STREAM_BYTES"))
	return nil
}

func (p *iptvFakeProxy) ProxyURL(_ context.Context, w http.ResponseWriter, channelID, url string) error {
	if p.urlFn != nil {
		return p.urlFn(w, channelID, url)
	}
	_, _ = w.Write([]byte("URL_BYTES"))
	return nil
}

// ─── Fake LibraryRepository ────────────────────────────────────────────────
//
// Implements Create (unchanged) plus ListForUser for the handlers
// that need to materialise the per-user library-access set (e.g.
// continue-watching filter).

type iptvFakeLibraryRepo struct {
	created       []*db.Library
	createErr     error
	librariesByID map[string]*db.Library
	// userAccess: if nil the ListForUser fallback returns every
	// library in librariesByID (admin behaviour). A populated map
	// keys a userID to the set of library IDs they can see.
	userAccess map[string]map[string]bool
	listErr    error
}

func (r *iptvFakeLibraryRepo) Create(_ context.Context, lib *db.Library) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.created = append(r.created, lib)
	if r.librariesByID == nil {
		r.librariesByID = map[string]*db.Library{}
	}
	r.librariesByID[lib.ID] = lib
	return nil
}

func (r *iptvFakeLibraryRepo) ListForUser(_ context.Context, userID string) ([]*db.Library, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	if r.userAccess == nil {
		out := make([]*db.Library, 0, len(r.librariesByID))
		for _, lib := range r.librariesByID {
			out = append(out, lib)
		}
		return out, nil
	}
	set := r.userAccess[userID]
	out := make([]*db.Library, 0, len(set))
	for id := range set {
		if lib, ok := r.librariesByID[id]; ok {
			out = append(out, lib)
		}
	}
	return out, nil
}

// iptvFakeAccess is the minimal LibraryAccessService fake used by the tests.
// By default grants access to every library/user combination; tests that need
// to exercise the deny path set a custom function via accessFn.
type iptvFakeAccess struct {
	accessFn func(userID, libraryID string) (bool, error)
}

func (a *iptvFakeAccess) UserHasAccess(_ context.Context, userID, libraryID string) (bool, error) {
	if a.accessFn != nil {
		return a.accessFn(userID, libraryID)
	}
	return true, nil
}

// Compile-time checks.
var (
	_ IPTVService            = (*iptvFakeService)(nil)
	_ IPTVStreamProxyService = (*iptvFakeProxy)(nil)
	_ LibraryRepository      = (*iptvFakeLibraryRepo)(nil)
	_ LibraryAccessService   = (*iptvFakeAccess)(nil)
)

// ─── Env + helpers ──────────────────────────────────────────────────────────

type iptvTestEnv struct {
	t         *testing.T
	svc       *iptvFakeService
	proxy     *iptvFakeProxy
	libraries *iptvFakeLibraryRepo
	access    *iptvFakeAccess
	handler   *IPTVHandler
	router    chi.Router
}

func newIPTVTestEnv(t *testing.T) *iptvTestEnv {
	t.Helper()
	env := &iptvTestEnv{
		t:         t,
		svc:       newIPTVFakeService(),
		proxy:     &iptvFakeProxy{},
		libraries: &iptvFakeLibraryRepo{},
		access:    &iptvFakeAccess{},
	}
	env.handler = NewIPTVHandler(env.svc, env.proxy, env.libraries, env.access, testutil.NopLogger())

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/libraries/{id}/channels", env.handler.ListChannels)
		r.Get("/libraries/{id}/channels/groups", env.handler.Groups)
		r.Post("/libraries/{id}/iptv/refresh-m3u", env.handler.RefreshM3U)
		r.Post("/libraries/{id}/iptv/refresh-epg", env.handler.RefreshEPG)
		r.Get("/channels/{channelId}", env.handler.GetChannel)
		r.Get("/channels/{channelId}/stream", env.handler.Stream)
		r.Get("/channels/{channelId}/proxy", env.handler.ProxyURL)
		r.Get("/channels/{channelId}/schedule", env.handler.Schedule)
		r.Get("/iptv/schedule", env.handler.BulkSchedule)
		r.Post("/iptv/schedule", env.handler.BulkSchedule)
		r.Get("/iptv/public/countries", env.handler.PublicCountries)
		r.Post("/iptv/public/import", env.handler.ImportPublicIPTV)
		r.Get("/iptv/epg-catalog", env.handler.EPGCatalog)
		r.Get("/libraries/{id}/epg-sources", env.handler.ListEPGSources)
		r.Post("/libraries/{id}/epg-sources", env.handler.AddEPGSource)
		r.Delete("/libraries/{id}/epg-sources/{sourceId}", env.handler.RemoveEPGSource)
		r.Patch("/libraries/{id}/epg-sources/reorder", env.handler.ReorderEPGSources)
		r.Get("/libraries/{id}/channels/unhealthy", env.handler.ListUnhealthyChannels)
		r.Post("/channels/{channelId}/reset-health", env.handler.ResetChannelHealth)
		r.Post("/channels/{channelId}/disable", env.handler.DisableChannel)
		r.Post("/channels/{channelId}/enable", env.handler.EnableChannel)
		r.Get("/libraries/{id}/channels/without-epg", env.handler.ListChannelsWithoutEPG)
		r.Patch("/channels/{channelId}", env.handler.PatchChannel)
		r.Post("/channels/{channelId}/watch", env.handler.RecordChannelWatch)
		r.Post("/channels/{channelId}/playback-failure", env.handler.RecordPlaybackFailure)
		r.Get("/me/channels/continue-watching", env.handler.ListContinueWatching)
	})
	env.router = r
	return env
}

func (e *iptvTestEnv) do(method, path, body string) *httptest.ResponseRecorder {
	return e.doAs(method, path, body, &auth.Claims{UserID: "u-admin", Role: "admin"})
}

// doAs issues a request with the given claims on the context. Pass nil for
// unauthenticated; pass a non-admin Claims to exercise the per-library ACL.
func (e *iptvTestEnv) doAs(method, path, body string, claims *auth.Claims) *httptest.ResponseRecorder {
	e.t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if claims != nil {
		req = req.WithContext(auth.WithClaims(req.Context(), claims))
	}
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

func iptvDecodeData(t *testing.T, rr *httptest.ResponseRecorder) any {
	t.Helper()
	var out map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out["data"]
}

// ─── ListChannels ───────────────────────────────────────────────────────────

func TestIPTVHandler_ListChannels_Empty(t *testing.T) {
	env := newIPTVTestEnv(t)
	rr := env.do(http.MethodGet, "/api/v1/libraries/lib-1/channels", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	data, _ := iptvDecodeData(t, rr).([]any)
	if len(data) != 0 {
		t.Fatalf("expected empty, got %d", len(data))
	}
}

func TestIPTVHandler_ListChannels_ActiveOnlyDefault(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.channels["lib-1"] = []*db.Channel{
		{ID: "c-1", LibraryID: "lib-1", Name: "Active", IsActive: true},
		{ID: "c-2", LibraryID: "lib-1", Name: "Inactive", IsActive: false},
	}
	rr := env.do(http.MethodGet, "/api/v1/libraries/lib-1/channels", "")
	data, _ := iptvDecodeData(t, rr).([]any)
	if len(data) != 1 {
		t.Fatalf("default active-only: got %d want 1", len(data))
	}
}

func TestIPTVHandler_ListChannels_IncludeInactive(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.channels["lib-1"] = []*db.Channel{
		{ID: "c-1", LibraryID: "lib-1", IsActive: true},
		{ID: "c-2", LibraryID: "lib-1", IsActive: false},
	}
	rr := env.do(http.MethodGet, "/api/v1/libraries/lib-1/channels?active=false", "")
	data, _ := iptvDecodeData(t, rr).([]any)
	if len(data) != 2 {
		t.Fatalf("active=false: got %d want 2", len(data))
	}
}

func TestIPTVHandler_ListChannels_DerivesCategoryAndLogoFallback(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.channels["lib-1"] = []*db.Channel{
		{ID: "c-1", LibraryID: "lib-1", Name: "Real Madrid TV", GroupName: "Deportes HD", IsActive: true},
	}
	rr := env.do(http.MethodGet, "/api/v1/libraries/lib-1/channels", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	list, _ := iptvDecodeData(t, rr).([]any)
	if len(list) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(list))
	}
	first, _ := list[0].(map[string]any)

	// Canonical category derived from the raw group-title.
	if first["category"] != "sports" {
		t.Errorf("category: got %v want sports", first["category"])
	}
	// Raw group-title preserved for operators / legacy clients.
	if first["group_name"] != "Deportes HD" {
		t.Errorf("group_name: got %v want Deportes HD", first["group_name"])
	}
	// Logo fallback is always populated, even without an upstream logo_url.
	if first["logo_initials"] != "RM" {
		t.Errorf("logo_initials: got %v want RM", first["logo_initials"])
	}
	if bg, _ := first["logo_bg"].(string); len(bg) != 7 || bg[0] != '#' {
		t.Errorf("logo_bg: got %v want #RRGGBB", first["logo_bg"])
	}
	if fg, _ := first["logo_fg"].(string); fg != "#ffffff" && fg != "#0a0d0b" {
		t.Errorf("logo_fg: got %v want light or dark sentinel", first["logo_fg"])
	}
}

// ─── GetChannel ─────────────────────────────────────────────────────────────

func TestIPTVHandler_GetChannel_WithNowPlaying(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.channelByID["c-1"] = &db.Channel{ID: "c-1", Name: "Foo", IsActive: true}
	env.svc.nowPlayingFn = func(_ context.Context, _ string) (*db.EPGProgram, error) {
		return &db.EPGProgram{Title: "On Now", Category: "News"}, nil
	}
	rr := env.do(http.MethodGet, "/api/v1/channels/c-1", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	data, _ := iptvDecodeData(t, rr).(map[string]any)
	if data["id"] != "c-1" {
		t.Errorf("id: %v", data["id"])
	}
	np, _ := data["now_playing"].(map[string]any)
	if np == nil || np["title"] != "On Now" {
		t.Errorf("now_playing shape: %v", data["now_playing"])
	}
}

func TestIPTVHandler_GetChannel_NotFound_500(t *testing.T) {
	env := newIPTVTestEnv(t)
	// Service returns a bare error, not AppError → handleServiceError maps to 500.
	rr := env.do(http.MethodGet, "/api/v1/channels/missing", "")
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d want 500", rr.Code)
	}
}

// ─── Groups ─────────────────────────────────────────────────────────────────

func TestIPTVHandler_Groups_ReturnsList(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.groups["lib-1"] = []string{"News", "Sports", "Movies"}
	rr := env.do(http.MethodGet, "/api/v1/libraries/lib-1/channels/groups", "")
	data, _ := iptvDecodeData(t, rr).([]any)
	if len(data) != 3 {
		t.Fatalf("groups: got %d want 3", len(data))
	}
}

// ─── Stream ─────────────────────────────────────────────────────────────────

func TestIPTVHandler_Stream_ProxiesWhenActive(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.channelByID["c-1"] = &db.Channel{ID: "c-1", IsActive: true, StreamURL: "http://live/x"}
	var gotURL string
	env.proxy.streamFn = func(w http.ResponseWriter, _, streamURL string) error {
		gotURL = streamURL
		_, _ = w.Write([]byte("TS-DATA"))
		return nil
	}
	rr := env.do(http.MethodGet, "/api/v1/channels/c-1/stream", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	if rr.Body.String() != "TS-DATA" {
		t.Errorf("body: %q", rr.Body.String())
	}
	if gotURL != "http://live/x" {
		t.Errorf("streamURL passed: %q", gotURL)
	}
}

func TestIPTVHandler_Stream_InactiveChannel_404(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.channelByID["c-1"] = &db.Channel{ID: "c-1", IsActive: false}
	rr := env.do(http.MethodGet, "/api/v1/channels/c-1/stream", "")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: got %d want 404", rr.Code)
	}
}

// ─── ProxyURL ───────────────────────────────────────────────────────────────

func TestIPTVHandler_ProxyURL_HappyPath(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.channelByID["c-1"] = &db.Channel{ID: "c-1", LibraryID: "lib-1", IsActive: true}
	var gotURL string
	env.proxy.urlFn = func(w http.ResponseWriter, _, url string) error {
		gotURL = url
		_, _ = w.Write([]byte("SEGMENT"))
		return nil
	}
	rr := env.do(http.MethodGet, "/api/v1/channels/c-1/proxy?url=https://cdn/seg.ts", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	if gotURL != "https://cdn/seg.ts" {
		t.Errorf("url passed: %q", gotURL)
	}
}

func TestIPTVHandler_ProxyURL_MissingURL_400(t *testing.T) {
	env := newIPTVTestEnv(t)
	rr := env.do(http.MethodGet, "/api/v1/channels/c-1/proxy", "")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rr.Code)
	}
}

// ─── Schedule ───────────────────────────────────────────────────────────────

func TestIPTVHandler_Schedule_ShapesPrograms(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.channelByID["c-1"] = &db.Channel{ID: "c-1", LibraryID: "lib-1"}
	env.svc.scheduleFn = func(_ context.Context, _ string, _, _ time.Time) ([]*db.EPGProgram, error) {
		return []*db.EPGProgram{
			{ID: "p-1", Title: "Show A", Category: "Drama"},
			{ID: "p-2", Title: "Show B"},
		}, nil
	}
	rr := env.do(http.MethodGet, "/api/v1/channels/c-1/schedule", "")
	data, _ := iptvDecodeData(t, rr).([]any)
	if len(data) != 2 {
		t.Fatalf("schedule length: %d", len(data))
	}
}

func TestIPTVHandler_Schedule_ParsesTimeRangeHours(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.channelByID["c-1"] = &db.Channel{ID: "c-1", LibraryID: "lib-1"}
	var gotFrom, gotTo time.Time
	env.svc.scheduleFn = func(_ context.Context, _ string, from, to time.Time) ([]*db.EPGProgram, error) {
		gotFrom, gotTo = from, to
		return nil, nil
	}
	// Handler interprets integer "from" as "hours ago" and "to" as "hours ahead"
	// (see parseTimeRange in iptv.go). from=6&to=12 → window spans ~18 hours.
	_ = env.do(http.MethodGet, "/api/v1/channels/c-1/schedule?from=6&to=12", "")
	span := gotTo.Sub(gotFrom)
	if span < 17*time.Hour || span > 19*time.Hour {
		t.Errorf("span out of expected range: %v", span)
	}
}

// ─── BulkSchedule ───────────────────────────────────────────────────────────

func TestIPTVHandler_BulkSchedule_HappyPath(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.channelByID["c-1"] = &db.Channel{ID: "c-1", LibraryID: "lib-1"}
	env.svc.channelByID["c-2"] = &db.Channel{ID: "c-2", LibraryID: "lib-1"}
	env.svc.bulkFn = func(_ context.Context, ids []string, _, _ time.Time) (map[string][]*db.EPGProgram, error) {
		out := map[string][]*db.EPGProgram{}
		for _, id := range ids {
			out[id] = []*db.EPGProgram{{ID: "p-" + id, Title: "T-" + id}}
		}
		return out, nil
	}
	rr := env.do(http.MethodGet, "/api/v1/iptv/schedule?channels=c-1,c-2", "")
	data, _ := iptvDecodeData(t, rr).(map[string]any)
	if len(data) != 2 || data["c-1"] == nil || data["c-2"] == nil {
		t.Fatalf("bulk shape: %v", data)
	}
}

func TestIPTVHandler_BulkSchedule_MissingChannels_400(t *testing.T) {
	env := newIPTVTestEnv(t)
	rr := env.do(http.MethodGet, "/api/v1/iptv/schedule", "")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rr.Code)
	}
}

func TestIPTVHandler_BulkSchedule_POST_HappyPath(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.channelByID["c-1"] = &db.Channel{ID: "c-1", LibraryID: "lib-1"}
	env.svc.channelByID["c-2"] = &db.Channel{ID: "c-2", LibraryID: "lib-1"}
	env.svc.bulkFn = func(_ context.Context, ids []string, _, _ time.Time) (map[string][]*db.EPGProgram, error) {
		out := map[string][]*db.EPGProgram{}
		for _, id := range ids {
			out[id] = []*db.EPGProgram{{ID: "p-" + id, Title: "T-" + id}}
		}
		return out, nil
	}
	rr := env.do(http.MethodPost, "/api/v1/iptv/schedule",
		`{"channels":["c-1","c-2"]}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rr.Code)
	}
	data, _ := iptvDecodeData(t, rr).(map[string]any)
	if len(data) != 2 || data["c-1"] == nil || data["c-2"] == nil {
		t.Fatalf("bulk shape: %v", data)
	}
}

func TestIPTVHandler_BulkSchedule_POST_LargeList(t *testing.T) {
	// 2,000 channels comfortably exceeds any URL-length limit and
	// forces the repo-level chunker to kick in. The handler path has
	// to route all of them to the service in one call.
	env := newIPTVTestEnv(t)
	ids := make([]string, 2000)
	for i := range ids {
		id := fmt.Sprintf("c-%04d", i)
		ids[i] = id
		env.svc.channelByID[id] = &db.Channel{ID: id, LibraryID: "lib-1"}
	}
	var gotIDs []string
	env.svc.bulkFn = func(_ context.Context, in []string, _, _ time.Time) (map[string][]*db.EPGProgram, error) {
		gotIDs = in
		return map[string][]*db.EPGProgram{}, nil
	}

	body, err := json.Marshal(map[string]any{"channels": ids})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	rr := env.do(http.MethodPost, "/api/v1/iptv/schedule", string(body))
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200, body=%s", rr.Code, rr.Body.String())
	}
	if len(gotIDs) != len(ids) {
		t.Fatalf("service got %d ids, want %d", len(gotIDs), len(ids))
	}
}

func TestIPTVHandler_BulkSchedule_POST_MissingChannels_400(t *testing.T) {
	env := newIPTVTestEnv(t)
	rr := env.do(http.MethodPost, "/api/v1/iptv/schedule", `{"channels":[]}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rr.Code)
	}
}

func TestIPTVHandler_BulkSchedule_POST_InvalidBody_400(t *testing.T) {
	env := newIPTVTestEnv(t)
	rr := env.do(http.MethodPost, "/api/v1/iptv/schedule", `{not json`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rr.Code)
	}
}

func TestIPTVHandler_BulkSchedule_POST_TooManyChannels_400(t *testing.T) {
	env := newIPTVTestEnv(t)
	ids := make([]string, 5001)
	for i := range ids {
		ids[i] = fmt.Sprintf("c-%d", i)
	}
	body, err := json.Marshal(map[string]any{"channels": ids})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	rr := env.do(http.MethodPost, "/api/v1/iptv/schedule", string(body))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rr.Code)
	}
}

func TestIPTVHandler_BulkSchedule_POST_UnknownField_400(t *testing.T) {
	env := newIPTVTestEnv(t)
	rr := env.do(http.MethodPost, "/api/v1/iptv/schedule",
		`{"channels":["c-1"],"foo":"bar"}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rr.Code)
	}
}

// ─── RefreshM3U / RefreshEPG ────────────────────────────────────────────────

func TestIPTVHandler_RefreshM3U_Returns202AndRunsAsync(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.runRefreshM3UCount = 42
	doneCh := make(chan string, 1)
	env.svc.runRefreshM3UDoneCh = doneCh

	rr := env.do(http.MethodPost, "/api/v1/libraries/lib-1/iptv/refresh-m3u", "")
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status: got %d want 202, body: %s", rr.Code, rr.Body.String())
	}
	data, _ := iptvDecodeData(t, rr).(map[string]any)
	if data["library_id"] != "lib-1" || data["status"] != "started" {
		t.Errorf("response payload: %v", data)
	}

	select {
	case <-doneCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("background RunRefreshM3U was not called within 500ms")
	}

	env.svc.mu.Lock()
	defer env.svc.mu.Unlock()
	if len(env.svc.runRefreshM3UCalls) != 1 || env.svc.runRefreshM3UCalls[0] != "lib-1" {
		t.Errorf("RunRefreshM3U calls: %v", env.svc.runRefreshM3UCalls)
	}
	if len(env.svc.publishFailedCalls) != 0 {
		t.Errorf("unexpected failure events: %v", env.svc.publishFailedCalls)
	}
}

func TestIPTVHandler_RefreshM3U_AlreadyInProgress_Returns409(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.tryAcquireErr = fmt.Errorf("library lib-1: %w", iptv.ErrRefreshInProgress)

	rr := env.do(http.MethodPost, "/api/v1/libraries/lib-1/iptv/refresh-m3u", "")
	// 409 + structured body is the contract the frontend relies on to
	// distinguish "join the in-flight refresh via SSE" from a hard
	// failure. A regression here would land back on the old 500 path
	// and revive the spurious error toast on legitimate races.
	if rr.Code != http.StatusConflict {
		t.Fatalf("status: got %d want 409, body: %s", rr.Code, rr.Body.String())
	}
	var body struct {
		Data struct {
			LibraryID string `json:"library_id"`
			Status    string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Data.LibraryID != "lib-1" || body.Data.Status != "in_progress" {
		t.Fatalf("body data: got %+v", body.Data)
	}
	env.svc.mu.Lock()
	defer env.svc.mu.Unlock()
	if len(env.svc.runRefreshM3UCalls) != 0 {
		t.Errorf("RunRefreshM3U should not be called when lock is held: %v", env.svc.runRefreshM3UCalls)
	}
}

func TestIPTVHandler_RefreshM3U_AsyncFailure_PublishesEvent(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.runRefreshM3UErr = errors.New("boom")
	doneCh := make(chan string, 1)
	env.svc.runRefreshM3UDoneCh = doneCh

	rr := env.do(http.MethodPost, "/api/v1/libraries/lib-1/iptv/refresh-m3u", "")
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status: got %d want 202", rr.Code)
	}

	select {
	case <-doneCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("background RunRefreshM3U was not called within 500ms")
	}

	// PublishRefreshFailed runs after RunRefreshM3U returns; poll
	// briefly so we don't depend on goroutine scheduling order.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		env.svc.mu.Lock()
		got := len(env.svc.publishFailedCalls)
		env.svc.mu.Unlock()
		if got > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	env.svc.mu.Lock()
	defer env.svc.mu.Unlock()
	if len(env.svc.publishFailedCalls) != 1 {
		t.Fatalf("publishFailedCalls: %v", env.svc.publishFailedCalls)
	}
	got := env.svc.publishFailedCalls[0]
	if got.LibraryID != "lib-1" || got.Error != "boom" {
		t.Errorf("publish payload: %+v", got)
	}
}

func TestIPTVHandler_RefreshEPG_ReportsCount(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.refreshEPGCount = 1234
	rr := env.do(http.MethodPost, "/api/v1/libraries/lib-1/iptv/refresh-epg", "")
	data, _ := iptvDecodeData(t, rr).(map[string]any)
	if data["programs_imported"] != float64(1234) {
		t.Errorf("programs_imported: %v", data["programs_imported"])
	}
}

// ─── Public IPTV ────────────────────────────────────────────────────────────

func TestIPTVHandler_PublicCountries_IncludesKnownCodes(t *testing.T) {
	env := newIPTVTestEnv(t)
	rr := env.do(http.MethodGet, "/api/v1/iptv/public/countries", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	data, _ := iptvDecodeData(t, rr).([]any)
	if len(data) < 5 {
		t.Fatalf("countries list seems truncated (got %d)", len(data))
	}
	// Each entry has code / name / flag.
	first, _ := data[0].(map[string]any)
	for _, k := range []string{"code", "name", "flag"} {
		if _, ok := first[k]; !ok {
			t.Errorf("missing field %q in entry: %v", k, first)
		}
	}
}

func TestIPTVHandler_ImportPublicIPTV_BadJSON_400(t *testing.T) {
	env := newIPTVTestEnv(t)
	rr := env.do(http.MethodPost, "/api/v1/iptv/public/import", `{not json`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rr.Code)
	}
}

func TestIPTVHandler_ImportPublicIPTV_UnknownCountry_400(t *testing.T) {
	env := newIPTVTestEnv(t)
	rr := env.do(http.MethodPost, "/api/v1/iptv/public/import", `{"country":"zz"}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rr.Code)
	}
}

func TestIPTVHandler_ImportPublicIPTV_CreatesLibrary(t *testing.T) {
	env := newIPTVTestEnv(t)
	rr := env.do(http.MethodPost, "/api/v1/iptv/public/import", `{"country":"us","name":"MyUS"}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status: got %d want 201, body: %s", rr.Code, rr.Body.String())
	}
	if len(env.libraries.created) != 1 {
		t.Fatalf("library not created: %v", env.libraries.created)
	}
	lib := env.libraries.created[0]
	if lib.ContentType != "livetv" || lib.Name != "MyUS" {
		t.Errorf("library fields: %+v", lib)
	}
	if lib.M3UURL == "" {
		t.Error("M3UURL not set from country")
	}
	// The handler fires off RefreshM3U in a goroutine — wait briefly to confirm.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		env.svc.mu.Lock()
		called := len(env.svc.refreshM3UCalls) > 0
		env.svc.mu.Unlock()
		if called {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("background RefreshM3U was not called within 500ms")
}

func TestIPTVHandler_ImportPublicIPTV_DefaultLibraryName(t *testing.T) {
	env := newIPTVTestEnv(t)
	rr := env.do(http.MethodPost, "/api/v1/iptv/public/import", `{"country":"us"}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status: %d", rr.Code)
	}
	lib := env.libraries.created[0]
	if !strings.HasPrefix(lib.Name, "Live TV - ") {
		t.Errorf("default name: %q", lib.Name)
	}
}

func TestIPTVHandler_ImportPublicIPTV_CreateError_500(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.libraries.createErr = errors.New("fs full")
	rr := env.do(http.MethodPost, "/api/v1/iptv/public/import", `{"country":"us"}`)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status: %d", rr.Code)
	}
}

// ─── EPG catalog + sources ───────────────────────────────────────────────

func TestIPTVHandler_AddEPGSource_Duplicate_409(t *testing.T) {
	env := newIPTVTestEnv(t)
	body := `{"url":"https://example/epg.xml"}`
	first := env.do(http.MethodPost, "/api/v1/libraries/lib-1/epg-sources", body)
	if first.Code != http.StatusCreated {
		t.Fatalf("first add status: %d", first.Code)
	}
	second := env.do(http.MethodPost, "/api/v1/libraries/lib-1/epg-sources", body)
	if second.Code != http.StatusConflict {
		t.Fatalf("duplicate add status: got %d want 409, body=%s",
			second.Code, second.Body.String())
	}
	// Ensure the error body carries our stable code so the frontend
	// can branch on it if it ever needs to (current UI just shows
	// the message).
	if !strings.Contains(second.Body.String(), "ALREADY_ATTACHED") {
		t.Errorf("duplicate response missing ALREADY_ATTACHED code: %s",
			second.Body.String())
	}
}

func TestIPTVHandler_EPGCatalog_Returns(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.epgCatalog = []iptv.PublicEPGSource{
		{ID: "test-src", Name: "Test", Language: "es", URL: "http://example/x.xml"},
	}
	rr := env.do(http.MethodGet, "/api/v1/iptv/epg-catalog", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	data, _ := iptvDecodeData(t, rr).([]any)
	if len(data) != 1 {
		t.Fatalf("catalog size: %d", len(data))
	}
}

func TestIPTVHandler_AddEPGSource_CatalogID(t *testing.T) {
	env := newIPTVTestEnv(t)
	rr := env.do(http.MethodPost, "/api/v1/libraries/lib-1/epg-sources",
		`{"catalog_id":"davidmuma-guiatv"}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status: %d, body=%s", rr.Code, rr.Body.String())
	}
	data, _ := iptvDecodeData(t, rr).(map[string]any)
	if data["catalog_id"] != "davidmuma-guiatv" {
		t.Errorf("catalog_id: %v", data["catalog_id"])
	}
}

func TestIPTVHandler_AddEPGSource_MissingFields_400(t *testing.T) {
	env := newIPTVTestEnv(t)
	rr := env.do(http.MethodPost, "/api/v1/libraries/lib-1/epg-sources", `{}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rr.Code)
	}
}

func TestIPTVHandler_AddEPGSource_InvalidJSON_400(t *testing.T) {
	env := newIPTVTestEnv(t)
	rr := env.do(http.MethodPost, "/api/v1/libraries/lib-1/epg-sources", `{bad`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rr.Code)
	}
}

func TestIPTVHandler_ListEPGSources_Empty(t *testing.T) {
	env := newIPTVTestEnv(t)
	rr := env.do(http.MethodGet, "/api/v1/libraries/lib-1/epg-sources", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	data, _ := iptvDecodeData(t, rr).([]any)
	if len(data) != 0 {
		t.Errorf("expected empty, got %d", len(data))
	}
}

func TestIPTVHandler_RemoveEPGSource_Works(t *testing.T) {
	env := newIPTVTestEnv(t)
	addRR := env.do(http.MethodPost, "/api/v1/libraries/lib-1/epg-sources",
		`{"url":"http://x/y.xml"}`)
	if addRR.Code != http.StatusCreated {
		t.Fatalf("add status: %d", addRR.Code)
	}
	added, _ := iptvDecodeData(t, addRR).(map[string]any)
	sourceID, _ := added["id"].(string)

	delRR := env.do(http.MethodDelete, "/api/v1/libraries/lib-1/epg-sources/"+sourceID, "")
	if delRR.Code != http.StatusNoContent {
		t.Fatalf("delete status: %d", delRR.Code)
	}
	listRR := env.do(http.MethodGet, "/api/v1/libraries/lib-1/epg-sources", "")
	data, _ := iptvDecodeData(t, listRR).([]any)
	if len(data) != 0 {
		t.Errorf("expected empty after delete, got %d", len(data))
	}
}

// ─── Channel health ──────────────────────────────────────────────────────

func TestIPTVHandler_ListUnhealthy_FiltersByThreshold(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.unhealthyByLibrary = map[string][]*db.Channel{
		"lib-1": {
			{ID: "c-dead", LibraryID: "lib-1", Name: "Dead", ConsecutiveFailures: 5},
			{ID: "c-flaky", LibraryID: "lib-1", Name: "Flaky", ConsecutiveFailures: 3},
			{ID: "c-ok", LibraryID: "lib-1", Name: "OK", ConsecutiveFailures: 1},
		},
	}
	rr := env.do(http.MethodGet, "/api/v1/libraries/lib-1/channels/unhealthy", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	data, _ := iptvDecodeData(t, rr).([]any)
	// Default threshold (3) should exclude c-ok but include c-flaky and c-dead.
	if len(data) != 2 {
		t.Fatalf("expected 2 unhealthy, got %d: %v", len(data), data)
	}
}

func TestIPTVHandler_ListUnhealthy_CustomThreshold(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.unhealthyByLibrary = map[string][]*db.Channel{
		"lib-1": {
			{ID: "c-dead", LibraryID: "lib-1", Name: "Dead", ConsecutiveFailures: 5},
			{ID: "c-flaky", LibraryID: "lib-1", Name: "Flaky", ConsecutiveFailures: 3},
		},
	}
	rr := env.do(http.MethodGet, "/api/v1/libraries/lib-1/channels/unhealthy?threshold=5", "")
	data, _ := iptvDecodeData(t, rr).([]any)
	if len(data) != 1 {
		t.Fatalf("threshold=5 should leave just c-dead, got %d", len(data))
	}
}

func TestIPTVHandler_ResetChannelHealth_Works(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.channelByID["c-1"] = &db.Channel{ID: "c-1", LibraryID: "lib-1"}
	rr := env.do(http.MethodPost, "/api/v1/channels/c-1/reset-health", "")
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: %d", rr.Code)
	}
	if len(env.svc.resetHealthCalls) != 1 || env.svc.resetHealthCalls[0] != "c-1" {
		t.Errorf("reset not recorded: %v", env.svc.resetHealthCalls)
	}
}

func TestIPTVHandler_DisableChannel_Works(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.channelByID["c-1"] = &db.Channel{ID: "c-1", LibraryID: "lib-1"}
	rr := env.do(http.MethodPost, "/api/v1/channels/c-1/disable", "")
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: %d", rr.Code)
	}
	if len(env.svc.setActiveCalls) != 1 ||
		env.svc.setActiveCalls[0].ID != "c-1" ||
		env.svc.setActiveCalls[0].Active {
		t.Errorf("disable not recorded: %v", env.svc.setActiveCalls)
	}
}

func TestIPTVHandler_EnableChannel_Works(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.channelByID["c-1"] = &db.Channel{ID: "c-1", LibraryID: "lib-1"}
	rr := env.do(http.MethodPost, "/api/v1/channels/c-1/enable", "")
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: %d", rr.Code)
	}
	if len(env.svc.setActiveCalls) != 1 ||
		!env.svc.setActiveCalls[0].Active {
		t.Errorf("enable not recorded: %v", env.svc.setActiveCalls)
	}
}

func TestIPTVHandler_ResetChannelHealth_InaccessibleLibrary_404(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.channelByID["c-secret"] = &db.Channel{ID: "c-secret", LibraryID: "lib-restricted"}
	env.access.accessFn = func(_, libraryID string) (bool, error) { return libraryID == "lib-ok", nil }
	rr := env.doAs(http.MethodPost, "/api/v1/channels/c-secret/reset-health", "", iptvUserClaims())
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on ACL deny, got %d", rr.Code)
	}
}

// ─── Channels without EPG + PATCH ────────────────────────────────────

func TestIPTVHandler_ListChannelsWithoutEPG(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.withoutEPGByLibrary = map[string][]*db.Channel{
		"lib-1": {
			{ID: "c-orphan-1", LibraryID: "lib-1", Name: "Orphan 1", TvgID: ""},
			{ID: "c-orphan-2", LibraryID: "lib-1", Name: "Orphan 2", TvgID: "wrong.id"},
		},
	}
	rr := env.do(http.MethodGet, "/api/v1/libraries/lib-1/channels/without-epg", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	data, _ := iptvDecodeData(t, rr).([]any)
	if len(data) != 2 {
		t.Fatalf("expected 2, got %d", len(data))
	}
}

func TestIPTVHandler_PatchChannel_SetsTvgID(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.channelByID["c-1"] = &db.Channel{
		ID: "c-1", LibraryID: "lib-1", Name: "La 1", TvgID: "",
	}
	rr := env.do(http.MethodPatch, "/api/v1/channels/c-1", `{"tvg_id":"La1.ES"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	if len(env.svc.tvgIDEdits) != 1 || env.svc.tvgIDEdits[0].TvgID != "La1.ES" {
		t.Errorf("edit not recorded: %v", env.svc.tvgIDEdits)
	}
	data, _ := iptvDecodeData(t, rr).(map[string]any)
	if data["tvg_id"] != "La1.ES" {
		t.Errorf("response tvg_id = %v, want La1.ES", data["tvg_id"])
	}
}

func TestIPTVHandler_PatchChannel_EmptyTvgID_Clears(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.channelByID["c-1"] = &db.Channel{
		ID: "c-1", LibraryID: "lib-1", Name: "La 1", TvgID: "old.id",
	}
	rr := env.do(http.MethodPatch, "/api/v1/channels/c-1", `{"tvg_id":""}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	if len(env.svc.tvgIDEdits) != 1 || env.svc.tvgIDEdits[0].TvgID != "" {
		t.Errorf("empty tvg_id should still hit service: %v", env.svc.tvgIDEdits)
	}
}

func TestIPTVHandler_PatchChannel_OmittedTvgID_NoOp(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.channelByID["c-1"] = &db.Channel{ID: "c-1", LibraryID: "lib-1"}
	rr := env.do(http.MethodPatch, "/api/v1/channels/c-1", `{}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	if len(env.svc.tvgIDEdits) != 0 {
		t.Errorf("omitted tvg_id should not touch service: %v", env.svc.tvgIDEdits)
	}
}

func TestIPTVHandler_PatchChannel_InvalidBody_400(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.channelByID["c-1"] = &db.Channel{ID: "c-1", LibraryID: "lib-1"}
	rr := env.do(http.MethodPatch, "/api/v1/channels/c-1", `{bad`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rr.Code)
	}
}

func TestIPTVHandler_PatchChannel_ACLDeny_404(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.channelByID["c-secret"] = &db.Channel{ID: "c-secret", LibraryID: "lib-restricted"}
	env.access.accessFn = func(_, libraryID string) (bool, error) { return libraryID == "lib-ok", nil }
	rr := env.doAs(http.MethodPatch, "/api/v1/channels/c-secret",
		`{"tvg_id":"X"}`, iptvUserClaims())
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: %d, want 404", rr.Code)
	}
}

func TestIPTVHandler_PatchChannel_TrimsSpaces(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.channelByID["c-1"] = &db.Channel{ID: "c-1", LibraryID: "lib-1"}
	rr := env.do(http.MethodPatch, "/api/v1/channels/c-1", `{"tvg_id":"  La1.ES  "}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	if env.svc.tvgIDEdits[0].TvgID != "La1.ES" {
		t.Errorf("expected trimmed value, got %q", env.svc.tvgIDEdits[0].TvgID)
	}
}

func TestIPTVHandler_ReorderEPGSources_Works(t *testing.T) {
	env := newIPTVTestEnv(t)
	a, _ := iptvDecodeData(t, env.do(http.MethodPost, "/api/v1/libraries/lib-1/epg-sources",
		`{"url":"http://x/a.xml"}`)).(map[string]any)
	b, _ := iptvDecodeData(t, env.do(http.MethodPost, "/api/v1/libraries/lib-1/epg-sources",
		`{"url":"http://x/b.xml"}`)).(map[string]any)

	aID, _ := a["id"].(string)
	bID, _ := b["id"].(string)

	body := fmt.Sprintf(`{"source_ids":["%s","%s"]}`, bID, aID)
	rr := env.do(http.MethodPatch, "/api/v1/libraries/lib-1/epg-sources/reorder", body)
	if rr.Code != http.StatusOK {
		t.Fatalf("reorder status: %d body=%s", rr.Code, rr.Body.String())
	}
	data, _ := iptvDecodeData(t, rr).([]any)
	first, _ := data[0].(map[string]any)
	if first["id"] != bID {
		t.Errorf("first id after reorder = %v, want %s", first["id"], bID)
	}
}
