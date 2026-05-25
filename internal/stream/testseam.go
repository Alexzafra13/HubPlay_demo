package stream

import (
	"log/slog"
	"time"

	"hubplay/internal/config"
)

// Hooks exportados para tests cross-package (e.g. admin_streams_test.go).
// Tests internos usan campos no-exportados directamente.

// NewManagerForTest crea un Manager mínimo sin detección HW, ffmpeg ni cleanup loop.
// Soporta read APIs y StopSession; StartSession panics (transcoder no conectado).
// Para tests que inyectan sesiones via SetSessionForTest.
func NewManagerForTest() *Manager {
	return &Manager{
		sessions:  make(map[string]*ManagedSession),
		cfg:       config.StreamingConfig{MaxTranscodeSessions: 8},
		logger:    slog.Default().With("module", "stream-manager-test"),
		stopClean: make(chan struct{}),
		metrics:   noopSink{},
	}
}

// SetSessionForTest atomically inserts a ManagedSession into the
// manager's internal map under the given key. This is the same
// mutation StartSession performs internally; lifting it as an
// exported test seam lets cross-package tests seed sessions without
// having to spin up a real ffmpeg pipeline.
//
// The key must be the same string that StopSession expects, i.e. the
// session map key (see sessionKey() — userID:itemID:profileName).
func SetSessionForTest(m *Manager, key string, ms *ManagedSession) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[key] = ms
}

// NewClosedSessionForTest returns a Session whose `done` channel is
// already closed, so a subsequent call to Stop() returns immediately
// rather than waiting the 5-second timeout. Intended for tests that
// inject sessions via SetSessionForTest and then call StopSession /
// KillSession — without the pre-closed channel each kill in a tight
// loop would burn ~5s of wall-clock time.
//
// outputDir should be a t.TempDir() so Session.Stop's RemoveAll has
// something safe to delete; passing "" still works (RemoveAll on an
// empty path is a no-op).
func NewClosedSessionForTest(id, itemID, profileName, outputDir string, startedAt time.Time) *Session {
	done := make(chan struct{})
	close(done)
	return &Session{
		ID:        id,
		ItemID:    itemID,
		Profile:   Profile{Name: profileName},
		OutputDir: outputDir,
		StartedAt: startedAt,
		done:      done,
	}
}
