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

// SetSessionForTest inserta atómicamente una ManagedSession en el mapa interno.
// key debe ser el session map key (userID:itemID:profileName).
func SetSessionForTest(m *Manager, key string, ms *ManagedSession) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[key] = ms
}

// NewClosedSessionForTest devuelve una Session con canal `done` ya cerrado
// para que Stop() retorne inmediato (sin el timeout de 5s).
// outputDir debería ser t.TempDir(); "" también funciona (RemoveAll no-op).
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
