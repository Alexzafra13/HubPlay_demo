package db_test

import (
	"context"
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

func newAuditRow(id, userID, outcome string, t time.Time) db.UploadAuditRow {
	return db.UploadAuditRow{
		ID:           id,
		UserID:       userID,
		OriginalName: "movie.mkv",
		Bytes:        1024 * 1024 * 700, // 700 MiB
		Outcome:      outcome,
		StartedAt:    t,
		FinishedAt:   t.Add(45 * time.Second),
		DurationMs:   45_000,
	}
}

func TestUploadAuditRepository_Insert_And_List(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUploadAuditRepository(testutil.Driver(), database)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	rows := []db.UploadAuditRow{
		newAuditRow("a1", "u-alex", "accepted", now.Add(-2*time.Hour)),
		newAuditRow("a2", "u-alex", "rejected", now.Add(-1*time.Hour)),
		newAuditRow("a3", "u-bea", "accepted", now.Add(-30*time.Minute)),
		newAuditRow("a4", "u-alex", "error", now),
	}
	rows[1].ErrorMessage = "ffprobe could not decode"
	rows[1].MimeDetected = "application/octet-stream"
	rows[3].FinalPath = "" // error path

	for _, r := range rows {
		if err := repo.Insert(ctx, r); err != nil {
			t.Fatalf("Insert %s: %v", r.ID, err)
		}
	}

	// ListByUser respects user filter + DESC order.
	alex, err := repo.ListByUser(ctx, "u-alex", 50)
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(alex) != 3 {
		t.Fatalf("want 3 rows for alex, got %d", len(alex))
	}
	// Order: a4 (most recent) → a2 → a1.
	wantOrder := []string{"a4", "a2", "a1"}
	for i, id := range wantOrder {
		if alex[i].ID != id {
			t.Errorf("row %d = %s, want %s", i, alex[i].ID, id)
		}
	}

	bea, _ := repo.ListByUser(ctx, "u-bea", 50)
	if len(bea) != 1 {
		t.Errorf("want 1 row for bea, got %d", len(bea))
	}

	// Re-fetched row preserves the optional fields.
	if alex[1].ErrorMessage != "ffprobe could not decode" {
		t.Errorf("error_message lost: %q", alex[1].ErrorMessage)
	}
	if alex[1].MimeDetected != "application/octet-stream" {
		t.Errorf("mime_detected lost: %q", alex[1].MimeDetected)
	}
}

func TestUploadAuditRepository_ListByUser_LimitCap(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUploadAuditRepository(testutil.Driver(), database)
	ctx := context.Background()

	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		_ = repo.Insert(ctx, newAuditRow(
			"row-"+string(rune('A'+i)),
			"u-alex",
			"accepted",
			now.Add(time.Duration(-i)*time.Minute),
		))
	}

	got, err := repo.ListByUser(ctx, "u-alex", 2)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("limit 2 returned %d", len(got))
	}
}

// TestUploadAuditRepository_Insert_RejectsInvalidOutcome pin la
// constraint CHECK de la migración — outcomes fuera del enum no caben.
func TestUploadAuditRepository_Insert_RejectsInvalidOutcome(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUploadAuditRepository(testutil.Driver(), database)

	bad := newAuditRow("a1", "u-alex", "mystery_state", time.Now())
	if err := repo.Insert(context.Background(), bad); err == nil {
		t.Error("expected CHECK violation for invalid outcome")
	}
}
