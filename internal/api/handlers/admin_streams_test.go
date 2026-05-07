package handlers_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/api/handlers"
	"hubplay/internal/stream"
)

// injectSession plants a fully-formed session straight into the
// manager's internal map via the package's exported test seam.
// Real sessions are built by StartSession through ffmpeg; the
// admin panel just reads the snapshot, so faking the rest of the
// pipeline is unnecessary.
func injectSession(t *testing.T, mgr *stream.Manager, key, userID, itemID, profileName string, method stream.PlaybackMethod, startedAt time.Time) {
	t.Helper()
	stream.SetSessionForTest(mgr, key, &stream.ManagedSession{
		Session:      stream.NewClosedSessionForTest(key, itemID, profileName, t.TempDir(), startedAt),
		UserID:       userID,
		Decision:     stream.PlaybackDecision{Method: method},
		LastAccessed: startedAt,
	})
}

func TestAdminStreams_ListSessions_EmptyReturnsEmptyArray(t *testing.T) {
	// Empty sessions must return a JSON array, not null — the frontend
	// renders an EmptyState only when data.length === 0, and `null`
	// would crash the .map() call.
	mgr := stream.NewManagerForTest()
	h := handlers.NewAdminStreamsHandler(mgr, nil, nil, slog.Default())

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/sessions", nil)
	h.ListSessions(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Data == nil {
		t.Fatal("data is null; want []")
	}
	if len(body.Data) != 0 {
		t.Errorf("data has %d entries on empty manager", len(body.Data))
	}
}

func TestAdminStreams_ListSessions_SortsByStartedAtDesc(t *testing.T) {
	// Freshest session first matches the "what's happening now?"
	// admin workflow; older sessions land lower on the panel.
	mgr := stream.NewManagerForTest()
	now := time.Now().UTC()
	injectSession(t, mgr, "userA:item1:720p", "userA", "item1", "720p",
		stream.MethodTranscode, now.Add(-2*time.Minute))
	injectSession(t, mgr, "userB:item2:1080p", "userB", "item2", "1080p",
		stream.MethodTranscode, now.Add(-30*time.Second))
	injectSession(t, mgr, "userC:item3:original", "userC", "item3", "",
		stream.MethodDirectPlay, now.Add(-10*time.Second))

	h := handlers.NewAdminStreamsHandler(mgr, nil, nil, slog.Default())
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/sessions", nil)
	h.ListSessions(rr, req)

	var body struct {
		Data []struct {
			SessionID string `json:"session_id"`
			UserID    string `json:"user_id"`
			ItemID    string `json:"item_id"`
			Profile   string `json:"profile"`
			Method    string `json:"method"`
			StartedAt string `json:"started_at"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data) != 3 {
		t.Fatalf("got %d sessions, want 3", len(body.Data))
	}
	if body.Data[0].UserID != "userC" {
		t.Errorf("first row = %s, want userC (most recent)", body.Data[0].UserID)
	}
	if body.Data[2].UserID != "userA" {
		t.Errorf("last row = %s, want userA (oldest)", body.Data[2].UserID)
	}
	if body.Data[2].Method != "Transcode" {
		t.Errorf("userA method = %q, want Transcode", body.Data[2].Method)
	}
	if body.Data[0].Method != "DirectPlay" {
		t.Errorf("userC method = %q, want DirectPlay", body.Data[0].Method)
	}
	if body.Data[0].Profile != "" {
		t.Errorf("DirectPlay session should have empty profile, got %q", body.Data[0].Profile)
	}
}

func TestAdminStreams_KillSession_StopsExistingSession(t *testing.T) {
	mgr := stream.NewManagerForTest()
	now := time.Now().UTC()
	injectSession(t, mgr, "userA:item1:720p", "userA", "item1", "720p",
		stream.MethodTranscode, now)

	if mgr.ActiveSessions() != 1 {
		t.Fatalf("setup: ActiveSessions = %d, want 1", mgr.ActiveSessions())
	}

	h := handlers.NewAdminStreamsHandler(mgr, nil, nil, slog.Default())
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/system/sessions/userA:item1:720p", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "userA:item1:720p")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	h.KillSession(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", rr.Code, rr.Body.String())
	}
	if mgr.ActiveSessions() != 0 {
		t.Errorf("session not removed; ActiveSessions = %d", mgr.ActiveSessions())
	}
}

func TestAdminStreams_KillSession_IdempotentOnUnknownID(t *testing.T) {
	// Killing a session that's already gone (idle reaper, user
	// teardown, ffmpeg crash) must succeed quietly so the admin
	// panel's button doesn't surface a bogus error to the operator
	// — the user-visible result is identical to a real kill.
	mgr := stream.NewManagerForTest()
	h := handlers.NewAdminStreamsHandler(mgr, nil, nil, slog.Default())

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/system/sessions/ghost", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "ghost")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	h.KillSession(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204 (idempotent)", rr.Code)
	}
}

func TestAdminStreams_KillSession_RejectsEmptyID(t *testing.T) {
	mgr := stream.NewManagerForTest()
	h := handlers.NewAdminStreamsHandler(mgr, nil, nil, slog.Default())

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/system/sessions/", nil)
	// No URLParams.Add → chi.URLParam returns "".

	h.KillSession(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

// TestAdminStreams_KillSession_DecodesPercentEncodedID covers the same
// shape of bug the /collections endpoint had: session keys contain
// colons ("userID:itemID:profile") and the frontend
// encodeURIComponent's them, so chi.URLParam delivers
// "user%3Aitem%3Aprofile". Without url.PathUnescape the manager
// lookup misses, StopSession idempotents to a no-op, and the panel
// returns 204 without actually killing anything — the worst kind
// of bug because the UI thinks success but the session keeps
// running. Pin the decode so a future edit can't regress it.
func TestAdminStreams_KillSession_DecodesPercentEncodedID(t *testing.T) {
	mgr := stream.NewManagerForTest()
	now := time.Now().UTC()
	injectSession(t, mgr, "userA:item1:720p", "userA", "item1", "720p",
		stream.MethodTranscode, now)

	if mgr.ActiveSessions() != 1 {
		t.Fatalf("setup: ActiveSessions = %d, want 1", mgr.ActiveSessions())
	}

	h := handlers.NewAdminStreamsHandler(mgr, nil, nil, slog.Default())
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/system/sessions/userA%3Aitem1%3A720p", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "userA%3Aitem1%3A720p")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	h.KillSession(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", rr.Code, rr.Body.String())
	}
	if mgr.ActiveSessions() != 0 {
		t.Errorf("session not removed (handler must url.PathUnescape); ActiveSessions = %d", mgr.ActiveSessions())
	}
}
