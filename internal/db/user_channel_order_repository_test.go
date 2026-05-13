package db_test

import (
	"context"
	"testing"

	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

// Tests below run against both backends via the matrix CI run
// (HUBPLAY_TEST_DRIVER=postgres). testutil.Exec auto-rewrites `?`
// placeholders to `$N` so the same fixtures work on both.

func TestUserChannelOrder_UpsertListDelete(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserChannelOrderRepository(testutil.Driver(), database)

	// Seed FK parents.
	testutil.Exec(t, database, "INSERT INTO users (id, username, display_name, password_hash, role) VALUES (?, ?, ?, ?, ?)",
		"u-1", "alice", "Alice", "$2a$", "user")
	testutil.Exec(t, database, "INSERT INTO libraries (id, name, content_type) VALUES (?, ?, ?)",
		"lib-1", "Live TV", "livetv")
	testutil.Exec(t, database, "INSERT INTO channels (id, library_id, name, number, stream_url, is_active) VALUES (?, ?, ?, ?, ?, ?)",
		"ch-a", "lib-1", "A", 1, "http://stream/a", true)
	testutil.Exec(t, database, "INSERT INTO channels (id, library_id, name, number, stream_url, is_active) VALUES (?, ?, ?, ?, ?, ?)",
		"ch-b", "lib-1", "B", 2, "http://stream/b", true)

	ctx := context.Background()

	// Empty for a fresh user.
	rows, err := repo.List(ctx, "u-1")
	if err != nil {
		t.Fatalf("list (empty): %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}

	// Upsert two overrides — one visible, one hidden.
	if err := repo.Upsert(ctx, "u-1", "ch-a", 99, false); err != nil {
		t.Fatalf("upsert ch-a: %v", err)
	}
	if err := repo.Upsert(ctx, "u-1", "ch-b", 50, true); err != nil {
		t.Fatalf("upsert ch-b: %v", err)
	}

	rows, err = repo.List(ctx, "u-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	// Ordered by position asc: ch-b (50) then ch-a (99).
	if rows[0].ChannelID != "ch-b" || rows[0].Position != 50 || !rows[0].Hidden {
		t.Errorf("row 0 unexpected: %+v", rows[0])
	}
	if rows[1].ChannelID != "ch-a" || rows[1].Position != 99 || rows[1].Hidden {
		t.Errorf("row 1 unexpected: %+v", rows[1])
	}

	// Upsert again on ch-a flips hidden + changes position (UPSERT path).
	if err := repo.Upsert(ctx, "u-1", "ch-a", 1, true); err != nil {
		t.Fatalf("upsert ch-a (replace): %v", err)
	}
	rows, _ = repo.List(ctx, "u-1")
	gotA := db.UserChannelOrderEntry{}
	for _, r := range rows {
		if r.ChannelID == "ch-a" {
			gotA = r
		}
	}
	if gotA.Position != 1 || !gotA.Hidden {
		t.Errorf("after upsert: ch-a = %+v, want position=1 hidden=true", gotA)
	}

	// Delete one specific row.
	if err := repo.Delete(ctx, "u-1", "ch-b"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	rows, _ = repo.List(ctx, "u-1")
	if len(rows) != 1 {
		t.Errorf("after delete: %d rows, want 1", len(rows))
	}

	// Reset wipes the remaining row.
	if err := repo.Reset(ctx, "u-1"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	rows, _ = repo.List(ctx, "u-1")
	if len(rows) != 0 {
		t.Errorf("after reset: %d rows, want 0", len(rows))
	}
}

func TestUserChannelOrder_ReplaceAllReplaces(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserChannelOrderRepository(testutil.Driver(), database)

	testutil.Exec(t, database, "INSERT INTO users (id, username, display_name, password_hash, role) VALUES (?, ?, ?, ?, ?)",
		"u-1", "alice", "Alice", "$2a$", "user")
	testutil.Exec(t, database, "INSERT INTO libraries (id, name, content_type) VALUES (?, ?, ?)",
		"lib-1", "Live TV", "livetv")
	for i, cid := range []string{"ch-a", "ch-b", "ch-c"} {
		testutil.Exec(t, database, "INSERT INTO channels (id, library_id, name, number, stream_url, is_active) VALUES (?, ?, ?, ?, ?, ?)",
			cid, "lib-1", cid, i+1, "http://stream/"+cid, true)
	}

	ctx := context.Background()

	// Seed initial state.
	_ = repo.Upsert(ctx, "u-1", "ch-a", 1, false)

	// Replace with fresh ordering: c, a (b is dropped from override —
	// it falls back to admin defaults).
	if err := repo.ReplaceAll(ctx, "u-1", []db.UserChannelOrderEntry{
		{ChannelID: "ch-c", Hidden: false},
		{ChannelID: "ch-a", Hidden: true},
	}); err != nil {
		t.Fatalf("replace: %v", err)
	}

	rows, _ := repo.List(ctx, "u-1")
	if len(rows) != 2 {
		t.Fatalf("expected 2 override rows, got %d", len(rows))
	}
	// ReplaceAll assigns position = index + 1.
	if rows[0].ChannelID != "ch-c" || rows[0].Position != 1 {
		t.Errorf("row 0 = %+v, want ch-c@1", rows[0])
	}
	if rows[1].ChannelID != "ch-a" || rows[1].Position != 2 || !rows[1].Hidden {
		t.Errorf("row 1 = %+v, want ch-a@2 hidden", rows[1])
	}
}
