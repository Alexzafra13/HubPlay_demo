package stream

import (
	"testing"
	"time"

	"hubplay/internal/config"
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
		transcoder: NewTranscoder(t.TempDir(), "", logger),
		cfg:        cfg,
		logger:     logger.With("module", "stream-manager"),
		stopClean:  make(chan struct{}),
	}
}

// closedChan returns a channel that's already closed (simulating a finished process).
func closedChan() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
