package audit_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http/httptest"
	"sync"
	"testing"

	"hubplay/internal/audit"
	"hubplay/internal/auth"
)

type fakeStore struct {
	mu        sync.Mutex
	rows      []audit.LogRow
	insertErr error
}

func (f *fakeStore) Insert(_ context.Context, row audit.LogRow) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.insertErr != nil {
		return f.insertErr
	}
	f.rows = append(f.rows, row)
	return nil
}

func (f *fakeStore) last() audit.LogRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.rows) == 0 {
		return audit.LogRow{}
	}
	return f.rows[len(f.rows)-1]
}

func newSvc() (*audit.Service, *fakeStore) {
	st := &fakeStore{}
	return audit.NewService(st, slog.Default()), st
}

// ─── Field plumbing ─────────────────────────────────────────────────

func TestService_LogAuthLogin_RecordsActorAndPayload(t *testing.T) {
	svc, store := newSvc()
	req := httptest.NewRequest("POST", "/auth/login", nil)
	req.Header.Set("User-Agent", "TestUA/1.0")
	req.RemoteAddr = "10.0.0.5:12345"

	svc.LogAuthLogin(req.Context(), req, "u-alex", "alex")

	got := store.last()
	if got.EventType != "auth.login.ok" {
		t.Errorf("event_type = %q", got.EventType)
	}
	if got.ActorUserID != "u-alex" {
		t.Errorf("actor = %q", got.ActorUserID)
	}
	if got.IPAddress != "10.0.0.5:12345" {
		t.Errorf("ip = %q", got.IPAddress)
	}
	if got.UserAgent != "TestUA/1.0" {
		t.Errorf("ua = %q", got.UserAgent)
	}
	if got.ID == "" {
		t.Error("id was not generated")
	}
}

func TestService_PayloadMarshalsToJSON(t *testing.T) {
	svc, store := newSvc()
	svc.LogPermissionChanged(context.Background(), nil, "u-target", map[string]bool{
		"can_edit_metadata": true,
		"can_view_audit":    false,
	})

	got := store.last()
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got.Payload), &parsed); err != nil {
		t.Fatalf("payload not JSON: %v (raw=%q)", err, got.Payload)
	}
	changes, ok := parsed["changes"].(map[string]any)
	if !ok {
		t.Fatalf("changes wrong shape: %v", parsed)
	}
	if changes["can_edit_metadata"] != true {
		t.Errorf("can_edit_metadata = %v", changes["can_edit_metadata"])
	}
}

func TestService_ActorFromContext(t *testing.T) {
	svc, store := newSvc()
	// Sin request: el actor viene de las claims del context.
	ctx := auth.WithClaims(context.Background(), &auth.Claims{UserID: "u-from-ctx"})
	svc.LogSystemRestart(ctx, nil, "manual")

	got := store.last()
	if got.ActorUserID != "u-from-ctx" {
		t.Errorf("actor = %q, want u-from-ctx", got.ActorUserID)
	}
}

func TestService_FailedLoginHasNoActor(t *testing.T) {
	svc, store := newSvc()
	req := httptest.NewRequest("POST", "/auth/login", nil)
	svc.LogAuthLoginFailed(req.Context(), req, "alex", "bad_password")

	got := store.last()
	if got.ActorUserID != "" {
		t.Errorf("failed login should have empty actor, got %q", got.ActorUserID)
	}
	if got.EventType != "auth.login.failed" {
		t.Error("event type wrong")
	}
}

// ─── Resilience ────────────────────────────────────────────────────

// TestService_InsertFailureIsSwallowed pin la decisión "fire-and-
// forget". Un INSERT fallido NO debe propagar al caller — si el
// audit falla por DB lock o disco lleno, el flujo principal
// (login, upload, etc.) NO debe abortar.
func TestService_InsertFailureIsSwallowed(t *testing.T) {
	st := &fakeStore{insertErr: errors.New("db locked")}
	svc := audit.NewService(st, slog.Default())

	// No panic, no return error — el método es void.
	svc.LogAuthLogin(context.Background(), nil, "u-x", "x")
}

// ─── X-Forwarded-For ───────────────────────────────────────────────

func TestService_XForwardedForExtractsFirstHop(t *testing.T) {
	svc, store := newSvc()
	req := httptest.NewRequest("POST", "/auth/login", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.42, 10.0.0.1, 10.0.0.2")
	req.RemoteAddr = "10.0.0.2:55555"

	svc.LogAuthLogin(req.Context(), req, "u-x", "x")

	got := store.last()
	if got.IPAddress != "203.0.113.42" {
		t.Errorf("ip = %q, want first XFF hop", got.IPAddress)
	}
}

func TestService_LongUserAgentIsTruncated(t *testing.T) {
	svc, store := newSvc()
	req := httptest.NewRequest("POST", "/auth/login", nil)
	long := make([]byte, 1024)
	for i := range long {
		long[i] = 'a'
	}
	req.Header.Set("User-Agent", string(long))

	svc.LogAuthLogin(req.Context(), req, "u-x", "x")

	got := store.last()
	if len(got.UserAgent) != 256 {
		t.Errorf("ua len = %d, want 256 (truncated)", len(got.UserAgent))
	}
}
