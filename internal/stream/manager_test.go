package stream

import (
	"testing"
	"time"

	"hubplay/internal/config"
	"hubplay/internal/event"
)

func TestSessionKey(t *testing.T) {
	key := sessionKey("user1", "item1", "720p")
	if key != "user1:item1:720p" {
		t.Errorf("unexpected key: %s", key)
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
		key := sessionKey("user", "item", profiles[i])
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
		transcoder: NewTranscoder(t.TempDir(), "", 4*time.Hour, logger),
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
