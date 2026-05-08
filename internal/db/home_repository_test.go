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

// TestHomeRepository_Recommended_ScoresUnwatchedByGenreOverlap covers the
// happy path of the "Recomendado para ti" tier: the caller has watched a
// drama; we surface an unwatched drama and skip the unwatched comedy
// because it doesn't share genres with their viewing history.
func TestHomeRepository_Recommended_ScoresUnwatchedByGenreOverlap(t *testing.T) {
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
		ID: "lib-rec", Name: "Movies", ContentType: "movies",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create library: %v", err)
	}

	// Three movies: one watched (drama+thriller), one drama unwatched
	// (should match), one comedy unwatched (should NOT match — no genre
	// overlap). Ordered so that without the genre filter the comedy
	// would still rank above the drama by added_at.
	mustItem := func(id, title, sortKey string, addedDelta time.Duration) {
		if err := repos.Items.Create(ctx, &db.Item{
			ID: id, LibraryID: "lib-rec", Type: "movie",
			Title: title, SortTitle: sortKey,
			DurationTicks: 72000000000, Container: "mp4",
			AddedAt: now.Add(addedDelta), UpdatedAt: now, IsAvailable: true,
		}); err != nil {
			t.Fatalf("create item %s: %v", id, err)
		}
	}
	mustItem("m-watched", "Already Seen", "already seen", -72*time.Hour)
	mustItem("m-drama-new", "Fresh Drama", "fresh drama", -1*time.Hour)
	mustItem("m-comedy-new", "Fresh Comedy", "fresh comedy", -30*time.Minute)

	if err := repos.ItemValues.SetGenres(ctx, "m-watched", []string{"Drama", "Thriller"}); err != nil {
		t.Fatalf("set genres watched: %v", err)
	}
	if err := repos.ItemValues.SetGenres(ctx, "m-drama-new", []string{"Drama", "Mystery"}); err != nil {
		t.Fatalf("set genres drama: %v", err)
	}
	if err := repos.ItemValues.SetGenres(ctx, "m-comedy-new", []string{"Comedy"}); err != nil {
		t.Fatalf("set genres comedy: %v", err)
	}

	// User has played the Drama+Thriller — that establishes their genre
	// affinity. The other two stay unwatched.
	if err := repos.UserData.UpdateProgress(ctx, "u-1", "m-watched", 30000000000, false); err != nil {
		t.Fatalf("update progress: %v", err)
	}

	rows, err := repos.Home.Recommended(ctx, "u-1", 5)
	if err != nil {
		t.Fatalf("recommended: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 recommendation, got %d: %+v", len(rows), rows)
	}
	if rows[0].ID != "m-drama-new" {
		t.Errorf("recommendation = %s, want m-drama-new (genre match)", rows[0].ID)
	}
	// `Because` should surface only the genres that actually overlap
	// (Drama in this case). Mystery was a genre on the candidate but
	// the user hasn't watched anything Mystery, so it shouldn't bubble.
	if len(rows[0].Because) == 0 {
		t.Errorf("Because is empty; want at least one matched genre")
	}
	for _, g := range rows[0].Because {
		if g != "Drama" {
			t.Errorf("Because contains %q; only Drama should match user history", g)
		}
	}
}

// TestHomeRepository_Recommended_ColdStartReturnsNil verifies the
// cold-start guarantee: a user with no engagement history gets nil
// (not an error, not a generic catalogue dump). The hero hides the
// slot in this case.
func TestHomeRepository_Recommended_ColdStartReturnsNil(t *testing.T) {
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(database)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := repos.Users.Create(ctx, &db.User{
		ID: "u-cold", Username: "cold", PasswordHash: "h",
		Role: "user", CreatedAt: now, IsActive: true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	rows, err := repos.Home.Recommended(ctx, "u-cold", 5)
	if err != nil {
		t.Fatalf("recommended: %v", err)
	}
	if rows != nil {
		t.Errorf("cold-start should return nil, got %+v", rows)
	}
}

// TestHomeRepository_Recommended_FiltersWatchedItems makes sure an item
// the user has already engaged with — even briefly — never shows up
// as a "fresh recommendation". The 5%-of-duration threshold keeps a
// 30-second misclick from flagging an item, so we test both the
// watched and "barely opened" boundaries.
func TestHomeRepository_Recommended_FiltersWatchedItems(t *testing.T) {
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
		ID: "lib-rec", Name: "Movies", ContentType: "movies",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create library: %v", err)
	}

	// Two dramas matching the user's affinity: one started past 5%
	// (should be filtered as "in progress"), one barely touched (1%,
	// should still surface).
	mustItem := func(id string) {
		if err := repos.Items.Create(ctx, &db.Item{
			ID: id, LibraryID: "lib-rec", Type: "movie",
			Title: id, SortTitle: id,
			DurationTicks: 1000_000_000, Container: "mp4",
			AddedAt: now, UpdatedAt: now, IsAvailable: true,
		}); err != nil {
			t.Fatalf("create item %s: %v", id, err)
		}
	}
	mustItem("m-seed")
	mustItem("m-in-progress")
	mustItem("m-barely-opened")
	for _, id := range []string{"m-seed", "m-in-progress", "m-barely-opened"} {
		if err := repos.ItemValues.SetGenres(ctx, id, []string{"Drama"}); err != nil {
			t.Fatalf("set genres %s: %v", id, err)
		}
	}

	// Establish affinity (seed completed by virtue of progress).
	if err := repos.UserData.UpdateProgress(ctx, "u-1", "m-seed", 500_000_000, true); err != nil {
		t.Fatalf("seed progress: %v", err)
	}
	// Opened past 5% but not completed.
	if err := repos.UserData.UpdateProgress(ctx, "u-1", "m-in-progress", 100_000_000, false); err != nil {
		t.Fatalf("in-progress: %v", err)
	}
	// Barely opened (1%): should still surface as recommendation.
	if err := repos.UserData.UpdateProgress(ctx, "u-1", "m-barely-opened", 10_000_000, false); err != nil {
		t.Fatalf("barely-opened: %v", err)
	}

	rows, err := repos.Home.Recommended(ctx, "u-1", 5)
	if err != nil {
		t.Fatalf("recommended: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 recommendation, got %d: %+v", len(rows), rows)
	}
	if rows[0].ID != "m-barely-opened" {
		t.Errorf("recommendation = %s, want m-barely-opened (m-in-progress past 5%% should be filtered)", rows[0].ID)
	}
}
