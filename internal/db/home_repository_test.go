package db_test

import (
	"context"
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

// setupHomeTrendingTest plants the minimal schema (user, library, movie) the
// trending query needs to surface a row. Returns the wired repositories so the
// caller can plant user_data either via the repo (post-fix UTC contract) or
// raw SQL (legacy bad strings).
func setupHomeTrendingTest(t *testing.T) *db.Repositories {
	t.Helper()
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(database)
	ctx := context.Background()

	now := time.Now().UTC()
	if err := repos.Users.Create(ctx, &db.User{
		ID: "u-1", Username: "u", PasswordHash: "h",
		Role: "user", CreatedAt: now, IsActive: true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repos.Libraries.Create(ctx, &db.Library{
		ID: "lib-h", Name: "Movies", ContentType: "movies",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := repos.Items.Create(ctx, &db.Item{
		ID: "movie-trending", LibraryID: "lib-h", Type: "movie",
		Title: "Trending Movie", SortTitle: "trending movie",
		DurationTicks: 72000000000, Container: "mp4",
		AddedAt: now, UpdatedAt: now, IsAvailable: true,
	}); err != nil {
		t.Fatalf("create item: %v", err)
	}
	return repos
}

// TestHomeRepository_Trending_FreshRoundTrip is the happy path: a row written
// through UpdateProgress (which UTC-normalises) round-trips through Trending
// without coerceSQLiteTime needing to do anything clever.
func TestHomeRepository_Trending_FreshRoundTrip(t *testing.T) {
	repos := setupHomeTrendingTest(t)
	ctx := context.Background()

	if err := repos.UserData.UpdateProgress(ctx, "u-1", "movie-trending", 30000000000, false); err != nil {
		t.Fatalf("update progress: %v", err)
	}

	rows, err := repos.Home.Trending(ctx, "u-1", 7, 12)
	if err != nil {
		t.Fatalf("trending: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "movie-trending" {
		t.Fatalf("trending rows = %+v, want one row for movie-trending", rows)
	}
	if rows[0].LastPlayedAt.IsZero() {
		t.Errorf("LastPlayedAt is zero, want a parsed timestamp")
	}
	if rows[0].LastPlayedAt.Location() != time.UTC {
		t.Errorf("LastPlayedAt location = %v, want UTC", rows[0].LastPlayedAt.Location())
	}
}

// TestHomeRepository_Trending_HandlesLegacyMonotonicTimestamp covers the prod
// regression: rows written by older builds (pre-UTC fix) carry a
// "2026-04-24 12:00:00 +0200 CEST m=+0.001234567" string the default Scan path
// rejects. The user_data table is full of these in production, so even after
// shipping the write-side UTC fix, Trending must keep parsing them or the rail
// keeps returning 500.
//
// We plant the row via raw SQL because the Upsert helper now defensively
// coerces to UTC — the legacy shape can't be reproduced through the public
// API anymore, by design.
func TestHomeRepository_Trending_HandlesLegacyMonotonicTimestamp(t *testing.T) {
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(database)
	ctx := context.Background()

	now := time.Now().UTC()
	_ = repos.Users.Create(ctx, &db.User{
		ID: "u-1", Username: "u", PasswordHash: "h",
		Role: "user", CreatedAt: now, IsActive: true,
	})
	_ = repos.Libraries.Create(ctx, &db.Library{
		ID: "lib-h", Name: "Movies", ContentType: "movies",
		CreatedAt: now, UpdatedAt: now,
	})
	_ = repos.Items.Create(ctx, &db.Item{
		ID: "movie-legacy", LibraryID: "lib-h", Type: "movie",
		Title: "Legacy Movie", SortTitle: "legacy movie",
		DurationTicks: 72000000000, Container: "mp4",
		AddedAt: now, UpdatedAt: now, IsAvailable: true,
	})

	// Plant a user_data row in the exact shape modernc.org/sqlite would have
	// written when given a non-UTC time.Now(): named-zone abbreviation plus
	// monotonic-clock suffix. Both updated_at and last_played_at use the bad
	// shape so we exercise both columns going through coerceSQLiteTime.
	const legacyTS = "2026-04-24 12:00:00 +0200 CEST m=+0.001234567"
	if _, err := database.ExecContext(ctx, `INSERT INTO user_data
		(user_id, item_id, position_ticks, play_count, completed, is_favorite,
		 last_played_at, updated_at)
		VALUES ('u-1', 'movie-legacy', 30000000000, 1, 0, 0, ?, ?)`,
		legacyTS, legacyTS,
	); err != nil {
		t.Fatalf("seed legacy user_data: %v", err)
	}

	rows, err := repos.Home.Trending(ctx, "u-1", 365, 12) // wide window to be safe
	if err != nil {
		t.Fatalf("trending must not 500 on legacy rows: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "movie-legacy" {
		t.Fatalf("trending rows = %+v, want one row for movie-legacy", rows)
	}
	want := time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC) // +0200 → 10:00Z
	if !rows[0].LastPlayedAt.Equal(want) {
		t.Errorf("parsed LastPlayedAt = %v, want %v", rows[0].LastPlayedAt, want)
	}
}
