package stream

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"hubplay/internal/config"
	"hubplay/internal/db"
)

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
	cacheDir := cfg.CacheDir
	if cacheDir == "" {
		home, _ := os.UserHomeDir()
		cacheDir = filepath.Join(home, ".hubplay", "cache", "transcode")
	}

	m := &Manager{
		sessions:   make(map[string]*ManagedSession),
		transcoder: NewTranscoder(cacheDir, "", logger),
		items:      items,
		streams:    streams,
		cfg:        cfg,
		logger:     logger.With("module", "stream-manager"),
		stopClean:  make(chan struct{}),
	}

	go m.cleanupLoop()
	return m
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
		m.mu.Unlock()
		return nil, fmt.Errorf("max transcode sessions (%d) reached", m.cfg.MaxTranscodeSessions)
	}
	m.mu.Unlock()

	// Fetch item and its streams
	item, err := m.items.GetByID(ctx, itemID)
	if err != nil {
		return nil, fmt.Errorf("get item: %w", err)
	}

	mediaStreams, err := m.streams.ListByItem(ctx, itemID)
	if err != nil {
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
	m.mu.Unlock()

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
	m.mu.Unlock()

	if ok {
		ms.Stop()
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

	m.mu.Lock()
	for key, ms := range m.sessions {
		if now.Sub(ms.LastAccessed) > maxIdle {
			toRemove = append(toRemove, key)
		}
	}
	for _, key := range toRemove {
		delete(m.sessions, key)
	}
	m.mu.Unlock()

	for _, key := range toRemove {
		// Session already removed from map; stop the process
		if ms, ok := m.sessions[key]; ok {
			ms.Stop()
		}
		m.logger.Info("cleaned up idle session", "key", key)
	}

	// Also clean up the transcoder's own sessions
	if len(toRemove) > 0 {
		for _, key := range toRemove {
			m.transcoder.Stop(key)
		}
	}
}
