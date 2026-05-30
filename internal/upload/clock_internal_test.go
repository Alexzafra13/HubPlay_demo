package upload

import (
	"context"
	"log/slog"
	"testing"
	"time"

	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/clock"
	"hubplay/internal/event"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/probe"
)

// Tests internos para verificar la inyección de clock. En `package upload`
// (no `_test`) porque los fields `clock` son privados y la observabilidad
// pública aún no expone el reloj.

func TestNewService_DefaultsClockToReal(t *testing.T) {
	svc := NewService(
		DefaultConfig(),
		mustStaging(t),
		dummyUserStore{},
		dummyAuditStore{},
		dummyBus{},
		NewLibraryPicker(dummyLibStore{}),
		dummyProber{},
		nil, // clk default
		slog.Default(),
	)
	if svc.clock == nil {
		t.Fatal("Service.clock should default to clock.New(), got nil")
	}
	diff := time.Since(svc.clock.Now())
	if diff < 0 || diff > time.Second {
		t.Errorf("default clock.Now() should be ~time.Now(); got delta %v", diff)
	}
}

func TestNewService_UsesInjectedClock(t *testing.T) {
	fixed := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	mock := &clock.Mock{CurrentTime: fixed}
	svc := NewService(
		DefaultConfig(),
		mustStaging(t),
		dummyUserStore{},
		dummyAuditStore{},
		dummyBus{},
		NewLibraryPicker(dummyLibStore{}),
		dummyProber{},
		mock,
		slog.Default(),
	)
	if !svc.clock.Now().Equal(fixed) {
		t.Errorf("injected clock not used: got %v, want %v", svc.clock.Now(), fixed)
	}
}

func TestNewGC_UsesInjectedClock(t *testing.T) {
	fixed := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	mock := &clock.Mock{CurrentTime: fixed}
	gc := NewGC(mustStaging(t), time.Hour, 24*time.Hour, mock, slog.Default())
	if !gc.clock.Now().Equal(fixed) {
		t.Errorf("injected clock not used: got %v, want %v", gc.clock.Now(), fixed)
	}
}

// ─── Stubs mínimos para los tests de clock injection ────────────────

func mustStaging(t *testing.T) *StagingDir {
	t.Helper()
	s, err := NewStagingDir(t.TempDir())
	if err != nil {
		t.Fatalf("NewStagingDir: %v", err)
	}
	return s
}

type dummyUserStore struct{}

func (dummyUserStore) GetByID(_ context.Context, _ string) (*authmodel.User, error) {
	return nil, nil
}
func (dummyUserStore) ReserveUploadBytes(_ context.Context, _ string, _ int64) error { return nil }
func (dummyUserStore) ReleaseUploadBytes(_ context.Context, _ string, _ int64) error { return nil }

type dummyAuditStore struct{}

func (dummyAuditStore) Insert(_ context.Context, _ AuditRow) error { return nil }

type dummyBus struct{}

func (dummyBus) Publish(_ event.Event) {}

type dummyProber struct{}

func (dummyProber) Probe(_ context.Context, _ string) (*probe.Result, error) {
	return nil, nil
}

type dummyLibStore struct{}

func (dummyLibStore) GetByID(_ context.Context, _ string) (*librarymodel.Library, error) {
	return nil, nil
}
func (dummyLibStore) ListForUser(_ context.Context, _ string) ([]*librarymodel.Library, error) {
	return nil, nil
}
