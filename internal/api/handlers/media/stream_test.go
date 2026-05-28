package media

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/api/handlers"
	"hubplay/internal/auth"
	"hubplay/internal/domain"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/stream"
	"hubplay/internal/testutil"
)

// ─── Fakes ──────────────────────────────────────────────────────────────────

type fakeStreamManager struct {
	mu             sync.Mutex
	startSessionFn func(ctx context.Context, req stream.StartSessionRequest) (*stream.ManagedSession, error)
	sessions       map[string]*stream.ManagedSession
	stopped        map[string]bool
}

func newFakeStreamManager() *fakeStreamManager {
	return &fakeStreamManager{
		sessions: map[string]*stream.ManagedSession{},
		stopped:  map[string]bool{},
	}
}

func (m *fakeStreamManager) StartSession(ctx context.Context, req stream.StartSessionRequest) (*stream.ManagedSession, error) {
	if m.startSessionFn != nil {
		return m.startSessionFn(ctx, req)
	}
	return nil, errors.New("not configured")
}

func (m *fakeStreamManager) GetSession(key string) (*stream.ManagedSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[key]
	return s, ok
}

func (m *fakeStreamManager) RestartSessionAt(key string, segmentIndex int, segmentDuration float64) error {
	return nil
}

func (m *fakeStreamManager) StopSession(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopped[key] = true
}

func (m *fakeStreamManager) StopSessionsByItem(userID, itemID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := userID + ":" + itemID + ":"
	count := 0
	for k := range m.sessions {
		if strings.HasPrefix(k, prefix) {
			m.stopped[k] = true
			count++
		}
	}
	// Mirror real Manager: also surface the item-prefix as a "key"
	// so test assertions can detect which item was bulk-stopped
	// without having to enumerate every (quality, audio) tuple.
	m.stopped[prefix] = true
	return count
}

func (m *fakeStreamManager) ActiveSessions() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// streamFakeItemRepo is distinct from image_test.go's fakeItemRepo because
// both live in the same test package.
type streamFakeItemRepo struct {
	byID map[string]*librarymodel.Item
}

func (r *streamFakeItemRepo) GetByID(_ context.Context, id string) (*librarymodel.Item, error) {
	if it, ok := r.byID[id]; ok {
		return it, nil
	}
	return nil, domain.NewNotFound("item")
}

func (r *streamFakeItemRepo) List(_ context.Context, _ librarymodel.ItemFilter) ([]*librarymodel.Item, int, error) {
	return nil, 0, nil
}

type streamFakeMediaStreamRepo struct {
	byItem map[string][]*librarymodel.MediaStream
}

func (r *streamFakeMediaStreamRepo) ListByItem(_ context.Context, itemID string) ([]*librarymodel.MediaStream, error) {
	return r.byItem[itemID], nil
}

// Compile-time interface checks.
var (
	_ handlers.StreamManagerService  = (*fakeStreamManager)(nil)
	_ handlers.ItemRepository        = (*streamFakeItemRepo)(nil)
	_ handlers.MediaStreamRepository = (*streamFakeMediaStreamRepo)(nil)
)

// ─── Test env ───────────────────────────────────────────────────────────────

type streamTestEnv struct {
	t       *testing.T
	manager *fakeStreamManager
	items   *streamFakeItemRepo
	streams *streamFakeMediaStreamRepo
	handler *StreamHandler
	server  *httptest.Server
}

func newStreamTestEnv(t *testing.T) *streamTestEnv {
	t.Helper()
	env := &streamTestEnv{
		t:       t,
		manager: newFakeStreamManager(),
		items:   &streamFakeItemRepo{byID: map[string]*librarymodel.Item{}},
		streams: &streamFakeMediaStreamRepo{byItem: map[string][]*librarymodel.MediaStream{}},
	}
	// nil externalIDs + providers — existing stream tests don't hit
	// the external-subtitle endpoints, so the new handlers short-
	// circuit to 503 via their nil-guard. Tests that need them wire
	// fakes locally.
	env.handler = NewStreamHandler(env.manager, env.items, env.streams, nil, nil, nil, "http://test", testutil.NopLogger())

	r := chi.NewRouter()
	r.Route("/api/v1/stream/{itemId}", func(r chi.Router) {
		r.Get("/info", env.handler.Info)
		r.Get("/master.m3u8", env.handler.MasterPlaylist)
		r.Get("/{quality}/stream.m3u8", env.handler.QualityPlaylist)
		r.Get("/{quality}/{segment}", env.handler.Segment)
		r.Get("/direct", env.handler.DirectPlay)
		r.Delete("/session", env.handler.StopSession)
		r.Get("/subtitles", env.handler.Subtitles)
		r.Get("/subtitles/{trackIndex}", env.handler.SubtitleTrack)
	})
	env.server = httptest.NewServer(r)
	t.Cleanup(env.server.Close)
	return env
}

// doWithClaims issues a request with an auth.Claims value injected in the
// context. It bypasses the middleware by calling the router with a pre-built
// context directly — httptest.Server doesn't carry the claims otherwise.
func (e *streamTestEnv) doWithClaims(method, path string, claims *auth.Claims) *httptest.ResponseRecorder {
	e.t.Helper()
	// Rebuild the router so we can inject context before dispatch.
	r := chi.NewRouter()
	r.Route("/api/v1/stream/{itemId}", func(r chi.Router) {
		r.Get("/info", e.handler.Info)
		r.Get("/master.m3u8", e.handler.MasterPlaylist)
		r.Get("/{quality}/stream.m3u8", e.handler.QualityPlaylist)
		r.Get("/{quality}/{segment}", e.handler.Segment)
		r.Get("/direct", e.handler.DirectPlay)
		r.Delete("/session", e.handler.StopSession)
		r.Get("/subtitles", e.handler.Subtitles)
		r.Get("/subtitles/{trackIndex}", e.handler.SubtitleTrack)
	})
	req := httptest.NewRequest(method, path, nil)
	if claims != nil {
		req = req.WithContext(auth.WithClaims(req.Context(), claims))
	}
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

// ─── Info ───────────────────────────────────────────────────────────────────

func TestStreamHandler_Info_HappyPath(t *testing.T) {
	env := newStreamTestEnv(t)
	env.items.byID["item-1"] = &librarymodel.Item{ID: "item-1", Type: "movie", Container: "mp4"}
	env.streams.byItem["item-1"] = []*librarymodel.MediaStream{
		{StreamType: "video", Codec: "h264", IsDefault: true},
		{StreamType: "audio", Codec: "aac", IsDefault: true},
	}

	resp, err := http.Get(env.server.URL + "/api/v1/stream/item-1/info")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	var env_ map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&env_); err != nil {
		t.Fatalf("decode: %v", err)
	}
	data, _ := env_["data"].(map[string]any)
	if data["item_id"] != "item-1" {
		t.Errorf("item_id: %v", data["item_id"])
	}
	if _, ok := data["profiles"].([]any); !ok {
		t.Errorf("profiles: missing or wrong shape")
	}
}

func TestStreamHandler_Info_ItemNotFound(t *testing.T) {
	env := newStreamTestEnv(t)
	resp, err := http.Get(env.server.URL + "/api/v1/stream/missing/info")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status: got %d want 404", resp.StatusCode)
	}
}

// ─── MasterPlaylist ─────────────────────────────────────────────────────────

func TestStreamHandler_MasterPlaylist_HappyPath(t *testing.T) {
	env := newStreamTestEnv(t)
	env.items.byID["item-1"] = &librarymodel.Item{ID: "item-1", Type: "movie"}

	resp, err := http.Get(env.server.URL + "/api/v1/stream/item-1/master.m3u8")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/vnd.apple.mpegurl" {
		t.Errorf("content-type: %q", ct)
	}
	body, _ := readBody(resp)
	if !strings.HasPrefix(body, "#EXTM3U") {
		t.Errorf("master playlist malformed (prefix): %q", body[:min(30, len(body))])
	}
}

func TestStreamHandler_MasterPlaylist_ItemNotFound(t *testing.T) {
	env := newStreamTestEnv(t)
	resp, err := http.Get(env.server.URL + "/api/v1/stream/missing/master.m3u8")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}

// ─── QualityPlaylist ────────────────────────────────────────────────────────

func TestStreamHandler_QualityPlaylist_Unauthenticated(t *testing.T) {
	env := newStreamTestEnv(t)
	// No claims injected → 401.
	rr := env.doWithClaims(http.MethodGet, "/api/v1/stream/item-1/720p/stream.m3u8", nil)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d want 401", rr.Code)
	}
}

func TestStreamHandler_QualityPlaylist_DirectPlayRedirect(t *testing.T) {
	env := newStreamTestEnv(t)
	env.manager.startSessionFn = func(_ context.Context, _ stream.StartSessionRequest) (*stream.ManagedSession, error) {
		return &stream.ManagedSession{
			Session:  &stream.Session{OutputDir: ""},
			Decision: stream.PlaybackDecision{Method: stream.MethodDirectPlay},
		}, nil
	}
	rr := env.doWithClaims(http.MethodGet, "/api/v1/stream/item-1/720p/stream.m3u8",
		&auth.Claims{UserID: "user-1"})
	if rr.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status: got %d want 307", rr.Code)
	}
	if loc := rr.Header().Get("Location"); !strings.Contains(loc, "/direct") {
		t.Errorf("Location header missing /direct: %q", loc)
	}
}

func TestStreamHandler_QualityPlaylist_ManifestServed(t *testing.T) {
	env := newStreamTestEnv(t)
	// Pre-create a manifest file so waitForFile returns immediately.
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "stream.m3u8")
	if err := os.WriteFile(manifestPath, []byte("#EXTM3U\n"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	env.manager.startSessionFn = func(_ context.Context, _ stream.StartSessionRequest) (*stream.ManagedSession, error) {
		return &stream.ManagedSession{
			Session:  &stream.Session{OutputDir: dir},
			Decision: stream.PlaybackDecision{Method: stream.MethodTranscode},
		}, nil
	}
	rr := env.doWithClaims(http.MethodGet, "/api/v1/stream/item-1/720p/stream.m3u8",
		&auth.Claims{UserID: "user-1"})
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200, body: %q", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/vnd.apple.mpegurl" {
		t.Errorf("content-type: %q", ct)
	}
}

// Regression: the synthesized VOD manifest must thread ?audio=N
// into every segment URL so hls.js carries it through to the segment
// handler. Otherwise the segment handler builds the wrong session
// key (without audio), can't find the registered session, and 404s.
func TestStreamHandler_QualityPlaylist_PropagatesAudioToSegments(t *testing.T) {
	env := newStreamTestEnv(t)
	// Item with a known duration → triggers the synthesized VOD path.
	env.items.byID["item-1"] = &librarymodel.Item{
		ID:            "item-1",
		Path:          "/tmp/x.mkv",
		IsAvailable:   true,
		DurationTicks: 60 * 10_000_000, // 60s → 10 segments at 6s
	}
	env.manager.startSessionFn = func(_ context.Context, _ stream.StartSessionRequest) (*stream.ManagedSession, error) {
		return &stream.ManagedSession{
			Session:  &stream.Session{OutputDir: t.TempDir()},
			Decision: stream.PlaybackDecision{Method: stream.MethodTranscode},
		}, nil
	}
	rr := env.doWithClaims(http.MethodGet, "/api/v1/stream/item-1/720p/stream.m3u8?audio=2",
		&auth.Claims{UserID: "user-1"})
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200, body: %q", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "?audio=2") {
		t.Errorf("synthesized manifest missing ?audio=2 on segment URLs:\n%s", body)
	}
	if strings.Contains(body, "?audio=2?") || strings.Contains(body, "?audio=2&audio=") {
		t.Errorf("synthesized manifest emitted a malformed audio query:\n%s", body)
	}
}

func TestStreamHandler_QualityPlaylist_ManagerError(t *testing.T) {
	env := newStreamTestEnv(t)
	env.manager.startSessionFn = func(_ context.Context, _ stream.StartSessionRequest) (*stream.ManagedSession, error) {
		return nil, domain.NewTranscodeBusy(3, 3)
	}
	rr := env.doWithClaims(http.MethodGet, "/api/v1/stream/item-1/720p/stream.m3u8",
		&auth.Claims{UserID: "user-1"})
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d want 503", rr.Code)
	}
}

// ─── Segment ────────────────────────────────────────────────────────────────

func TestStreamHandler_Segment_Unauthenticated(t *testing.T) {
	env := newStreamTestEnv(t)
	rr := env.doWithClaims(http.MethodGet, "/api/v1/stream/item-1/720p/segment00001.ts", nil)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status: %d", rr.Code)
	}
}

func TestStreamHandler_Segment_SessionNotFound(t *testing.T) {
	env := newStreamTestEnv(t)
	rr := env.doWithClaims(http.MethodGet, "/api/v1/stream/item-1/720p/segment00001.ts",
		&auth.Claims{UserID: "user-1"})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: got %d want 404", rr.Code)
	}
}

func TestStreamHandler_Segment_InvalidFilename(t *testing.T) {
	env := newStreamTestEnv(t)
	dir := t.TempDir()
	env.manager.sessions[stream.SessionKey("user-1", "item-1", "720p", -1, -1)] = &stream.ManagedSession{
		Session: &stream.Session{OutputDir: dir},
	}
	// Filename doesn't match validSegmentName regex.
	rr := env.doWithClaims(http.MethodGet, "/api/v1/stream/item-1/720p/evil.sh",
		&auth.Claims{UserID: "user-1"})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400, body: %q", rr.Code, rr.Body.String())
	}
}

func TestStreamHandler_Segment_HappyPath(t *testing.T) {
	env := newStreamTestEnv(t)
	dir := t.TempDir()
	segPath := filepath.Join(dir, "segment00042.ts")
	if err := os.WriteFile(segPath, []byte("TS_BYTES"), 0o644); err != nil {
		t.Fatalf("write seg: %v", err)
	}
	env.manager.sessions[stream.SessionKey("user-1", "item-1", "720p", -1, -1)] = &stream.ManagedSession{
		Session: &stream.Session{OutputDir: dir},
	}
	rr := env.doWithClaims(http.MethodGet, "/api/v1/stream/item-1/720p/segment00042.ts",
		&auth.Claims{UserID: "user-1"})
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "video/mp2t" {
		t.Errorf("content-type: %q", ct)
	}
	if rr.Body.String() != "TS_BYTES" {
		t.Errorf("body mismatch: %q", rr.Body.String())
	}
}

// Regression: when the master playlist embeds ?audio=N, segment URLs
// must carry the same param so the handler reconstructs the key the
// session was registered under (user:item:profile:audio). Before the
// fix, the segment handler ignored ?audio= and built a 3-part key,
// never finding the session — every segment 404'd with
// SESSION_NOT_FOUND, and the player accreted zombie sessions across
// retries until MaxTranscodeSessionsPerUser tripped 503 storms.
func TestStreamHandler_Segment_HonoursAudioQuery(t *testing.T) {
	env := newStreamTestEnv(t)
	dir := t.TempDir()
	segPath := filepath.Join(dir, "segment00012.ts")
	if err := os.WriteFile(segPath, []byte("AUDIO_VARIANT"), 0o644); err != nil {
		t.Fatalf("write seg: %v", err)
	}
	// Session registered with audioStreamIndex=1 (Spanish dub on a
	// typical Marvel rip — index 0 is English default).
	env.manager.sessions[stream.SessionKey("user-1", "item-1", "1080p", 1, -1)] = &stream.ManagedSession{
		Session: &stream.Session{OutputDir: dir},
	}
	// Request without ?audio=1 → handler builds key with audio=-1
	// → no session match → 404. The test pins this behaviour so a
	// future "let it fall back to default" change is at least a
	// deliberate decision.
	rr := env.doWithClaims(http.MethodGet, "/api/v1/stream/item-1/1080p/segment00012.ts",
		&auth.Claims{UserID: "user-1"})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("without ?audio, status = %d, want 404", rr.Code)
	}
	// With ?audio=1 → handler reconstructs the right key → 200.
	rr = env.doWithClaims(http.MethodGet, "/api/v1/stream/item-1/1080p/segment00012.ts?audio=1",
		&auth.Claims{UserID: "user-1"})
	if rr.Code != http.StatusOK {
		t.Fatalf("with ?audio=1, status = %d, want 200, body=%q", rr.Code, rr.Body.String())
	}
	if rr.Body.String() != "AUDIO_VARIANT" {
		t.Errorf("body = %q, want AUDIO_VARIANT", rr.Body.String())
	}
}

// ─── DirectPlay ─────────────────────────────────────────────────────────────

func TestStreamHandler_DirectPlay_ItemNotFound(t *testing.T) {
	env := newStreamTestEnv(t)
	resp, err := http.Get(env.server.URL + "/api/v1/stream/missing/direct")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}

func TestStreamHandler_DirectPlay_Unavailable(t *testing.T) {
	env := newStreamTestEnv(t)
	env.items.byID["item-1"] = &librarymodel.Item{ID: "item-1", Path: "", IsAvailable: false}

	resp, err := http.Get(env.server.URL + "/api/v1/stream/item-1/direct")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}

func TestStreamHandler_DirectPlay_ServesFile(t *testing.T) {
	env := newStreamTestEnv(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "movie.mp4")
	payload := []byte("FAKE_MP4_CONTENT")
	if err := os.WriteFile(p, payload, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	env.items.byID["item-1"] = &librarymodel.Item{ID: "item-1", Path: p, Container: "mp4", IsAvailable: true}

	resp, err := http.Get(env.server.URL + "/api/v1/stream/item-1/direct")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "video/mp4" {
		t.Errorf("content-type: %q", ct)
	}
	body, _ := readBody(resp)
	if body != string(payload) {
		t.Errorf("body: got %q want %q", body, payload)
	}
}

// ─── StopSession ────────────────────────────────────────────────────────────

func TestStreamHandler_StopSession_Unauthenticated(t *testing.T) {
	env := newStreamTestEnv(t)
	rr := env.doWithClaims(http.MethodDelete, "/api/v1/stream/item-1/session", nil)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status: %d", rr.Code)
	}
}

func TestStreamHandler_StopSession_HappyPath(t *testing.T) {
	env := newStreamTestEnv(t)
	// Seed sessions across three (quality, audio) tuples so the bulk-
	// stop path has something to clear. The legacy per-key stop path
	// only covered one tuple — it was the root cause of the 503-
	// TranscodeBusy storms after PR #229 landed audio-aware keys.
	env.manager.sessions["user-1:item-1:1080p:-1"] = &stream.ManagedSession{}
	env.manager.sessions["user-1:item-1:720p:-1"] = &stream.ManagedSession{}
	env.manager.sessions["user-1:item-1:1080p:1"] = &stream.ManagedSession{}
	// Distractor for a different item — must not be touched.
	env.manager.sessions["user-1:other-item:720p:-1"] = &stream.ManagedSession{}

	rr := env.doWithClaims(http.MethodDelete, "/api/v1/stream/item-1/session",
		&auth.Claims{UserID: "user-1"})
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: %d", rr.Code)
	}
	for _, k := range []string{
		"user-1:item-1:1080p:-1",
		"user-1:item-1:720p:-1",
		"user-1:item-1:1080p:1",
	} {
		if !env.manager.stopped[k] {
			t.Errorf("expected key %q to be stopped; got %v", k, env.manager.stopped)
		}
	}
	if env.manager.stopped["user-1:other-item:720p:-1"] {
		t.Errorf("foreign session for other-item should not be stopped")
	}
}

// ─── Subtitles ──────────────────────────────────────────────────────────────

func TestStreamHandler_Subtitles_FiltersAndShapes(t *testing.T) {
	env := newStreamTestEnv(t)
	env.streams.byItem["item-1"] = []*librarymodel.MediaStream{
		{StreamType: "video", Codec: "h264"},
		{StreamType: "audio", Codec: "aac"},
		{StreamType: "subtitle", Codec: "subrip", Language: "en", Title: "English", IsDefault: true},
		{StreamType: "subtitle", Codec: "ass", Language: "es", IsForced: true},
	}
	resp, err := http.Get(env.server.URL + "/api/v1/stream/item-1/subtitles")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	subs, _ := out["data"].([]any)
	if len(subs) != 2 {
		t.Fatalf("expected 2 subtitle entries, got %d (%v)", len(subs), subs)
	}
}

func TestStreamHandler_SubtitleTrack_ItemNotFound(t *testing.T) {
	env := newStreamTestEnv(t)
	resp, err := http.Get(env.server.URL + "/api/v1/stream/missing/subtitles/0")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}

func TestStreamHandler_SubtitleTrack_FileUnavailable(t *testing.T) {
	env := newStreamTestEnv(t)
	env.items.byID["item-1"] = &librarymodel.Item{ID: "item-1", Path: ""}
	resp, err := http.Get(env.server.URL + "/api/v1/stream/item-1/subtitles/0")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}

// ─── Pure helpers ───────────────────────────────────────────────────────────

func TestValidSegmentName(t *testing.T) {
	accept := []string{"segment00000.ts", "segment12345.ts", "segment99999.ts", "stream.m3u8"}
	reject := []string{
		"", "evil.sh", "../etc/passwd", "segment.ts", "segment1.ts",
		"segment000001.ts", "SEGMENT00001.ts", "stream.m3u", "foo/segment00001.ts",
		"segment00001.ts.sh",
	}
	for _, name := range accept {
		if !validSegmentName.MatchString(name) {
			t.Errorf("accept %q: should match but didn't", name)
		}
	}
	for _, name := range reject {
		if validSegmentName.MatchString(name) {
			t.Errorf("reject %q: matched but shouldn't", name)
		}
	}
}

func TestContainerToMIME(t *testing.T) {
	cases := map[string]string{
		"mp4":          "video/mp4",
		"mov":          "video/mp4",
		"webm":         "video/webm",
		"matroska":     "video/x-matroska",
		"mkv":          "video/x-matroska",
		"avi":          "video/x-msvideo",
		"mpegts":       "video/mp2t",
		"ts":           "video/mp2t",
		"unknown":      "video/mp4", // default
		"":             "video/mp4",
		"matroska,mp4": "video/x-matroska", // first match wins
	}
	for in, want := range cases {
		if got := containerToMIME(in); got != want {
			t.Errorf("containerToMIME(%q) = %q want %q", in, got, want)
		}
	}
}

func TestParseFloat(t *testing.T) {
	cases := map[string]float64{
		"":      0,
		"0":     0,
		"3.14":  3.14,
		"-1.5":  -1.5,
		"bogus": 0, // error → 0
	}
	for in, want := range cases {
		if got := parseFloat(in); got != want {
			t.Errorf("parseFloat(%q) = %v want %v", in, got, want)
		}
	}
}

func TestWaitForFile_Appears(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "late.txt")

	// Sleep LEGÍTIMO (F15-1 batch 4): la unidad bajo test es
	// waitForFile, que ESPERA la aparición de un fichero. Sin retraso
	// el fichero existe en el primer poll y no estamos testeando la
	// espera. Es el escenario "file appears late" que la función debe
	// manejar.
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = os.WriteFile(p, []byte("hi"), 0o644)
	}()

	if err := waitForFile(p, 2*time.Second); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestWaitForFile_Timeout(t *testing.T) {
	p := filepath.Join(t.TempDir(), "never.txt")
	err := waitForFile(p, 100*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

// ─── Utility ────────────────────────────────────────────────────────────────

func readBody(resp *http.Response) (string, error) {
	defer resp.Body.Close() //nolint:errcheck
	buf := make([]byte, 0, 512)
	tmp := make([]byte, 512)
	for {
		n, err := resp.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	return string(buf), nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
