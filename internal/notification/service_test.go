package notification

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"hubplay/internal/clock"
	"hubplay/internal/event"
)

// stubAdmins implementa AdminLister con una lista fija.
type stubAdmins struct{ ids []string }

func (s *stubAdmins) ListAdminIDs(_ context.Context) ([]string, error) {
	return s.ids, nil
}

// stubBus captura las publicaciones para verificar EventCreated.
type stubBus struct {
	mu     sync.Mutex
	events []event.Event
}

func (b *stubBus) Publish(e event.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, e)
}

// newRepoForTest crea un Repository sobre SQLite :memory: con el
// schema minimo (la migration 049 inline). Evita arrastrar el
// migrator entero para un test unit.
func newRepoForTest(t *testing.T) (*Repository, func()) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	const schema = `
CREATE TABLE users (
    id TEXT PRIMARY KEY,
    username TEXT,
    role TEXT
);
CREATE TABLE notifications (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL,
    kind       TEXT NOT NULL,
    title      TEXT NOT NULL,
    body       TEXT NOT NULL DEFAULT '',
    link       TEXT NOT NULL DEFAULT '',
    payload    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    read_at    TIMESTAMP
);
INSERT INTO users (id, username, role) VALUES ('u1', 'admin1', 'admin');
INSERT INTO users (id, username, role) VALUES ('u2', 'admin2', 'admin');
`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	return NewRepository("sqlite", db, nil), func() { _ = db.Close() }
}

func newSvcForTest(t *testing.T) (*Service, *stubBus, func()) {
	t.Helper()
	repo, closeFn := newRepoForTest(t)
	bus := &stubBus{}
	admins := &stubAdmins{ids: []string{"u1", "u2"}}
	clk := &clock.Mock{CurrentTime: time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewService(repo, admins, bus, clk, logger), bus, closeFn
}

func TestCreate_PersistsAndPublishes(t *testing.T) {
	svc, bus, closeFn := newSvcForTest(t)
	t.Cleanup(closeFn)
	ctx := context.Background()

	n, err := svc.Create(ctx, "u1", KindPairingRequestReceived,
		"Test title", "Test body", "/admin/peers", map[string]any{"x": 1})
	if err != nil {
		t.Fatal(err)
	}
	if n.ID == "" {
		t.Error("ID should be generated")
	}
	if n.Payload == "" || n.Payload == "null" {
		t.Errorf("payload should be JSON, got %q", n.Payload)
	}

	listed, err := svc.ListForUser(ctx, "u1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(listed))
	}

	if len(bus.events) != 1 {
		t.Errorf("expected 1 published event, got %d", len(bus.events))
	}
	if bus.events[0].Type != EventCreated {
		t.Errorf("event type = %s, want %s", bus.events[0].Type, EventCreated)
	}
	if uid := bus.events[0].Data["user_id"]; uid != "u1" {
		t.Errorf("event user_id = %v, want u1", uid)
	}
}

func TestFanOutToAdmins_CreatesPerAdmin(t *testing.T) {
	svc, bus, closeFn := newSvcForTest(t)
	t.Cleanup(closeFn)
	ctx := context.Background()

	n, err := svc.FanOutToAdmins(ctx, KindPairingRequestReceived,
		"new pairing", "from server X", "/admin/peers", nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("expected 2 fan-out notifications, got %d", n)
	}

	// Cada admin tiene una entrada en su inbox.
	for _, uid := range []string{"u1", "u2"} {
		got, err := svc.ListForUser(ctx, uid, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Errorf("user %s: expected 1 notif, got %d", uid, len(got))
		}
	}

	if len(bus.events) != 2 {
		t.Errorf("expected 2 published events, got %d", len(bus.events))
	}
}

func TestMarkRead_FlipsReadAtAndDecrementsUnread(t *testing.T) {
	svc, _, closeFn := newSvcForTest(t)
	t.Cleanup(closeFn)
	ctx := context.Background()

	n, err := svc.Create(ctx, "u1", KindPairingRequestReceived, "t", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	count, err := svc.UnreadCountForUser(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("unread = %d, want 1", count)
	}

	if err := svc.MarkRead(ctx, n.ID, "u1"); err != nil {
		t.Fatal(err)
	}
	count, _ = svc.UnreadCountForUser(ctx, "u1")
	if count != 0 {
		t.Errorf("after mark-read unread = %d, want 0", count)
	}
}

func TestMarkRead_RejectsCrossUserAccess(t *testing.T) {
	svc, _, closeFn := newSvcForTest(t)
	t.Cleanup(closeFn)
	ctx := context.Background()

	n, err := svc.Create(ctx, "u1", KindPairingRequestReceived, "t", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// u2 intenta marcar como leida la notif de u1 -> debe fallar
	// (devuelve ErrNotFound porque el WHERE user_id la oculta).
	if err := svc.MarkRead(ctx, n.ID, "u2"); err == nil {
		t.Error("expected error when other user marks not-their notification")
	}
	// La notif de u1 sigue no-leida.
	count, _ := svc.UnreadCountForUser(ctx, "u1")
	if count != 1 {
		t.Errorf("unread for u1 = %d, want 1 (no cross-user effect)", count)
	}
}

func TestMarkAllReadForUser_OnlyAffectsThatUser(t *testing.T) {
	svc, _, closeFn := newSvcForTest(t)
	t.Cleanup(closeFn)
	ctx := context.Background()

	_, _ = svc.Create(ctx, "u1", KindPairingRequestReceived, "t1", "", "", nil)
	_, _ = svc.Create(ctx, "u1", KindPairingRequestReceived, "t2", "", "", nil)
	_, _ = svc.Create(ctx, "u2", KindPairingRequestReceived, "t3", "", "", nil)

	n, err := svc.MarkAllReadForUser(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("marked = %d, want 2", n)
	}
	c1, _ := svc.UnreadCountForUser(ctx, "u1")
	c2, _ := svc.UnreadCountForUser(ctx, "u2")
	if c1 != 0 || c2 != 1 {
		t.Errorf("unread u1=%d u2=%d, want 0/1 (only u1 affected)", c1, c2)
	}
}

// TestRepository_UsesInjectedClock verifica que Insert con
// CreatedAt zero usa el clock inyectado en NewRepository (no time.Now).
// Sin el campo `clock`, este test se rompería: el CreatedAt sería el
// momento de ejecución, no el del mock.
func TestRepository_UsesInjectedClock(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close() //nolint:errcheck
	const schema = `
CREATE TABLE notifications (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL,
    kind       TEXT NOT NULL,
    title      TEXT NOT NULL,
    body       TEXT NOT NULL DEFAULT '',
    link       TEXT NOT NULL DEFAULT '',
    payload    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    read_at    TIMESTAMP
);`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}

	fixed := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	mock := &clock.Mock{CurrentTime: fixed}
	repo := NewRepository("sqlite", db, mock)

	n := &Notification{
		UserID: "u1",
		Kind:   "test",
		Title:  "hello",
	}
	if err := repo.Insert(context.Background(), n); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if !n.CreatedAt.Equal(fixed) {
		t.Errorf("CreatedAt = %v, want fixed %v", n.CreatedAt, fixed)
	}
}
