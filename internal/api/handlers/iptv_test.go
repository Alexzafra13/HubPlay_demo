package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/auth"
	"hubplay/internal/db"
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

	// Per-user channel-favorites set, keyed by userID → set of channelIDs.
	// Nil until the first write; methods lazily initialize.
	favoritesByUser map[string]map[string]struct{}
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

func (s *iptvFakeService) RefreshEPG(_ context.Context, _ string) (int, error) {
	return s.refreshEPGCount, s.refreshEPGErr
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

// ─── Fake LibraryRepository (handler only calls Create) ─────────────────────

type iptvFakeLibraryRepo struct {
	created []*db.Library
	createErr error
}

func (r *iptvFakeLibraryRepo) Create(_ context.Context, lib *db.Library) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.created = append(r.created, lib)
	return nil
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
		r.Get("/iptv/public/countries", env.handler.PublicCountries)
		r.Post("/iptv/public/import", env.handler.ImportPublicIPTV)
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

// ─── RefreshM3U / RefreshEPG ────────────────────────────────────────────────

func TestIPTVHandler_RefreshM3U_ReportsCount(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.refreshM3UCount = 42
	rr := env.do(http.MethodPost, "/api/v1/libraries/lib-1/iptv/refresh-m3u", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	data, _ := iptvDecodeData(t, rr).(map[string]any)
	if data["channels_imported"] != float64(42) {
		t.Errorf("channels_imported: %v", data["channels_imported"])
	}
	if len(env.svc.refreshM3UCalls) == 0 || env.svc.refreshM3UCalls[0] != "lib-1" {
		t.Errorf("libraryID: %v", env.svc.refreshM3UCalls)
	}
}

func TestIPTVHandler_RefreshM3U_ServiceError_500(t *testing.T) {
	env := newIPTVTestEnv(t)
	env.svc.refreshM3UErr = errors.New("boom")
	rr := env.do(http.MethodPost, "/api/v1/libraries/lib-1/iptv/refresh-m3u", "")
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d want 500", rr.Code)
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
