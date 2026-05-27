package db_test

import (
	"context"
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

func newAuditRepo(t *testing.T) *db.AuditLogRepository {
	t.Helper()
	database := testutil.NewTestDB(t)
	return db.NewAuditLogRepository(testutil.Driver(), database)
}

func insertEvt(t *testing.T, repo *db.AuditLogRepository, row db.AuditLogRow) {
	t.Helper()
	if err := repo.Insert(context.Background(), row); err != nil {
		t.Fatalf("insert %s: %v", row.EventType, err)
	}
}

func TestAuditLogRepo_InsertAndQuery_ReturnsDESCByDate(t *testing.T) {
	repo := newAuditRepo(t)
	now := time.Now().UTC().Truncate(time.Second)

	insertEvt(t, repo, db.AuditLogRow{
		ID: "e1", ActorUserID: "u-1", EventType: "auth.login.ok",
		CreatedAt: now.Add(-3 * time.Hour),
	})
	insertEvt(t, repo, db.AuditLogRow{
		ID: "e2", ActorUserID: "u-1", EventType: "permission.changed",
		CreatedAt: now.Add(-2 * time.Hour),
	})
	insertEvt(t, repo, db.AuditLogRow{
		ID: "e3", ActorUserID: "u-2", EventType: "auth.login.failed",
		CreatedAt: now.Add(-1 * time.Hour),
	})

	rows, total, err := repo.Query(context.Background(), db.AuditQuery{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	wantOrder := []string{"e3", "e2", "e1"}
	for i, id := range wantOrder {
		if rows[i].ID != id {
			t.Errorf("row %d id = %s, want %s", i, rows[i].ID, id)
		}
	}
}

func TestAuditLogRepo_Query_FilterByActor(t *testing.T) {
	repo := newAuditRepo(t)
	now := time.Now().UTC()

	insertEvt(t, repo, db.AuditLogRow{ID: "e1", ActorUserID: "u-1", EventType: "x", CreatedAt: now})
	insertEvt(t, repo, db.AuditLogRow{ID: "e2", ActorUserID: "u-2", EventType: "x", CreatedAt: now})
	insertEvt(t, repo, db.AuditLogRow{ID: "e3", ActorUserID: "u-1", EventType: "y", CreatedAt: now})

	rows, total, _ := repo.Query(context.Background(), db.AuditQuery{ActorUserID: "u-1"})
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(rows) != 2 {
		t.Errorf("rows = %d", len(rows))
	}
}

func TestAuditLogRepo_Query_FilterByEventTypePrefix(t *testing.T) {
	repo := newAuditRepo(t)
	now := time.Now().UTC()

	insertEvt(t, repo, db.AuditLogRow{ID: "e1", EventType: "auth.login.ok", CreatedAt: now})
	insertEvt(t, repo, db.AuditLogRow{ID: "e2", EventType: "auth.login.failed", CreatedAt: now})
	insertEvt(t, repo, db.AuditLogRow{ID: "e3", EventType: "auth.logout", CreatedAt: now})
	insertEvt(t, repo, db.AuditLogRow{ID: "e4", EventType: "permission.changed", CreatedAt: now})

	// Prefix "auth." engancha 3 de los 4.
	_, total, _ := repo.Query(context.Background(), db.AuditQuery{EventTypePrefix: "auth."})
	if total != 3 {
		t.Errorf("total auth.* = %d, want 3", total)
	}

	// Exacto "auth.login.ok" engancha sólo 1.
	_, total, _ = repo.Query(context.Background(), db.AuditQuery{EventTypePrefix: "auth.login.ok"})
	if total != 1 {
		t.Errorf("total auth.login.ok = %d, want 1", total)
	}
}

func TestAuditLogRepo_Query_FilterByTimeWindow(t *testing.T) {
	repo := newAuditRepo(t)
	now := time.Now().UTC().Truncate(time.Second)

	insertEvt(t, repo, db.AuditLogRow{ID: "old", EventType: "x", CreatedAt: now.Add(-10 * time.Hour)})
	insertEvt(t, repo, db.AuditLogRow{ID: "mid", EventType: "x", CreatedAt: now.Add(-5 * time.Hour)})
	insertEvt(t, repo, db.AuditLogRow{ID: "new", EventType: "x", CreatedAt: now.Add(-1 * time.Hour)})

	_, total, _ := repo.Query(context.Background(), db.AuditQuery{
		From: now.Add(-6 * time.Hour),
		To:   now,
	})
	if total != 2 {
		t.Errorf("total in window = %d, want 2 (mid+new)", total)
	}
}

func TestAuditLogRepo_Query_SearchText(t *testing.T) {
	repo := newAuditRepo(t)
	now := time.Now().UTC()

	insertEvt(t, repo, db.AuditLogRow{
		ID: "e1", EventType: "x", CreatedAt: now,
		Payload: `{"username":"alex"}`,
	})
	insertEvt(t, repo, db.AuditLogRow{
		ID: "e2", EventType: "x", CreatedAt: now,
		Payload: `{"username":"bob"}`,
	})
	insertEvt(t, repo, db.AuditLogRow{
		ID: "e3", EventType: "x", CreatedAt: now,
		IPAddress: "192.168.1.42",
	})

	rows, total, _ := repo.Query(context.Background(), db.AuditQuery{SearchText: "alex"})
	if total != 1 || rows[0].ID != "e1" {
		t.Errorf("search alex: total=%d rows=%v", total, rows)
	}

	_, total, _ = repo.Query(context.Background(), db.AuditQuery{SearchText: "192.168"})
	if total != 1 {
		t.Errorf("search IP: total=%d, want 1", total)
	}
}

func TestAuditLogRepo_Query_Pagination(t *testing.T) {
	repo := newAuditRepo(t)
	now := time.Now().UTC()

	// 5 filas, todas con timestamps escalonados para orden determinístico.
	for i := 0; i < 5; i++ {
		insertEvt(t, repo, db.AuditLogRow{
			ID: "e" + string(rune('A'+i)), EventType: "x",
			CreatedAt: now.Add(time.Duration(-i) * time.Minute),
		})
	}

	page1, total, _ := repo.Query(context.Background(), db.AuditQuery{Limit: 2, Offset: 0})
	if total != 5 {
		t.Errorf("total = %d", total)
	}
	if len(page1) != 2 {
		t.Errorf("page1 len = %d", len(page1))
	}

	page2, _, _ := repo.Query(context.Background(), db.AuditQuery{Limit: 2, Offset: 2})
	if len(page2) != 2 {
		t.Errorf("page2 len = %d", len(page2))
	}
	// No solapan.
	if page1[0].ID == page2[0].ID {
		t.Error("pages overlap")
	}
}

func TestAuditLogRepo_DeleteOlderThan(t *testing.T) {
	repo := newAuditRepo(t)
	now := time.Now().UTC()

	insertEvt(t, repo, db.AuditLogRow{ID: "old", EventType: "x", CreatedAt: now.Add(-100 * 24 * time.Hour)})
	insertEvt(t, repo, db.AuditLogRow{ID: "recent", EventType: "x", CreatedAt: now.Add(-1 * time.Hour)})

	deleted, err := repo.DeleteOlderThan(context.Background(), now.Add(-90*24*time.Hour))
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}

	_, total, _ := repo.Query(context.Background(), db.AuditQuery{})
	if total != 1 {
		t.Errorf("post-sweep total = %d, want 1", total)
	}
}

func TestAuditLogRepo_DistinctEventTypes(t *testing.T) {
	repo := newAuditRepo(t)
	now := time.Now().UTC()
	// IDs únicos por iteración para no chocar contra la PK aunque
	// los event_type se repitan a propósito (la repetición es lo
	// que estamos testeando — el DISTINCT colapsa los duplicados).
	for i, et := range []string{"auth.login.ok", "auth.login.ok", "permission.changed", "system.restart"} {
		insertEvt(t, repo, db.AuditLogRow{
			ID: "row-" + string(rune('A'+i)), EventType: et, CreatedAt: now,
		})
	}

	types, err := repo.DistinctEventTypes(context.Background())
	if err != nil {
		t.Fatalf("distinct: %v", err)
	}
	if len(types) != 3 {
		t.Errorf("types = %v, want 3 distinct", types)
	}
	// Orden ASC del SQL — primero "auth.login.ok".
	if types[0] != "auth.login.ok" {
		t.Errorf("order broken: %v", types)
	}
}

// TestAuditLogRepo_Insert_UsesSeamTimeNow demuestra el uso del
// package-level seam timeNow: si el caller no rellena CreatedAt
// (Insert lo resuelve internamente con `timeNow().UTC()`), el
// timestamp persistido debe ser el que devuelva el reloj swappable.
//
// Sirve también de "smoke test" del seam — si alguien rompe el
// patrón sustituyendo timeNow() por time.Now() en algún repo, este
// test fallará.
func TestAuditLogRepo_Insert_UsesSeamTimeNow(t *testing.T) {
	frozen := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	db.SetTimeNowForTest(t, func() time.Time { return frozen })

	repo := newAuditRepo(t)
	// CreatedAt cero → Insert debe rellenarlo con el reloj swappable.
	insertEvt(t, repo, db.AuditLogRow{
		ID: "seam-1", ActorUserID: "u-1", EventType: "test.seam",
	})

	rows, _, err := repo.Query(context.Background(), db.AuditQuery{ActorUserID: "u-1"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	got := rows[0].CreatedAt.UTC().Truncate(time.Second)
	want := frozen.UTC().Truncate(time.Second)
	if !got.Equal(want) {
		t.Errorf("CreatedAt: got %v, want %v (seam not applied)", got, want)
	}
}
