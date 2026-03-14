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
