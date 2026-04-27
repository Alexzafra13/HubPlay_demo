package db_test

import (
	"context"
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

func setupUserDataTest(t *testing.T) (*db.UserDataRepository, *db.ItemRepository) {
	t.Helper()
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(database)

	// Create a user
	now := time.Now()
	_ = repos.Users.Create(context.Background(), &db.User{
		ID: "user-1", Username: "testuser", PasswordHash: "hash",
		Role: "user", CreatedAt: now, IsActive: true,
	})

	// Create a library
	_ = repos.Libraries.Create(context.Background(), &db.Library{
		ID: "lib-ud", Name: "Movies", ContentType: "movies",
		CreatedAt: now, UpdatedAt: now,
	})

	// Create items
	for _, item := range []*db.Item{
		{ID: "movie-1", LibraryID: "lib-ud", Type: "movie", Title: "Movie 1",
			SortTitle: "movie 1", DurationTicks: 72000000000, Container: "mp4",
			AddedAt: now, UpdatedAt: now, IsAvailable: true},
		{ID: "movie-2", LibraryID: "lib-ud", Type: "movie", Title: "Movie 2",
			SortTitle: "movie 2", DurationTicks: 54000000000, Container: "mkv",
			AddedAt: now, UpdatedAt: now, IsAvailable: true},
	} {
		_ = repos.Items.Create(context.Background(), item)
	}

	return repos.UserData, repos.Items
}

func TestUserData_UpdateProgress(t *testing.T) {
	repo, _ := setupUserDataTest(t)
	ctx := context.Background()

	// Update progress
	err := repo.UpdateProgress(ctx, "user-1", "movie-1", 30000000000, false)
	if err != nil {
		t.Fatal(err)
	}

	// Get it back
	ud, err := repo.Get(ctx, "user-1", "movie-1")
	if err != nil {
		t.Fatal(err)
	}
	if ud == nil {
		t.Fatal("expected user data, got nil")
	}
	if ud.PositionTicks != 30000000000 {
		t.Errorf("position = %d, want 30000000000", ud.PositionTicks)
	}
	if ud.Completed {
		t.Error("should not be completed")
	}
}

func TestUserData_GetNonExistent(t *testing.T) {
	repo, _ := setupUserDataTest(t)

	ud, err := repo.Get(context.Background(), "user-1", "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if ud != nil {
		t.Error("expected nil for non-existent user data")
	}
}

func TestUserData_MarkPlayed(t *testing.T) {
	repo, _ := setupUserDataTest(t)
	ctx := context.Background()

	err := repo.MarkPlayed(ctx, "user-1", "movie-1")
	if err != nil {
		t.Fatal(err)
	}

	ud, err := repo.Get(ctx, "user-1", "movie-1")
	if err != nil {
		t.Fatal(err)
	}
	if ud.PlayCount != 1 {
		t.Errorf("play_count = %d, want 1", ud.PlayCount)
	}
	if !ud.Completed {
		t.Error("should be completed")
	}

	// Mark again — count should increment
	_ = repo.MarkPlayed(ctx, "user-1", "movie-1")
	ud, _ = repo.Get(ctx, "user-1", "movie-1")
	if ud.PlayCount != 2 {
		t.Errorf("play_count = %d, want 2", ud.PlayCount)
	}
}

func TestUserData_Favorites(t *testing.T) {
	repo, _ := setupUserDataTest(t)
	ctx := context.Background()

	// Set favorite
	err := repo.SetFavorite(ctx, "user-1", "movie-1", true)
	if err != nil {
		t.Fatal(err)
	}

	// Check
	ud, _ := repo.Get(ctx, "user-1", "movie-1")
	if !ud.IsFavorite {
		t.Error("should be favorite")
	}

	// List favorites
	favs, err := repo.Favorites(ctx, "user-1", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(favs) != 1 {
		t.Fatalf("expected 1 favorite, got %d", len(favs))
	}
	if favs[0].ItemID != "movie-1" {
		t.Errorf("favorite item = %s, want movie-1", favs[0].ItemID)
	}

	// Unfavorite
	_ = repo.SetFavorite(ctx, "user-1", "movie-1", false)
	favs, _ = repo.Favorites(ctx, "user-1", 10, 0)
	if len(favs) != 0 {
		t.Error("expected 0 favorites after unfavorite")
	}
}

func TestUserData_ContinueWatching(t *testing.T) {
	repo, _ := setupUserDataTest(t)
	ctx := context.Background()

	// Start watching movie-1
	_ = repo.UpdateProgress(ctx, "user-1", "movie-1", 15000000000, false)
	// Complete movie-2
	_ = repo.MarkPlayed(ctx, "user-1", "movie-2")

	items, err := repo.ContinueWatching(ctx, "user-1", 10)
	if err != nil {
		t.Fatal(err)
	}

	// Only movie-1 should appear (in progress, not completed)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].ItemID != "movie-1" {
		t.Errorf("item = %s, want movie-1", items[0].ItemID)
	}
	if items[0].PositionTicks != 15000000000 {
		t.Errorf("position = %d", items[0].PositionTicks)
	}
}

// ContinueWatching's filtered shape — near-complete and abandoned
// items are dropped instead of polluting the rail.

func TestUserData_ContinueWatching_DropsNearComplete(t *testing.T) {
	repo, _ := setupUserDataTest(t)
	ctx := context.Background()

	// movie-1 duration = 72e9. Position 65e9 ≈ 90.3 %, must be
	// classified as "effectively done" and excluded from the rail
	// even though `completed = 0`.
	now := time.Now()
	if err := repo.Upsert(ctx, &db.UserData{
		UserID: "user-1", ItemID: "movie-1",
		PositionTicks: 65_000_000_000, Completed: false,
		LastPlayedAt: &now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	// movie-2 duration = 54e9. Position 27e9 = 50 %, recent play —
	// the canonical "in progress, came back yesterday" case.
	if err := repo.Upsert(ctx, &db.UserData{
		UserID: "user-1", ItemID: "movie-2",
		PositionTicks: 27_000_000_000, Completed: false,
		LastPlayedAt: &now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	items, err := repo.ContinueWatching(ctx, "user-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item (only movie-2 — movie-1 is near-complete), got %d", len(items))
	}
	if items[0].ItemID != "movie-2" {
		t.Errorf("item = %s, want movie-2", items[0].ItemID)
	}
}

func TestUserData_ContinueWatching_DropsAbandoned(t *testing.T) {
	repo, _ := setupUserDataTest(t)
	ctx := context.Background()

	now := time.Now()
	old := now.Add(-45 * 24 * time.Hour) // > AbandonedAfter (30d)

	// Old play, <50 % progress: abandoned. movie-1 duration 72e9,
	// position 10e9 ≈ 13 %, last played 45 days ago. Drop.
	if err := repo.Upsert(ctx, &db.UserData{
		UserID: "user-1", ItemID: "movie-1",
		PositionTicks: 10_000_000_000, Completed: false,
		LastPlayedAt: &old, UpdatedAt: old,
	}); err != nil {
		t.Fatal(err)
	}
	// Old play, >50 % progress: NOT abandoned — the user invested
	// real time, the rail keeps it. movie-2 duration 54e9, position
	// 35e9 ≈ 65 %, also 45 days old.
	if err := repo.Upsert(ctx, &db.UserData{
		UserID: "user-1", ItemID: "movie-2",
		PositionTicks: 35_000_000_000, Completed: false,
		LastPlayedAt: &old, UpdatedAt: old,
	}); err != nil {
		t.Fatal(err)
	}

	items, err := repo.ContinueWatching(ctx, "user-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item (movie-2 — movie-1 abandoned at 13%%), got %d", len(items))
	}
	if items[0].ItemID != "movie-2" {
		t.Errorf("item = %s, want movie-2", items[0].ItemID)
	}
}

func TestUserData_ContinueWatching_KeepsItemsWithUnknownDuration(t *testing.T) {
	// duration_ticks = 0 means "we couldn't probe this file" — both
	// the near-complete and abandoned filters require a known duration
	// to reason about progress. Without one, the rail surfaces the
	// item rather than silently swallow it: better to nag the user
	// than vanish content.
	repo, items := setupUserDataTest(t)
	ctx := context.Background()
	now := time.Now()
	old := now.Add(-90 * 24 * time.Hour) // way past AbandonedAfter

	// Patch movie-2 to have unknown duration. Item.Update doesn't
	// exist as a public surface, so we go through the repository's
	// existing patch path: a fresh row with duration_ticks=0.
	if err := items.Update(ctx, &db.Item{
		ID: "movie-2", LibraryID: "lib-ud", Type: "movie", Title: "Movie 2",
		SortTitle: "movie 2", DurationTicks: 0, Container: "mkv",
		AddedAt: now, UpdatedAt: now, IsAvailable: true,
	}); err != nil {
		t.Fatal(err)
	}

	// Even with old timestamp + tiny position, the item must remain
	// because the filters bail out when duration is unknown.
	if err := repo.Upsert(ctx, &db.UserData{
		UserID: "user-1", ItemID: "movie-2",
		PositionTicks: 1_000_000, Completed: false,
		LastPlayedAt: &old, UpdatedAt: old,
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := repo.ContinueWatching(ctx, "user-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ItemID != "movie-2" {
		t.Errorf("unknown-duration item should survive both filters, got %+v", rows)
	}
}

func TestUserData_Delete(t *testing.T) {
	repo, _ := setupUserDataTest(t)
	ctx := context.Background()

	_ = repo.UpdateProgress(ctx, "user-1", "movie-1", 100, false)
	_ = repo.Delete(ctx, "user-1", "movie-1")

	ud, _ := repo.Get(ctx, "user-1", "movie-1")
	if ud != nil {
		t.Error("expected nil after delete")
	}
}
