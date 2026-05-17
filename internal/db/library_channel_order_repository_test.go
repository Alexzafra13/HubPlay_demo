package db_test

// LibraryChannelOrderRepository tests. Mirror of the per-user repo
// tests since the two APIs are intentionally parallel — adding a
// new column to one and forgetting the other has bitten before,
// so the test surfaces are symmetric on purpose.

import (
	"context"
	"testing"

	iptvmodel "hubplay/internal/iptv/model"
	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

func TestLibraryChannelOrder_UpsertListDelete(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewLibraryChannelOrderRepository(testutil.Driver(), database)

	testutil.Exec(t, database, "INSERT INTO libraries (id, name, content_type) VALUES (?, ?, ?)",
		"lib-1", "Live TV", "livetv")
	testutil.Exec(t, database, "INSERT INTO channels (id, library_id, name, number, stream_url, is_active) VALUES (?, ?, ?, ?, ?, ?)",
		"ch-a", "lib-1", "A", 1, "http://stream/a", true)
	testutil.Exec(t, database, "INSERT INTO channels (id, library_id, name, number, stream_url, is_active) VALUES (?, ?, ?, ?, ?, ?)",
		"ch-b", "lib-1", "B", 2, "http://stream/b", true)

	ctx := context.Background()

	rows, err := repo.List(ctx, "lib-1")
	if err != nil {
		t.Fatalf("list (empty): %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}

	if err := repo.Upsert(ctx, "lib-1", "ch-a", 99, false); err != nil {
		t.Fatalf("upsert ch-a: %v", err)
	}
	if err := repo.Upsert(ctx, "lib-1", "ch-b", 50, true); err != nil {
		t.Fatalf("upsert ch-b: %v", err)
	}

	rows, err = repo.List(ctx, "lib-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].ChannelID != "ch-b" || rows[0].Position != 50 || !rows[0].Hidden {
		t.Errorf("row 0 unexpected: %+v", rows[0])
	}
	if rows[1].ChannelID != "ch-a" || rows[1].Position != 99 || rows[1].Hidden {
		t.Errorf("row 1 unexpected: %+v", rows[1])
	}

	if err := repo.Upsert(ctx, "lib-1", "ch-a", 1, true); err != nil {
		t.Fatalf("upsert ch-a (replace): %v", err)
	}
	rows, _ = repo.List(ctx, "lib-1")
	gotA := iptvmodel.LibraryChannelOrderEntry{}
	for _, r := range rows {
		if r.ChannelID == "ch-a" {
			gotA = r
		}
	}
	if gotA.Position != 1 || !gotA.Hidden {
		t.Errorf("after upsert: ch-a = %+v, want position=1 hidden=true", gotA)
	}

	if err := repo.Delete(ctx, "lib-1", "ch-b"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	rows, _ = repo.List(ctx, "lib-1")
	if len(rows) != 1 {
		t.Errorf("after delete: %d rows, want 1", len(rows))
	}

	if err := repo.Reset(ctx, "lib-1"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	rows, _ = repo.List(ctx, "lib-1")
	if len(rows) != 0 {
		t.Errorf("after reset: %d rows, want 0", len(rows))
	}
}

func TestLibraryChannelOrder_ReplaceAllReplaces(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewLibraryChannelOrderRepository(testutil.Driver(), database)

	testutil.Exec(t, database, "INSERT INTO libraries (id, name, content_type) VALUES (?, ?, ?)",
		"lib-1", "Live TV", "livetv")
	testutil.Exec(t, database, "INSERT INTO channels (id, library_id, name, number, stream_url, is_active) VALUES (?, ?, ?, ?, ?, ?)",
		"ch-a", "lib-1", "A", 1, "http://stream/a", true)
	testutil.Exec(t, database, "INSERT INTO channels (id, library_id, name, number, stream_url, is_active) VALUES (?, ?, ?, ?, ?, ?)",
		"ch-b", "lib-1", "B", 2, "http://stream/b", true)
	testutil.Exec(t, database, "INSERT INTO channels (id, library_id, name, number, stream_url, is_active) VALUES (?, ?, ?, ?, ?, ?)",
		"ch-c", "lib-1", "C", 3, "http://stream/c", true)

	ctx := context.Background()

	// Seed two rows we expect to be wiped by ReplaceAll.
	if err := repo.Upsert(ctx, "lib-1", "ch-a", 99, false); err != nil {
		t.Fatal(err)
	}
	if err := repo.Upsert(ctx, "lib-1", "ch-c", 50, true); err != nil {
		t.Fatal(err)
	}

	// Replace with: ch-b first (visible), ch-a second (hidden).
	newEntries := []iptvmodel.LibraryChannelOrderEntry{
		{ChannelID: "ch-b", Hidden: false},
		{ChannelID: "ch-a", Hidden: true},
	}
	if err := repo.ReplaceAll(ctx, "lib-1", newEntries); err != nil {
		t.Fatalf("ReplaceAll: %v", err)
	}

	rows, err := repo.List(ctx, "lib-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("after ReplaceAll: %d rows, want 2 (ch-c should be wiped)", len(rows))
	}
	if rows[0].ChannelID != "ch-b" || rows[0].Position != 1 || rows[0].Hidden {
		t.Errorf("row 0: %+v want ch-b/1/false", rows[0])
	}
	if rows[1].ChannelID != "ch-a" || rows[1].Position != 2 || !rows[1].Hidden {
		t.Errorf("row 1: %+v want ch-a/2/true", rows[1])
	}

	// ReplaceAll with empty entries should clear everything.
	if err := repo.ReplaceAll(ctx, "lib-1", nil); err != nil {
		t.Fatalf("ReplaceAll(empty): %v", err)
	}
	rows, _ = repo.List(ctx, "lib-1")
	if len(rows) != 0 {
		t.Errorf("after ReplaceAll(empty): %d rows, want 0", len(rows))
	}
}
