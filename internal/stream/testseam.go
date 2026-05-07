package stream

import (
	"log/slog"
	"time"

	"hubplay/internal/config"
)

// This file exposes hooks intended for tests that live OUTSIDE the
// stream package (e.g. internal/api/handlers/admin_streams_test.go).
// Tests inside the stream package itself can — and do — touch
// unexported fields directly via newTestManager / m.mu.Lock(). The
// helpers below are the public counterpart: same building blocks,
// reachable across package boundaries, with names that telegraph the
// "do not call this from production" intent.

// NewManagerForTest builds a minimal Manager without running hardware
// acceleration detection, validating ffmpeg, wiring the cleanup
// loop, or attaching repositories. The returned manager supports
// every read API (ActiveSessions, ListAllSessions, GetSession, etc.)
// and StopSession, but StartSession will panic — by design — because
// the transcoder is not wired.
//
// The intended caller is an external test that wants to inject
// pre-built ManagedSession values via SetSessionForTest and then
// exercise the read or kill paths.
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
