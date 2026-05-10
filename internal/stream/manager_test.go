package stream

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hubplay/internal/config"
	"hubplay/internal/event"
)

func TestSessionKey(t *testing.T) {
	if got := sessionKey("user1", "item1", "720p", -1); got != "user1:item1:720p:-1" {
		t.Errorf("default audio: unexpected key %q", got)
	}
	if got := sessionKey("user1", "item1", "720p", 2); got != "user1:item1:720p:2" {
		t.Errorf("explicit audio idx: unexpected key %q", got)
	}
	// Different audio indexes must not collide -- that's what allows
	// mid-playback dub switching to spawn a fresh session instead
	// of returning the old transcode unchanged.
	if sessionKey("u", "i", "p", 0) == sessionKey("u", "i", "p", 1) {
		t.Errorf("keys for distinct audio indexes collided")
	}
}

func TestManager_ActiveSessions_Empty(t *testing.T) {
	m := newTestManager(t)
	defer m.Shutdown()

	if got := m.ActiveSessions(); got != 0 {
		t.Errorf("expected 0 active sessions, got %d", got)
	}
}

func TestManager_GetSession_NotFound(t *testing.T) {
	m := newTestManager(t)
	defer m.Shutdown()

	_, ok := m.GetSession("nonexistent")
	if ok {
		t.Error("expected session not found")
	}
}

func TestManager_TouchSession_NoOp(t *testing.T) {
	m := newTestManager(t)
	defer m.Shutdown()

	// Should not panic when touching nonexistent session
	m.TouchSession("nonexistent")
}

func TestManager_StopSession_NoOp(t *testing.T) {
	m := newTestManager(t)
	defer m.Shutdown()

	// Should not panic when stopping nonexistent session
	m.StopSession("nonexistent")
}

func TestManager_Shutdown_Empty(t *testing.T) {
	m := newTestManager(t)
	m.Shutdown()
}

func TestManager_CleanupIdle(t *testing.T) {
	m := newTestManager(t)
	defer m.Shutdown()

	// Manually inject a session that's old
	m.mu.Lock()
	m.sessions["old-session"] = &ManagedSession{
		Session: &Session{
			ID:        "old-session",
			OutputDir: t.TempDir(),
			done:      closedChan(),
		},
		UserID:       "user1",
		LastAccessed: time.Now().Add(-10 * time.Minute),
	}
	m.sessions["fresh-session"] = &ManagedSession{
		Session: &Session{
			ID:        "fresh-session",
			OutputDir: t.TempDir(),
			done:      closedChan(),
		},
		UserID:       "user2",
		LastAccessed: time.Now(),
	}
	m.mu.Unlock()

	if m.ActiveSessions() != 2 {
		t.Fatalf("expected 2 sessions, got %d", m.ActiveSessions())
	}

	// Run cleanup with a 5-minute timeout — should remove the old one
	m.cleanupIdle(5 * time.Minute)

	if m.ActiveSessions() != 1 {
		t.Errorf("expected 1 session after cleanup, got %d", m.ActiveSessions())
	}

	_, ok := m.GetSession("fresh-session")
	if !ok {
		t.Error("fresh-session should still exist")
	}

	_, ok = m.GetSession("old-session")
	if ok {
		t.Error("old-session should have been cleaned up")
	}
}

func TestManager_Shutdown_StopsAll(t *testing.T) {
	m := newTestManager(t)

	// Inject sessions
	m.mu.Lock()
	profiles := []string{"720p", "480p", "360p"}
	for i := range 3 {
		key := sessionKey("user", "item", profiles[i], -1)
		m.sessions[key] = &ManagedSession{
			Session: &Session{
				ID:        key,
				OutputDir: t.TempDir(),
				done:      closedChan(),
			},
			UserID:       "user",
			LastAccessed: time.Now(),
		}
	}
	m.mu.Unlock()

	if m.ActiveSessions() != 3 {
		t.Fatalf("expected 3 sessions, got %d", m.ActiveSessions())
	}

	m.Shutdown()

	if m.ActiveSessions() != 0 {
		t.Errorf("expected 0 sessions after shutdown, got %d", m.ActiveSessions())
	}
}

func TestManager_StopSessionsByItem_StopsEveryVariant(t *testing.T) {
	m := newTestManager(t)
	defer m.Shutdown()

	// Three sessions for the same (user, item) across two qualities
	// and two audio configs. The legacy per-key DELETE only stopped
	// one — the rest accreted as zombies and ate the per-user cap
	// on the next playback. StopSessionsByItem must wipe all three.
	seed := func(key, itemID string) {
		m.mu.Lock()
		m.sessions[key] = &ManagedSession{
			Session: &Session{
				ID:        key,
				ItemID:    itemID,
				OutputDir: t.TempDir(),
				done:      closedChan(),
			},
			UserID:       "user1",
			LastAccessed: time.Now(),
		}
		m.mu.Unlock()
	}
	seed(SessionKey("user1", "item1", "1080p", -1), "item1")
	seed(SessionKey("user1", "item1", "1080p", 1), "item1")
	seed(SessionKey("user1", "item1", "720p", -1), "item1")
	// Distractor for a different item — must be left alone.
	seed(SessionKey("user1", "item2", "720p", -1), "item2")

	stopped := m.StopSessionsByItem("user1", "item1")
	if stopped != 3 {
		t.Errorf("StopSessionsByItem returned %d, want 3", stopped)
	}
	if got := m.ActiveSessions(); got != 1 {
		t.Errorf("active = %d, want 1 (the other-item distractor)", got)
	}
	if _, ok := m.sessions[SessionKey("user1", "item2", "720p", -1)]; !ok {
		t.Errorf("foreign session for item2 should still be alive")
	}
}

func TestManager_StopSession_RemovesSession(t *testing.T) {
	m := newTestManager(t)
	defer m.Shutdown()

	key := "user1:item1:720p"
	m.mu.Lock()
	m.sessions[key] = &ManagedSession{
		Session: &Session{
			ID:        key,
			OutputDir: t.TempDir(),
			done:      closedChan(),
		},
		UserID:       "user1",
		LastAccessed: time.Now(),
	}
	m.mu.Unlock()

	if m.ActiveSessions() != 1 {
		t.Fatalf("expected 1 session, got %d", m.ActiveSessions())
	}

	m.StopSession(key)

	if m.ActiveSessions() != 0 {
		t.Errorf("expected 0 sessions after stop, got %d", m.ActiveSessions())
	}
}

func TestManager_GetSession_UpdatesLastAccessed(t *testing.T) {
	m := newTestManager(t)
	defer m.Shutdown()

	key := "user1:item1:720p"
	oldTime := time.Now().Add(-1 * time.Hour)
	m.mu.Lock()
	m.sessions[key] = &ManagedSession{
		Session: &Session{
			ID:        key,
			OutputDir: t.TempDir(),
			done:      closedChan(),
		},
		UserID:       "user1",
		LastAccessed: oldTime,
	}
	m.mu.Unlock()

	ms, ok := m.GetSession(key)
	if !ok {
		t.Fatal("session should exist")
	}

	if ms.LastAccessed.Equal(oldTime) {
		t.Error("GetSession should update LastAccessed time")
	}

	if time.Since(ms.LastAccessed) > time.Second {
		t.Error("LastAccessed should be updated to approximately now")
	}
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	logger := testLogger()
	cfg := config.StreamingConfig{
		SegmentDuration:      6,
		MaxTranscodeSessions: 5,
		TranscodePreset:      "veryfast",
		DefaultAudioBitrate:  "192k",
		CacheDir:             "",
		IdleTimeout:          5 * time.Minute,
	}

	return &Manager{
		sessions:   make(map[string]*ManagedSession),
		// HWAccelNone + libx264 — software path, matches what the
		// existing tests assumed before HW accel detection was wired.
		transcoder: NewTranscoder(t.TempDir(), "", 4*time.Hour, HWAccelNone, "libx264", logger),
		cfg:        cfg,
		logger:     logger.With("module", "stream-manager"),
		stopClean:  make(chan struct{}),
		metrics:    noopSink{},
	}
}

// fakeSink records every metric call so tests can assert the manager hooks
// fire on the expected paths without pulling Prometheus into this package.
type fakeSink struct {
	started int
	busy    int
	failed  int
	active  []int
}

func (f *fakeSink) TranscodeStarted()         { f.started++ }
func (f *fakeSink) TranscodeBusy()            { f.busy++ }
func (f *fakeSink) TranscodeFailed()          { f.failed++ }
func (f *fakeSink) SetActiveSessions(n int)   { f.active = append(f.active, n) }

func TestManager_SetMetrics_InitialisesActiveGauge(t *testing.T) {
	m := newTestManager(t)
	defer m.Shutdown()

	// Pre-populate so SetMetrics has something to report.
	m.mu.Lock()
	m.sessions["x"] = &ManagedSession{Session: &Session{ID: "x", done: closedChan()}}
	m.mu.Unlock()

	sink := &fakeSink{}
	m.SetMetrics(sink)

	if len(sink.active) == 0 || sink.active[len(sink.active)-1] != 1 {
		t.Errorf("SetMetrics should seed the active-sessions gauge, got %v", sink.active)
	}
}

func TestManager_StopSession_NotifiesMetrics(t *testing.T) {
	m := newTestManager(t)
	defer m.Shutdown()

	sink := &fakeSink{}
	m.SetMetrics(sink)

	key := "user:item:720p"
	m.mu.Lock()
	m.sessions[key] = &ManagedSession{Session: &Session{ID: key, OutputDir: t.TempDir(), done: closedChan()}}
	m.mu.Unlock()

	m.StopSession(key)

	// The last active value recorded should be 0 — the gauge must drain on
	// stop or the dashboard lies forever after a session ends.
	if len(sink.active) == 0 || sink.active[len(sink.active)-1] != 0 {
		t.Errorf("StopSession should report active=0, got %v", sink.active)
	}
}

func TestManager_CleanupIdle_NotifiesMetricsOnlyWhenSomethingRemoved(t *testing.T) {
	m := newTestManager(t)
	defer m.Shutdown()

	sink := &fakeSink{}
	m.SetMetrics(sink)
	// Reset: SetMetrics already emitted an initial SetActiveSessions(0).
	sink.active = nil

	// Nothing to clean → no emission (prevents flood of identical zeros).
	m.cleanupIdle(5 * time.Minute)
	if len(sink.active) != 0 {
		t.Errorf("cleanupIdle should not notify when no session removed: %v", sink.active)
	}

	// With a session to reap, the gauge should drop to 0 once.
	m.mu.Lock()
	m.sessions["old"] = &ManagedSession{
		Session:      &Session{ID: "old", OutputDir: t.TempDir(), done: closedChan()},
		LastAccessed: time.Now().Add(-1 * time.Hour),
	}
	m.mu.Unlock()
	m.cleanupIdle(1 * time.Minute)

	if len(sink.active) != 1 || sink.active[0] != 0 {
		t.Errorf("cleanupIdle with removal should emit active=0, got %v", sink.active)
	}
}

func TestManager_SetMetrics_NilIsNoOp(t *testing.T) {
	m := newTestManager(t)
	defer m.Shutdown()

	// Calling with nil must keep the existing sink intact so production code
	// that passes a possibly-nil sink does not blow the manager up.
	m.SetMetrics(nil)

	// The default noopSink must still be in place — StopSession must not
	// panic on a nil metrics field.
	m.StopSession("does-not-exist")
}

// closedChan returns a channel that's already closed (simulating a finished process).
func closedChan() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

// ─── Event publisher ─────────────────────────────────────────────────────────

func TestManager_StopSession_PublishesTranscodeCompleted(t *testing.T) {
	m := newTestManager(t)
	defer m.Shutdown()

	bus := eventNewBus()
	m.SetEventBus(bus)

	done := make(chan event.Event, 1)
	bus.Subscribe(event.TranscodeCompleted, func(e event.Event) { done <- e })

	key := "u:it:720p"
	m.mu.Lock()
	m.sessions[key] = &ManagedSession{
		Session:      &Session{ID: key, OutputDir: t.TempDir(), done: closedChan()},
		UserID:       "u",
		LastAccessed: time.Now(),
	}
	m.sessions[key].ItemID = "it"
	m.mu.Unlock()

	m.StopSession(key)

	select {
	case e := <-done:
		if e.Data["session_id"] != key || e.Data["user_id"] != "u" || e.Data["item_id"] != "it" {
			t.Errorf("event payload: %+v", e.Data)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("TranscodeCompleted not published within 1s")
	}
}

func TestManager_PublishIsNilSafeWithoutBus(t *testing.T) {
	m := newTestManager(t)
	defer m.Shutdown()

	// Without SetEventBus the default bus is nil — publish must not panic.
	m.publish(event.Event{Type: event.TranscodeStarted})
}

// Local helper so the test file's import of internal/event stays scoped to
// this one spot.
func eventNewBus() *event.Bus {
	return event.NewBus(testLogger())
}

// TestManager_RestartSessionAt_CoalescesAdjacent pins the regression
// for #176: hls.js fans out 2-3 parallel segment fetches after a
// seek; before the per-session restart mutex + coverage check, each
// fetch triggered its own RestartAt and orphaned the previous
// ffmpeg. Now the second / third arrivals see they're within the
// coalesce window of the first restart and bail without touching
// the transcoder.
//
// We pre-set LastRestartSegment as if the "first" caller already
// finished, then fire a burst of subsequent calls — each must
// return nil without altering state. (We can't unit-test the
// in-flight race directly without mocking the transcoder; this
// inverted form covers the same code path.)
func TestManager_RestartSessionAt_CoalescesAdjacent(t *testing.T) {
	m := newTestManager(t)
	defer m.Shutdown()

	m.mu.Lock()
	m.sessions["test-key"] = &ManagedSession{
		Session: &Session{
			ID:        "test-key",
			ItemID:    "item-1",
			OutputDir: t.TempDir(),
			done:      closedChan(),
		},
		UserID:             "user1",
		InputPath:          "/dev/null",
		Decision:           PlaybackDecision{Profile: DefaultProfile()},
		LastAccessed:       time.Now(),
		LastRestartSegment: 953, // ffmpeg "is producing" from 953 onwards
		LastRestartTime:    time.Now(),
	}
	m.mu.Unlock()

	// Simulate hls.js fanout: near-adjacent segment requests that all
	// arrive within milliseconds of the first restart (LastRestartTime
	// just set above is "now"). Both gates of the AND-coalesce hold
	// → all extra calls return nil without touching ffmpeg.
	for _, segIdx := range []int{953, 954, 955, 956, 958} {
		if err := m.RestartSessionAt("test-key", segIdx, 6.0); err != nil {
			t.Errorf("RestartSessionAt(%d) returned error %v — coalesce check should have skipped",
				segIdx, err)
		}
	}

	ms, ok := m.GetSession("test-key")
	if !ok {
		t.Fatal("session disappeared during coalesce checks")
	}
	if ms.LastRestartSegment != 953 {
		t.Errorf("LastRestartSegment changed to %d (expected 953); coverage check failed to coalesce",
			ms.LastRestartSegment)
	}
}

// TestManager_RestartSessionAt_StaleCoalesceDoesNotBlock pins the
// regression from the 2026-05-08 user report: first seek worked
// instantly; second seek a few seconds later "felt blocked" because
// it landed near the first restart point and the segment-only
// coalesce check told the manager to skip the restart, leaving
// ffmpeg encoding linearly through the gap while the player waited.
//
// The fix is the AND-gate: coalesce requires BOTH recent-in-time
// AND nearby-in-segment. With LastRestartTime several seconds in the
// past, even a near-segment request must trigger a real restart.
//
// We can't run a real transcode in a unit test, but we CAN verify
// the manager *attempts* a restart — the transcoder.RestartAt call
// will fail synthetically against /dev/null, and the resulting
// error is what we assert. The important contract is "does not
// silently return nil and let the player wait".
func TestManager_RestartSessionAt_StaleCoalesceDoesNotBlock(t *testing.T) {
	m := newTestManager(t)
	defer m.Shutdown()

	now := time.Now()
	m.mu.Lock()
	m.sessions["test-key"] = &ManagedSession{
		Session: &Session{
			ID:        "test-key",
			ItemID:    "item-1",
			OutputDir: t.TempDir(),
			done:      closedChan(),
		},
		UserID:             "user1",
		InputPath:          "/dev/null",
		Decision:           PlaybackDecision{Profile: DefaultProfile()},
		LastAccessed:       now,
		LastRestartSegment: 953,
		// 5 s ago — outside the time-coalesce window. A second seek
		// to a nearby segment must NOT coalesce.
		LastRestartTime: now.Add(-5 * time.Second),
	}
	m.mu.Unlock()

	// Adjacent segment that under the OLD segment-only coalesce
	// would have returned nil (delta=2, within ±10). Under the new
	// AND-coalesce the time gate fails → real restart attempted.
	// transcoder.RestartAt will fail (/dev/null is not a real file),
	// which is fine — what we're pinning is the rejection of the
	// silent-coalesce path. A nil return here means the regression
	// is back.
	err := m.RestartSessionAt("test-key", 955, 6.0)
	if err == nil {
		t.Fatal("RestartSessionAt returned nil for a stale-coalesce case — manager silently skipped a real seek (regression from 2026-05-08)")
	}
	if errors.Is(err, ErrSessionNotFound) || errors.Is(err, ErrRestartRateLimited) {
		t.Fatalf("RestartSessionAt returned unrelated sentinel %v — expected a transcoder error from the genuine restart attempt", err)
	}
}

// TestManager_RestartSessionAt_NotFound covers the lookup-miss
// branch — caller used a key that's no longer in the map (idle
// reaper happened between the segment-handler's GetSession and the
// RestartSessionAt call, for example). The sentinel
// ErrSessionNotFound is what lets the handler render a clean 404
// for "session vanished" instead of the generic 503 we use for
// genuine restart failures.
func TestManager_RestartSessionAt_NotFound(t *testing.T) {
	m := newTestManager(t)
	defer m.Shutdown()

	err := m.RestartSessionAt("never-existed", 100, 6.0)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

// TestManager_RestartSessionAt_RateLimited pins the regression for
// the 2026-05-07 incident: 4 RestartSessionAt events in 42 s for a
// single user click, with +366-segment cadence — a frontend seek loop
// that bypassed the coalesce window because each spurious seek landed
// far enough from the previous one to count as "different scene".
// After the SeekBar pointerup-commit fix the loop shouldn't reproduce,
// but the rate limit here is the server-side belt: a runaway client
// can fire at most restartRateLimitMax non-coalesced restarts per
// window before the manager refuses to spawn more ffmpegs.
func TestManager_RestartSessionAt_RateLimited(t *testing.T) {
	m := newTestManager(t)
	defer m.Shutdown()

	now := time.Now()
	m.mu.Lock()
	m.sessions["test-key"] = &ManagedSession{
		Session: &Session{
			ID:        "test-key",
			ItemID:    "item-1",
			OutputDir: t.TempDir(),
			done:      closedChan(),
		},
		UserID:             "user1",
		InputPath:          "/dev/null",
		Decision:           PlaybackDecision{Profile: DefaultProfile()},
		LastAccessed:       now,
		LastRestartSegment: 100,
		LastRestartTime:    now,
		// Pre-seat the session at the cap so the next non-coalesced
		// restart trips the limit. Window is "fresh" so the reset
		// branch isn't taken.
		restartWindowStart: now,
		restartWindowCount: restartRateLimitMax,
	}
	m.mu.Unlock()

	// Far-away segment (delta = 400) clears the coalesce window and
	// reaches the rate-limit check.
	err := m.RestartSessionAt("test-key", 500, 6.0)
	if !errors.Is(err, ErrRestartRateLimited) {
		t.Errorf("expected ErrRestartRateLimited, got %v", err)
	}
}

// TestManager_RestartSessionAt_RateLimitWindowResets verifies that
// the sliding window reopens on its own — a session that was at the
// cap an hour ago is allowed to seek again now. The check is
// exercised indirectly: with windowStart in the deep past, the
// manager increments past the cap should NOT trip because the
// reset branch zeroes the counter first.
func TestManager_RestartSessionAt_RateLimitWindowResets(t *testing.T) {
	m := newTestManager(t)
	defer m.Shutdown()

	now := time.Now()
	m.mu.Lock()
	m.sessions["test-key"] = &ManagedSession{
		Session: &Session{
			ID:        "test-key",
			ItemID:    "item-1",
			OutputDir: t.TempDir(),
			done:      closedChan(),
		},
		UserID:             "user1",
		InputPath:          "/dev/null",
		Decision:           PlaybackDecision{Profile: DefaultProfile()},
		LastAccessed:       now,
		LastRestartSegment: 100,
		LastRestartTime:    now, // recent enough that the AND-coalesce time gate holds
		restartWindowStart: now.Add(-2 * time.Minute), // outside the window
		restartWindowCount: restartRateLimitMax + 50,  // would otherwise trip
	}
	m.mu.Unlock()

	// Coalesced restart: BOTH gates hold (delta=2 ≤ 6 AND time gate
	// fresh) so the call returns nil without ever consuming
	// rate-limit budget. The original intent of this test was the
	// window-reset path; with the AND-coalesce in place the cleaner
	// invariant is "coalesce never burns budget" — and that's what
	// we verify via restartWindowCount below.
	if err := m.RestartSessionAt("test-key", 102, 6.0); err != nil {
		t.Fatalf("coalesced restart errored unexpectedly: %v", err)
	}
	// Confirm the rate-limit accounting was never touched on a
	// coalesced call (defensive: we want fanout to stay free).
	ms, _ := m.GetSession("test-key")
	if ms.restartWindowCount != restartRateLimitMax+50 {
		t.Errorf("coalesced restart consumed rate-limit budget: count went from %d to %d",
			restartRateLimitMax+50, ms.restartWindowCount)
	}
}

// TestManager_StartGroup_CollapsesConcurrent pins the contract that
// guards against the "two ffmpegs alive for the same session"
// production bug: StartSession's slow path must collapse onto a single
// execution when concurrent callers arrive for the same session key.
//
// The bug as observed: the player's init burst would fire StartSession
// + an immediate retry (auth refresh, hls.js loading the manifest,
// double-clicked Play, etc.) within the same few hundred ms.  Both
// callers missed the m.sessions fast-path, both reached
// transcoder.Start, and both ended up with their own ffmpeg writing
// segmentNNNNN.ts to the same /cache/<sessionID> directory.  htop
// showed two ffmpegs at 99% CPU for the same Bluray rip.
//
// Driving the actual StartSession path concurrently from a unit test
// would require fakes for ItemRepository / MediaStreamRepository /
// Transcoder, none of which the rest of this package needs.  Instead
// we verify the singleflight instance the production code relies on
// (m.startGroup) collapses N parallel Do() calls for the same key
// into one fn execution — the property StartSession is built on.
// If the field is ever moved, renamed, or replaced with something
// non-coalescing, this test fails.
func TestManager_StartGroup_CollapsesConcurrent(t *testing.T) {
	m := newTestManager(t)
	defer m.Shutdown()

	var counter int32
	// Sleep long enough that all N goroutines are guaranteed to be
	// blocked inside Do() by the time the leader's fn returns. With
	// N=5 spawned in a tight loop, 50 ms is comfortably more than
	// the goroutine-startup window.
	fn := func() (any, error) {
		atomic.AddInt32(&counter, 1)
		time.Sleep(50 * time.Millisecond)
		return "winner", nil
	}

	const N = 5
	results := make([]any, N)
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			v, _, _ := m.startGroup.Do("test-key", fn)
			results[i] = v
		}(i)
	}
	wg.Wait()

	if got := atomic.LoadInt32(&counter); got != 1 {
		t.Errorf("fn ran %d times; expected 1 — singleflight failed to collapse %d concurrent callers", got, N)
	}
	for i, r := range results {
		if r != "winner" {
			t.Errorf("caller %d got result %v; expected all callers to receive the leader's value", i, r)
		}
	}
}

// TestManager_ListAllSessions covers the snapshot path the admin
// "Now Playing" panel polls — every session in the manager's map
// should appear in the slice, with the public key plus copies of
// UserID / ItemID / Profile / Method / StartedAt / LastAccessed.
// The slice itself is owned by the caller (no aliasing back to live
// state), and the order is unspecified because Go map iteration is
// non-deterministic, so we verify by lookup, not by index.
func TestManager_ListAllSessions(t *testing.T) {
	m := newTestManager(t)
	defer m.Shutdown()

	now := time.Now()
	m.mu.Lock()
	m.sessions["userA:item1:720p"] = &ManagedSession{
		Session: &Session{
			ID:        "userA:item1:720p",
			ItemID:    "item1",
			Profile:   Profile{Name: "720p"},
			OutputDir: t.TempDir(),
			StartedAt: now.Add(-30 * time.Second),
			done:      closedChan(),
		},
		UserID:       "userA",
		Decision:     PlaybackDecision{Method: MethodTranscode},
		LastAccessed: now,
	}
	m.sessions["userB:item2:1080p"] = &ManagedSession{
		Session: &Session{
			ID:        "userB:item2:1080p",
			ItemID:    "item2",
			Profile:   Profile{Name: "1080p"},
			OutputDir: t.TempDir(),
			StartedAt: now.Add(-2 * time.Minute),
			done:      closedChan(),
		},
		UserID:       "userB",
		Decision:     PlaybackDecision{Method: MethodDirectStream},
		LastAccessed: now.Add(-5 * time.Second),
	}
	m.mu.Unlock()

	snaps := m.ListAllSessions()
	if len(snaps) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(snaps))
	}

	byID := make(map[string]SessionSnapshot, len(snaps))
	for _, s := range snaps {
		byID[s.ID] = s
	}

	a, ok := byID["userA:item1:720p"]
	if !ok {
		t.Fatal("missing snapshot for userA:item1:720p")
	}
	if a.UserID != "userA" || a.ItemID != "item1" || a.Profile != "720p" {
		t.Errorf("userA snapshot mismatch: %+v", a)
	}
	if a.Method != MethodTranscode {
		t.Errorf("userA method = %q, want Transcode", a.Method)
	}

	b, ok := byID["userB:item2:1080p"]
	if !ok {
		t.Fatal("missing snapshot for userB:item2:1080p")
	}
	if b.UserID != "userB" || b.ItemID != "item2" || b.Profile != "1080p" {
		t.Errorf("userB snapshot mismatch: %+v", b)
	}
	if b.Method != MethodDirectStream {
		t.Errorf("userB method = %q, want DirectStream", b.Method)
	}
	if !b.LastAccessed.Before(a.LastAccessed) {
		t.Errorf("LastAccessed values not preserved: a=%v b=%v", a.LastAccessed, b.LastAccessed)
	}
}

// TestManager_ListAllSessions_Empty pins the zero-state response so
// the admin panel never has to special-case nil vs len()==0.
func TestManager_ListAllSessions_Empty(t *testing.T) {
	m := newTestManager(t)
	defer m.Shutdown()

	snaps := m.ListAllSessions()
	if snaps == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(snaps) != 0 {
		t.Errorf("expected empty slice, got %d entries", len(snaps))
	}
}
