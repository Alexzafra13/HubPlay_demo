package stream

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"hubplay/internal/config"
	"hubplay/internal/db"
	"hubplay/internal/domain"
	"hubplay/internal/event"
)

// MetricsSink is the minimal observability surface the Manager uses. Keeping
// it local (instead of importing an observability package) avoids a package
// cycle and lets tests pass a nil-safe sink without Prometheus in the mix.
type MetricsSink interface {
	TranscodeStarted()
	TranscodeBusy()
	TranscodeFailed()
	SetActiveSessions(n int)
}

// noopSink is the default implementation used when no metrics are wired in.
// Using a value type with empty methods lets the Manager call metrics.* on
// every hot path without nil checks.
type noopSink struct{}

func (noopSink) TranscodeStarted()       {}
func (noopSink) TranscodeBusy()          {}
func (noopSink) TranscodeFailed()        {}
func (noopSink) SetActiveSessions(n int) {}

// Manager orchestrates streaming sessions (direct play, remux, and transcode).
type Manager struct {
	mu         sync.Mutex
	sessions   map[string]*ManagedSession
	transcoder *Transcoder
	items      *db.ItemRepository
	streams    *db.MediaStreamRepository
	cfg        config.StreamingConfig
	logger     *slog.Logger
	stopClean  chan struct{}
	metrics    MetricsSink
	bus        *event.Bus // optional; nil-safe
	// hwAccel is the snapshot of accelerator detection done at startup.
	// Cached here so the admin /system/stats endpoint can read it without
	// re-running ffmpeg on every poll. Zero value means "no detection
	// performed" (HWAccel.Enabled = false in config).
	hwAccel HWAccelResult
}

// ManagedSession wraps a transcoding session with access tracking.
type ManagedSession struct {
	*Session
	UserID       string
	Decision     PlaybackDecision
	LastAccessed time.Time
}

// NewManager creates a streaming manager.
func NewManager(
	items *db.ItemRepository,
	streams *db.MediaStreamRepository,
	cfg config.StreamingConfig,
	logger *slog.Logger,
) *Manager {
	// Single source of truth for the cache directory — preflight checks
	// use the same helper so "the cache dir" means the same thing in both
	// places.
	cacheDir := cfg.EffectiveCacheDir()

	// Detect hardware acceleration once at construction. Detection is
	// fast (< 50 ms on a warm system) and the result is read on every
	// transcode session, so doing it inline here keeps the wiring at
	// a single point. When `Enabled = false` we skip detection
	// entirely and fall back to libx264 — matches a deliberately-
	// configured "force software" deployment.
	hwAccel := HWAccelNone
	encoder := "libx264"
	hwResult := HWAccelResult{Selected: HWAccelNone, Encoder: "libx264"}
	if cfg.HWAccel.Enabled {
		hwResult = DetectHWAccel(cfg.HWAccel.Preferred, logger)
		hwAccel = hwResult.Selected
		encoder = hwResult.Encoder
	}

	m := &Manager{
		sessions:   make(map[string]*ManagedSession),
		transcoder: NewTranscoder(cacheDir, "", cfg.TranscodeTimeout, hwAccel, encoder, logger),
		items:      items,
		streams:    streams,
		cfg:        cfg,
		logger:     logger.With("module", "stream-manager"),
		stopClean:  make(chan struct{}),
		metrics:    noopSink{},
		hwAccel:    hwResult,
	}

	go m.cleanupLoop()
	return m
}

// SetMetrics wires an observability sink into the manager. Passing nil is a
// no-op (the default noopSink stays in place) so callers never have to
// short-circuit in production code.
func (m *Manager) SetMetrics(sink MetricsSink) {
	if sink == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metrics = sink
	m.metrics.SetActiveSessions(len(m.sessions))
}

// SetEventBus wires an event bus so the manager can publish lifecycle events
// (TranscodeStarted / TranscodeCompleted). Passing nil disables publishing.
// Follows the SetMetrics pattern so the constructor signature stays stable.
func (m *Manager) SetEventBus(bus *event.Bus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bus = bus
}

// publish sends an event if a bus is wired. Reads m.bus under the mutex to
// stay race-free with SetEventBus.
func (m *Manager) publish(e event.Event) {
	m.mu.Lock()
	bus := m.bus
	m.mu.Unlock()
	if bus != nil {
		bus.Publish(e)
	}
}

// sessionKey builds a unique key for a user+item+profile combination.
func sessionKey(userID, itemID, profile string) string {
	return userID + ":" + itemID + ":" + profile
}

// StartSession creates or returns an existing session for the given item.
func (m *Manager) StartSession(ctx context.Context, userID, itemID, profileName string, startTime float64) (*ManagedSession, error) {
	key := sessionKey(userID, itemID, profileName)

	m.mu.Lock()
	// Return existing session if available
	if ms, ok := m.sessions[key]; ok {
		ms.LastAccessed = time.Now()
		m.mu.Unlock()
		return ms, nil
	}

	// Check concurrent limit
	if m.cfg.MaxTranscodeSessions > 0 && len(m.sessions) >= m.cfg.MaxTranscodeSessions {
		active := len(m.sessions)
		m.mu.Unlock()
		m.metrics.TranscodeBusy()
		return nil, domain.NewTranscodeBusy(active, m.cfg.MaxTranscodeSessions)
	}
	m.mu.Unlock()

	// Fetch item and its streams
	item, err := m.items.GetByID(ctx, itemID)
	if err != nil {
		m.metrics.TranscodeFailed()
		return nil, fmt.Errorf("get item: %w", err)
	}

	mediaStreams, err := m.streams.ListByItem(ctx, itemID)
	if err != nil {
		m.metrics.TranscodeFailed()
		return nil, fmt.Errorf("get streams: %w", err)
	}

	decision := Decide(item, mediaStreams, profileName)

	// Direct play doesn't need a transcode session
	if decision.Method == MethodDirectPlay {
		ms := &ManagedSession{
			Session: &Session{
				ID:        key,
				ItemID:    itemID,
				StartedAt: time.Now(),
			},
			UserID:       userID,
			Decision:     decision,
			LastAccessed: time.Now(),
		}
		// No lock needed — direct play sessions are not tracked
		return ms, nil
	}

	// Start transcode/remux session
	session, err := m.transcoder.Start(key, itemID, item.Path, decision.Profile, startTime)
	if err != nil {
		m.metrics.TranscodeFailed()
		return nil, fmt.Errorf("start transcode: %w", err)
	}

	ms := &ManagedSession{
		Session:      session,
		UserID:       userID,
		Decision:     decision,
		LastAccessed: time.Now(),
	}

	m.mu.Lock()
	m.sessions[key] = ms
	active := len(m.sessions)
	m.mu.Unlock()

	m.metrics.TranscodeStarted()
	m.metrics.SetActiveSessions(active)

	m.publish(event.Event{
		Type: event.TranscodeStarted,
		Data: map[string]any{
			"session_id": key,
			"user_id":    userID,
			"item_id":    itemID,
			"profile":    decision.Profile.Name,
			"method":     string(decision.Method),
		},
	})

	m.logger.Info("session started",
		"key", key,
		"method", decision.Method,
		"profile", decision.Profile.Name,
	)

	return ms, nil
}

// TouchSession updates the last accessed time for a session.
func (m *Manager) TouchSession(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ms, ok := m.sessions[key]; ok {
		ms.LastAccessed = time.Now()
	}
}

// StopSession stops a specific session.
func (m *Manager) StopSession(key string) {
	m.mu.Lock()
	ms, ok := m.sessions[key]
	if ok {
		delete(m.sessions, key)
	}
	active := len(m.sessions)
	m.mu.Unlock()

	if ok {
		ms.Stop()
		m.metrics.SetActiveSessions(active)
		m.publish(event.Event{
			Type: event.TranscodeCompleted,
			Data: map[string]any{
				"session_id": key,
				"user_id":    ms.UserID,
				"item_id":    ms.ItemID,
			},
		})
		m.logger.Info("session stopped", "key", key)
	}
}

// GetSession returns a managed session by key.
func (m *Manager) GetSession(key string) (*ManagedSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ms, ok := m.sessions[key]
	if ok {
		ms.LastAccessed = time.Now()
	}
	return ms, ok
}

// ActiveSessions returns the count of active transcode sessions.
func (m *Manager) ActiveSessions() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// MaxTranscodeSessions returns the configured concurrent transcode cap (0
// means unlimited). Read by admin endpoints to render "X of Y in use".
func (m *Manager) MaxTranscodeSessions() int {
	return m.cfg.MaxTranscodeSessions
}

// HWAccelInfo returns the accelerator snapshot computed at construction.
// Zero-value when HW acceleration is disabled in config.
func (m *Manager) HWAccelInfo() HWAccelResult {
	return m.hwAccel
}

// HWAccelEnabled reports whether the operator turned HW acceleration on
// in config. Distinct from HWAccelInfo() because "enabled but no
// accelerators detected" is a different (and actionable) state from
// "disabled in config" — the admin panel renders different copy for each.
func (m *Manager) HWAccelEnabled() bool {
	return m.cfg.HWAccel.Enabled
}

// CacheDir returns the resolved transcode cache directory. Useful for the
// admin storage panel — same value the manager passes to the transcoder so
// the operator sees what's actually in use, not a stale config value.
func (m *Manager) CacheDir() string {
	return m.cfg.EffectiveCacheDir()
}

// Shutdown stops all sessions and the cleanup loop.
func (m *Manager) Shutdown() {
	close(m.stopClean)

	m.mu.Lock()
	sessions := make([]*ManagedSession, 0, len(m.sessions))
	for _, ms := range m.sessions {
		sessions = append(sessions, ms)
	}
	m.sessions = make(map[string]*ManagedSession)
	m.mu.Unlock()

	for _, ms := range sessions {
		ms.Stop()
	}
	m.metrics.SetActiveSessions(0)
	m.logger.Info("stream manager shut down", "stopped_sessions", len(sessions))
}

// cleanupLoop periodically removes idle sessions.
func (m *Manager) cleanupLoop() {
	idleTimeout := m.cfg.IdleTimeout
	if idleTimeout <= 0 {
		idleTimeout = 5 * time.Minute
	}

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopClean:
			return
		case <-ticker.C:
			m.cleanupIdle(idleTimeout)
		}
	}
}

func (m *Manager) cleanupIdle(maxIdle time.Duration) {
	now := time.Now()
	var toRemove []string
	var toStop []*ManagedSession

	m.mu.Lock()
	for key, ms := range m.sessions {
		if now.Sub(ms.LastAccessed) > maxIdle {
			toRemove = append(toRemove, key)
			toStop = append(toStop, ms)
			delete(m.sessions, key)
		}
	}
	active := len(m.sessions)
	m.mu.Unlock()

	for i, ms := range toStop {
		ms.Stop()
		m.transcoder.Stop(toRemove[i])
		m.logger.Info("cleaned up idle session", "key", toRemove[i])
	}

	if len(toStop) > 0 {
		m.metrics.SetActiveSessions(active)
	}
}
