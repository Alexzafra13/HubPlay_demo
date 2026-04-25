package handlers

import (
	"net/http"
	"strings"
	"testing"

	"hubplay/internal/auth"
	"hubplay/internal/db"
)

// resetPlaybackBeacons clears the package-global cooldown map so a
// test can run from a clean slate. The handler keeps the map private
// for production; tests reach in via the same file's package scope.
func resetPlaybackBeacons() {
	playbackBeaconMu.Lock()
	defer playbackBeaconMu.Unlock()
	for k := range playbackBeaconLastAt {
		delete(playbackBeaconLastAt, k)
	}
}

func TestIPTVHandler_RecordPlaybackFailure_HappyPath(t *testing.T) {
	resetPlaybackBeacons()
	env := newIPTVTestEnv(t)
	seedChannel(env, "ch-pf-1", "lib-a")

	rr := env.doAs(http.MethodPost, "/api/v1/channels/ch-pf-1/playback-failure",
		`{"kind":"manifest","details":"hls.js fatal manifest"}`,
		&auth.Claims{UserID: "u-pf-alice", Role: "user"})
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	if got := len(env.svc.recordProbeFailureCalls); got != 1 {
		t.Fatalf("expected 1 RecordProbeFailure call, got %d", got)
	}
	call := env.svc.recordProbeFailureCalls[0]
	if call.ChannelID != "ch-pf-1" {
		t.Errorf("wrong channel: %q", call.ChannelID)
	}
	if call.Err == nil || !strings.Contains(call.Err.Error(), "manifest") {
		t.Errorf("expected error to mention kind, got %v", call.Err)
	}
	if !strings.Contains(call.Err.Error(), "hls.js fatal manifest") {
		t.Errorf("expected error to include details, got %v", call.Err)
	}

	data, _ := iptvDecodeData(t, rr).(map[string]any)
	if data["channel_id"] != "ch-pf-1" {
		t.Errorf("bad channel_id: %v", data["channel_id"])
	}
	if data["recorded"] != true {
		t.Errorf("expected recorded=true, got %v", data["recorded"])
	}
	// counter bumped to 1, threshold is 3 → still "degraded"
	if got := data["health_status"]; got != "degraded" {
		t.Errorf("expected health_status=degraded, got %v", got)
	}
	if data["unhealthy_threshold"] == nil {
		t.Error("missing unhealthy_threshold in response")
	}
}

func TestIPTVHandler_RecordPlaybackFailure_DefaultsToUnknownKind(t *testing.T) {
	resetPlaybackBeacons()
	env := newIPTVTestEnv(t)
	seedChannel(env, "ch-pf-2", "lib-a")

	// Empty body — handler must default kind to "unknown" and accept.
	rr := env.doAs(http.MethodPost, "/api/v1/channels/ch-pf-2/playback-failure", "",
		&auth.Claims{UserID: "u-pf-bob", Role: "user"})
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	if got := len(env.svc.recordProbeFailureCalls); got != 1 {
		t.Fatalf("expected 1 call, got %d", got)
	}
	if !strings.Contains(env.svc.recordProbeFailureCalls[0].Err.Error(), "unknown") {
		t.Errorf("expected synthetic error to mention 'unknown', got %v",
			env.svc.recordProbeFailureCalls[0].Err)
	}
}

func TestIPTVHandler_RecordPlaybackFailure_Unauthenticated(t *testing.T) {
	resetPlaybackBeacons()
	env := newIPTVTestEnv(t)
	seedChannel(env, "ch-pf-3", "lib-a")

	rr := env.doAs(http.MethodPost, "/api/v1/channels/ch-pf-3/playback-failure",
		`{"kind":"network"}`, nil)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
	if len(env.svc.recordProbeFailureCalls) != 0 {
		t.Errorf("beacon recorded without auth: %d calls",
			len(env.svc.recordProbeFailureCalls))
	}
}

func TestIPTVHandler_RecordPlaybackFailure_DenyWithoutLibraryAccess(t *testing.T) {
	resetPlaybackBeacons()
	env := newIPTVTestEnv(t)
	seedChannel(env, "ch-pf-4", "lib-a")
	env.access.accessFn = func(_, _ string) (bool, error) { return false, nil }

	rr := env.doAs(http.MethodPost, "/api/v1/channels/ch-pf-4/playback-failure",
		`{"kind":"network"}`,
		&auth.Claims{UserID: "u-pf-eve", Role: "user"})
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 (deny via NOT_FOUND), got %d", rr.Code)
	}
	if len(env.svc.recordProbeFailureCalls) != 0 {
		t.Errorf("beacon recorded despite ACL deny: %d calls",
			len(env.svc.recordProbeFailureCalls))
	}
}

func TestIPTVHandler_RecordPlaybackFailure_UnknownChannel_500(t *testing.T) {
	resetPlaybackBeacons()
	env := newIPTVTestEnv(t)
	// No seed → fake GetChannel returns a bare error → handleServiceError → 500.
	rr := env.doAs(http.MethodPost, "/api/v1/channels/ch-missing/playback-failure",
		`{"kind":"unknown"}`,
		&auth.Claims{UserID: "u-pf-ghost", Role: "user"})
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
	if len(env.svc.recordProbeFailureCalls) != 0 {
		t.Errorf("beacon recorded against missing channel: %d calls",
			len(env.svc.recordProbeFailureCalls))
	}
}

func TestIPTVHandler_RecordPlaybackFailure_RejectsUnknownKind(t *testing.T) {
	resetPlaybackBeacons()
	env := newIPTVTestEnv(t)
	seedChannel(env, "ch-pf-5", "lib-a")

	rr := env.doAs(http.MethodPost, "/api/v1/channels/ch-pf-5/playback-failure",
		`{"kind":"sql-injection-attempt"}`,
		&auth.Claims{UserID: "u-pf-mal", Role: "user"})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
	if len(env.svc.recordProbeFailureCalls) != 0 {
		t.Errorf("beacon recorded with bad kind: %d calls",
			len(env.svc.recordProbeFailureCalls))
	}
}

func TestIPTVHandler_RecordPlaybackFailure_RejectsMalformedJSON(t *testing.T) {
	resetPlaybackBeacons()
	env := newIPTVTestEnv(t)
	seedChannel(env, "ch-pf-6", "lib-a")

	rr := env.doAs(http.MethodPost, "/api/v1/channels/ch-pf-6/playback-failure",
		`{"kind": not json`,
		&auth.Claims{UserID: "u-pf-bad", Role: "user"})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on malformed JSON, got %d", rr.Code)
	}
	if len(env.svc.recordProbeFailureCalls) != 0 {
		t.Errorf("beacon recorded on bad body: %d calls",
			len(env.svc.recordProbeFailureCalls))
	}
}

func TestIPTVHandler_RecordPlaybackFailure_RejectsUnknownFields(t *testing.T) {
	resetPlaybackBeacons()
	env := newIPTVTestEnv(t)
	seedChannel(env, "ch-pf-7", "lib-a")

	// DisallowUnknownFields is on — extra keys must 400.
	rr := env.doAs(http.MethodPost, "/api/v1/channels/ch-pf-7/playback-failure",
		`{"kind":"network","stack":"<<huge stack trace>>"}`,
		&auth.Claims{UserID: "u-pf-extra", Role: "user"})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on unknown field, got %d", rr.Code)
	}
}

func TestIPTVHandler_RecordPlaybackFailure_CooldownReturns202(t *testing.T) {
	resetPlaybackBeacons()
	env := newIPTVTestEnv(t)
	seedChannel(env, "ch-pf-8", "lib-a")
	claims := &auth.Claims{UserID: "u-pf-flap", Role: "user"}

	rr1 := env.doAs(http.MethodPost, "/api/v1/channels/ch-pf-8/playback-failure",
		`{"kind":"timeout"}`, claims)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first call status %d, want 200", rr1.Code)
	}

	// Second call within the cooldown window: accepted (202) but not recorded.
	rr2 := env.doAs(http.MethodPost, "/api/v1/channels/ch-pf-8/playback-failure",
		`{"kind":"timeout"}`, claims)
	if rr2.Code != http.StatusAccepted {
		t.Fatalf("second call status %d, want 202", rr2.Code)
	}
	if got := len(env.svc.recordProbeFailureCalls); got != 1 {
		t.Errorf("cooldown didn't suppress: got %d calls", got)
	}
	data, _ := iptvDecodeData(t, rr2).(map[string]any)
	if data["recorded"] != false {
		t.Errorf("expected recorded=false on cooldown, got %v", data["recorded"])
	}
	if data["reason"] != "cooldown" {
		t.Errorf("expected reason=cooldown, got %v", data["reason"])
	}
}

func TestIPTVHandler_RecordPlaybackFailure_CooldownIsPerUser(t *testing.T) {
	resetPlaybackBeacons()
	env := newIPTVTestEnv(t)
	seedChannel(env, "ch-pf-9", "lib-a")

	// User A flags the channel.
	rrA := env.doAs(http.MethodPost, "/api/v1/channels/ch-pf-9/playback-failure",
		`{"kind":"network"}`,
		&auth.Claims{UserID: "u-pf-A", Role: "user"})
	if rrA.Code != http.StatusOK {
		t.Fatalf("A status %d", rrA.Code)
	}
	// User B flagging the same channel must be accepted independently.
	rrB := env.doAs(http.MethodPost, "/api/v1/channels/ch-pf-9/playback-failure",
		`{"kind":"network"}`,
		&auth.Claims{UserID: "u-pf-B", Role: "user"})
	if rrB.Code != http.StatusOK {
		t.Fatalf("B status %d (cooldown should be per-user)", rrB.Code)
	}
	if got := len(env.svc.recordProbeFailureCalls); got != 2 {
		t.Errorf("expected 2 calls (one per user), got %d", got)
	}
}

func TestIPTVHandler_RecordPlaybackFailure_DeadStatusAtThreshold(t *testing.T) {
	resetPlaybackBeacons()
	env := newIPTVTestEnv(t)
	seedChannel(env, "ch-pf-10", "lib-a")
	// Pre-load failures so the next bump crosses the threshold.
	env.svc.channelByID["ch-pf-10"].ConsecutiveFailures = db.UnhealthyThreshold - 1

	rr := env.doAs(http.MethodPost, "/api/v1/channels/ch-pf-10/playback-failure",
		`{"kind":"media"}`,
		&auth.Claims{UserID: "u-pf-final", Role: "user"})
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	data, _ := iptvDecodeData(t, rr).(map[string]any)
	if data["health_status"] != "dead" {
		t.Errorf("expected health_status=dead at threshold, got %v", data["health_status"])
	}
}

func TestIPTVHandler_RecordPlaybackFailure_TruncatesLongDetails(t *testing.T) {
	resetPlaybackBeacons()
	env := newIPTVTestEnv(t)
	seedChannel(env, "ch-pf-11", "lib-a")

	// Build a details string that, after JSON-encoding, fits under the
	// 2 KB max-body cap but exceeds the 200-char details slice.
	long := strings.Repeat("X", 300)
	body := `{"kind":"unknown","details":"` + long + `"}`
	rr := env.doAs(http.MethodPost, "/api/v1/channels/ch-pf-11/playback-failure", body,
		&auth.Claims{UserID: "u-pf-long", Role: "user"})
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	if got := len(env.svc.recordProbeFailureCalls); got != 1 {
		t.Fatalf("expected 1 call, got %d", got)
	}
	msg := env.svc.recordProbeFailureCalls[0].Err.Error()
	// "player: unknown (" + ≤200 X's + ")" → way under the original 300.
	if strings.Count(msg, "X") > playbackDetailsMaxLen {
		t.Errorf("details not truncated: error len mentions %d X's", strings.Count(msg, "X"))
	}
}
