package fedhandler

import (
	"context"
	"errors"
	"strings"
	"sync"

	"hubplay/internal/api/handlers"
	"hubplay/internal/domain"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/stream"
)

// streamFakeItemRepo is a minimal fake for handlers.ItemRepository.
type streamFakeItemRepo struct {
	byID map[string]*librarymodel.Item
}

func (r *streamFakeItemRepo) GetByID(_ context.Context, id string) (*librarymodel.Item, error) {
	if it, ok := r.byID[id]; ok {
		return it, nil
	}
	return nil, domain.NewNotFound("item")
}

func (r *streamFakeItemRepo) List(_ context.Context, _ librarymodel.ItemFilter) ([]*librarymodel.Item, int, error) {
	return nil, 0, nil
}

var _ handlers.ItemRepository = (*streamFakeItemRepo)(nil)

// fakeStreamManager is a minimal fake for handlers.StreamManagerService.
type fakeStreamManager struct {
	mu             sync.Mutex
	startSessionFn func(ctx context.Context, req stream.StartSessionRequest) (*stream.ManagedSession, error)
	sessions       map[string]*stream.ManagedSession
	stopped        map[string]bool
}

func newFakeStreamManager() *fakeStreamManager {
	return &fakeStreamManager{
		sessions: map[string]*stream.ManagedSession{},
		stopped:  map[string]bool{},
	}
}

func (m *fakeStreamManager) StartSession(ctx context.Context, req stream.StartSessionRequest) (*stream.ManagedSession, error) {
	if m.startSessionFn != nil {
		return m.startSessionFn(ctx, req)
	}
	return nil, errors.New("not configured")
}

func (m *fakeStreamManager) GetSession(key string) (*stream.ManagedSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[key]
	return s, ok
}

func (m *fakeStreamManager) RestartSessionAt(_ string, _ int, _ float64) error {
	return nil
}

func (m *fakeStreamManager) StopSession(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopped[key] = true
}

func (m *fakeStreamManager) StopSessionsByItem(userID, itemID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := userID + ":" + itemID + ":"
	count := 0
	for k := range m.sessions {
		if strings.HasPrefix(k, prefix) {
			m.stopped[k] = true
			count++
		}
	}
	m.stopped[prefix] = true
	return count
}

func (m *fakeStreamManager) ActiveSessions() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

var _ handlers.StreamManagerService = (*fakeStreamManager)(nil)
