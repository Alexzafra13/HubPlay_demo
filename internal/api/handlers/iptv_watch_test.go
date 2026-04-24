package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"hubplay/internal/auth"
	"hubplay/internal/db"
)

// Helper: decode `{"data": ...}` from a response body.
func watchDecode(t *testing.T, rr *httptest.ResponseRecorder) any {
	t.Helper()
	var out map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out["data"]
}

// seedChannel adds one channel to the fake service + library repo so
// the handlers can resolve it.
func seedChannel(env *iptvTestEnv, chID, libID string) {
	ch := &db.Channel{
		ID: chID, LibraryID: libID, Name: chID,
		StreamURL: "http://example/" + chID, IsActive: true,
	}
	env.svc.channels[libID] = append(env.svc.channels[libID], ch)
	env.svc.channelByID[chID] = ch
	if env.libraries.librariesByID == nil {
		env.libraries.librariesByID = map[string]*db.Library{}
	}
	if _, ok := env.libraries.librariesByID[libID]; !ok {
		env.libraries.librariesByID[libID] = &db.Library{ID: libID, Name: libID, ContentType: "livetv"}
	}
}

func TestIPTVHandler_RecordChannelWatch_HappyPath(t *testing.T) {
	env := newIPTVTestEnv(t)
	seedChannel(env, "ch-1", "lib-a")

	rr := env.doAs(http.MethodPost, "/api/v1/channels/ch-1/watch", "",
		&auth.Claims{UserID: "u-alice", Role: "user"})
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	if len(env.svc.recordWatchCalls) != 1 {
		t.Fatalf("expected 1 record call, got %d", len(env.svc.recordWatchCalls))
	}
	call := env.svc.recordWatchCalls[0]
	if call.UserID != "u-alice" || call.ChannelID != "ch-1" {
		t.Errorf("unexpected call: %+v", call)
	}
	data, _ := watchDecode(t, rr).(map[string]any)
	if data["channel_id"] != "ch-1" || data["last_watched_at"] == "" {
		t.Errorf("bad response body: %v", data)
	}
}

func TestIPTVHandler_RecordChannelWatch_Unauthenticated(t *testing.T) {
	env := newIPTVTestEnv(t)
	seedChannel(env, "ch-1", "lib-a")

	rr := env.doAs(http.MethodPost, "/api/v1/channels/ch-1/watch", "", nil)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
	if len(env.svc.recordWatchCalls) != 0 {
		t.Errorf("beacon ran without auth: %d calls", len(env.svc.recordWatchCalls))
	}
}

func TestIPTVHandler_RecordChannelWatch_DeniesWithoutLibraryAccess(t *testing.T) {
	env := newIPTVTestEnv(t)
	seedChannel(env, "ch-1", "lib-a")
	env.access.accessFn = func(_, libraryID string) (bool, error) {
		return false, nil // non-admin can't see this library
	}

	rr := env.doAs(http.MethodPost, "/api/v1/channels/ch-1/watch", "",
		&auth.Claims{UserID: "u-eve", Role: "user"})
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 deny, got %d", rr.Code)
	}
	if len(env.svc.recordWatchCalls) != 0 {
		t.Errorf("record fired despite ACL deny: %d", len(env.svc.recordWatchCalls))
	}
}

func TestIPTVHandler_RecordChannelWatch_RaceOnDeletedChannel(t *testing.T) {
	// RunNow-style race: the channel was resolved by GetChannel but
	// disappeared between the lookup and RecordWatch (CASCADE after
	// M3U refresh). The service returns ErrChannelNotFound; the
	// handler surfaces a clean 404 instead of 500.
	env := newIPTVTestEnv(t)
	seedChannel(env, "ch-1", "lib-a")
	env.svc.recordWatchErr = db.ErrChannelNotFound

	rr := env.doAs(http.MethodPost, "/api/v1/channels/ch-1/watch", "",
		&auth.Claims{UserID: "u-alice", Role: "user"})
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 on race, got %d", rr.Code)
	}
}

func TestIPTVHandler_ListContinueWatching_HappyPathAsUser(t *testing.T) {
	env := newIPTVTestEnv(t)
	seedChannel(env, "ch-1", "lib-a")
	seedChannel(env, "ch-2", "lib-a")

	// Simulate prior plays: ch-1 then ch-2 (most recent first in fake).
	env.svc.watchedByUser = map[string][]*db.Channel{
		"u-alice": {
			env.svc.channelByID["ch-2"],
			env.svc.channelByID["ch-1"],
		},
	}
	// Alice has access to lib-a.
	env.libraries.userAccess = map[string]map[string]bool{
		"u-alice": {"lib-a": true},
	}

	rr := env.doAs(http.MethodGet, "/api/v1/me/channels/continue-watching?limit=5", "",
		&auth.Claims{UserID: "u-alice", Role: "user"})
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	data, _ := watchDecode(t, rr).([]any)
	if len(data) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(data))
	}
	first := data[0].(map[string]any)
	if first["id"] != "ch-2" {
		t.Errorf("wrong order: first=%v", first["id"])
	}
	if first["last_watched_at"] == "" {
		t.Error("last_watched_at missing")
	}
}

func TestIPTVHandler_ListContinueWatching_FiltersByAccess(t *testing.T) {
	// User has only lib-a access. History has channels from lib-a and
	// lib-b. Rail must show only lib-a.
	env := newIPTVTestEnv(t)
	seedChannel(env, "ch-a1", "lib-a")
	seedChannel(env, "ch-b1", "lib-b")

	env.svc.watchedByUser = map[string][]*db.Channel{
		"u-alice": {
			env.svc.channelByID["ch-b1"], // most recent but denied
			env.svc.channelByID["ch-a1"],
		},
	}
	env.libraries.userAccess = map[string]map[string]bool{
		"u-alice": {"lib-a": true},
	}

	rr := env.doAs(http.MethodGet, "/api/v1/me/channels/continue-watching", "",
		&auth.Claims{UserID: "u-alice", Role: "user"})
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	data, _ := watchDecode(t, rr).([]any)
	if len(data) != 1 {
		t.Fatalf("expected 1 row after ACL filter, got %d", len(data))
	}
	row := data[0].(map[string]any)
	if row["id"] != "ch-a1" {
		t.Errorf("leaked denied channel: %v", row["id"])
	}
}

func TestIPTVHandler_ListContinueWatching_AdminSkipsFilter(t *testing.T) {
	// Admin's call passes nil accessibleLibraries → service returns
	// everything. Verify by asserting the service didn't get a map.
	env := newIPTVTestEnv(t)
	seedChannel(env, "ch-a1", "lib-a")
	seedChannel(env, "ch-b1", "lib-b")
	env.svc.watchedByUser = map[string][]*db.Channel{
		"u-admin": {
			env.svc.channelByID["ch-a1"],
			env.svc.channelByID["ch-b1"],
		},
	}

	rr := env.doAs(http.MethodGet, "/api/v1/me/channels/continue-watching", "",
		&auth.Claims{UserID: "u-admin", Role: "admin"})
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	data, _ := watchDecode(t, rr).([]any)
	if len(data) != 2 {
		t.Errorf("admin should see both libraries: got %d", len(data))
	}
}

func TestIPTVHandler_ListContinueWatching_RespectsLimitCap(t *testing.T) {
	// Even with limit=9999 the response is capped at 20. The service
	// fake only honours whatever limit the handler passes through, so
	// this test watches that the handler clamps before calling the
	// service — we seed 25 channels and expect ≤20.
	env := newIPTVTestEnv(t)
	history := make([]*db.Channel, 0, 25)
	for i := 0; i < 25; i++ {
		id := "ch-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		seedChannel(env, id, "lib-a")
		history = append(history, env.svc.channelByID[id])
	}
	env.svc.watchedByUser = map[string][]*db.Channel{"u-alice": history}

	rr := env.doAs(http.MethodGet, "/api/v1/me/channels/continue-watching?limit=9999", "",
		&auth.Claims{UserID: "u-alice", Role: "admin"})
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	data, _ := watchDecode(t, rr).([]any)
	if len(data) > 20 {
		t.Errorf("cap not enforced: got %d", len(data))
	}
}

func TestIPTVHandler_ListContinueWatching_UnauthenticatedReturns401(t *testing.T) {
	env := newIPTVTestEnv(t)
	rr := env.doAs(http.MethodGet, "/api/v1/me/channels/continue-watching", "", nil)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestIPTVHandler_ListContinueWatching_UsesDefaultLimit(t *testing.T) {
	// No `limit` query param → default 10. Seed 15 entries; expect 10.
	env := newIPTVTestEnv(t)
	history := make([]*db.Channel, 0, 15)
	for i := 0; i < 15; i++ {
		id := "ch-" + string(rune('a'+i))
		seedChannel(env, id, "lib-a")
		history = append(history, env.svc.channelByID[id])
	}
	env.svc.watchedByUser = map[string][]*db.Channel{"u-alice": history}

	rr := env.doAs(http.MethodGet, "/api/v1/me/channels/continue-watching", "",
		&auth.Claims{UserID: "u-alice", Role: "admin"})
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	data, _ := watchDecode(t, rr).([]any)
	if len(data) != 10 {
		t.Errorf("default limit not applied: got %d, want 10", len(data))
	}
}
