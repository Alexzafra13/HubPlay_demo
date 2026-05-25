package stream

import (
	"log/slog"
	"time"

	"hubplay/internal/config"
)

// Hooks para tests fuera del paquete stream (ej. admin_streams_test.go).
// Tests dentro del paquete usan campos no exportados directamente.

// NewManagerForTest construye un Manager mínimo sin detección de HW,
// validación de ffmpeg ni loop de limpieza. Soporta APIs de lectura
// y StopSession; StartSession paniquea por diseño.
func NewManagerForTest() *Manager {
	return &Manager{
		sessions:  make(map[string]*ManagedSession),
		cfg:       config.StreamingConfig{MaxTranscodeSessions: 8},
		logger:    slog.Default().With("module", "stream-manager-test"),
		stopClean: make(chan struct{}),
		metrics:   noopSink{},
	}
}

// SetSessionForTest inserta atómicamente una ManagedSession en el map
// interno del manager. Permite a tests cross-package sembrar sesiones
// sin arrancar un pipeline ffmpeg real.
func SetSessionForTest(m *Manager, key string, ms *ManagedSession) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[key] = ms
}

// NewClosedSessionForTest devuelve una Session con `done` ya cerrado,
// para que Stop() retorne inmediatamente en tests. outputDir debe ser
// un t.TempDir().
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
