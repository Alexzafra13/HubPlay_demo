package db_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

// BenchmarkHomeRepository_Trending measures the cross-cutting
// "trending across all users" rail. The query has an inner CTE
// folding episodes up to their series + a per-row library_access
// EXISTS gate — the two pieces that make Home rails noticeably
// heavier than a plain repo list.
func BenchmarkHomeRepository_Trending(b *testing.B) {
	for _, n := range []int{500, 2000} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			home, userID := newBenchHomeRepo(b, n)
			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rows, err := home.Trending(ctx, userID, 7, 12)
				if err != nil {
					b.Fatalf("trending: %v", err)
				}
				if len(rows) == 0 {
					b.Fatalf("expected non-empty trending rail")
				}
			}
		})
	}
}

// BenchmarkHomeRepository_LiveNow measures the "live now" rail, the
// only Home rail that joins against epg_programs + user_channel_favorites
// in addition to the library_access EXISTS gate.
func BenchmarkHomeRepository_LiveNow(b *testing.B) {
	for _, n := range []int{500, 2000} {
		b.Run(fmt.Sprintf("channels=%d", n), func(b *testing.B) {
			home, userID := newBenchHomeRepoLiveTV(b, n)
			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rows, err := home.LiveNow(ctx, userID, 5)
				if err != nil {
					b.Fatalf("live now: %v", err)
				}
				if len(rows) == 0 {
					b.Fatalf("expected non-empty live now rail")
				}
			}
		})
	}
}

// newBenchHomeRepo seeds a movies library with `n` items, one user
// with access, and user_data rows for ~30 % of the items so the
// trending rollup has work to do. Returns the HomeRepository + the
// caller's user id.
func newBenchHomeRepo(b *testing.B, n int) (*db.HomeRepository, string) {
	b.Helper()
	database := testutil.NewTestDB(b)
	repos := db.NewRepositories(testutil.Driver(), database)
	ctx := context.Background()

	now := time.Now().UTC()
	if err := repos.Libraries.Create(ctx, &db.Library{
		ID: "lib-mov", Name: "Movies", ContentType: "movies",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		b.Fatalf("create library: %v", err)
	}
	if err := repos.Users.Create(ctx, &authmodel.User{
		ID: "u-1", Username: "u1", DisplayName: "U1",
		PasswordHash: "x", Role: "admin", IsActive: true, CreatedAt: now,
	}); err != nil {
		b.Fatalf("create user: %v", err)
	}
	if err := repos.Libraries.GrantAccess(ctx, "u-1", "lib-mov"); err != nil {
		b.Fatalf("grant access: %v", err)
	}

	// Movies + a few episodes so the rollup CTE has work.
	const ticks90min int64 = 90 * 60 * 10_000_000
	for i := 0; i < n; i++ {
		typ := "movie"
		var parent string
		if i%10 == 0 {
			typ = "episode"
			parent = fmt.Sprintf("series-%d", i/40)
			if i%40 == 0 {
				_ = repos.Items.Create(ctx, &db.Item{
					ID: parent, LibraryID: "lib-mov",
					Type: "series", Title: parent, SortTitle: parent,
					Path:    "/m/" + parent,
					AddedAt: now, UpdatedAt: now, IsAvailable: true,
				})
			}
		}
		_ = repos.Items.Create(ctx, &db.Item{
			ID: fmt.Sprintf("i-%05d", i), LibraryID: "lib-mov",
			ParentID:    parent,
			Type:        typ,
			Title:       fmt.Sprintf("Title %d", i),
			SortTitle:   fmt.Sprintf("Title %05d", i),
			Path:        fmt.Sprintf("/m/%d.mkv", i),
			AddedAt:     now, UpdatedAt: now, IsAvailable: true,
			DurationTicks: ticks90min,
		})

		// Touch ~30 % of items so Trending has plays.
		if i%3 == 0 {
			played := now.AddDate(0, 0, -((i * 7) % 7))
			_ = repos.UserData.Upsert(ctx, &db.UserData{
				UserID: "u-1", ItemID: fmt.Sprintf("i-%05d", i),
				PositionTicks: ticks90min / 2, PlayCount: 1,
				LastPlayedAt: &played, UpdatedAt: now,
			})
		}
	}
	return db.NewHomeRepository(testutil.Driver(), database), "u-1"
}

// newBenchHomeRepoLiveTV seeds a live-tv library with `n` channels
// + EPG programs for "now" so the LEFT JOIN matches. Returns the
// HomeRepository + the caller's user id. The user has a single
// favourite channel so the favourite-first ORDER BY has work.
func newBenchHomeRepoLiveTV(b *testing.B, n int) (*db.HomeRepository, string) {
	b.Helper()
	database := testutil.NewTestDB(b)
	repos := db.NewRepositories(testutil.Driver(), database)
	ctx := context.Background()

	now := time.Now().UTC()
	if err := repos.Libraries.Create(ctx, &db.Library{
		ID: "lib-tv", Name: "Live TV", ContentType: "livetv",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		b.Fatalf("create library: %v", err)
	}
	if err := repos.Users.Create(ctx, &authmodel.User{
		ID: "u-1", Username: "u1", DisplayName: "U1",
		PasswordHash: "x", Role: "admin", IsActive: true, CreatedAt: now,
	}); err != nil {
		b.Fatalf("create user: %v", err)
	}
	if err := repos.Libraries.GrantAccess(ctx, "u-1", "lib-tv"); err != nil {
		b.Fatalf("grant access tv: %v", err)
	}

	chRepo := db.NewChannelRepository(testutil.Driver(), database)
	for i := 0; i < n; i++ {
		_ = chRepo.Create(ctx, &db.Channel{
			ID: fmt.Sprintf("ch-%05d", i), LibraryID: "lib-tv",
			Name: fmt.Sprintf("Ch %d", i), Number: i + 1,
			GroupName: "Bench",
			StreamURL: fmt.Sprintf("http://stream.example.com/%d.ts", i),
			TvgID:     fmt.Sprintf("tv-%d", i),
			IsActive:  true,
			AddedAt:   now,
		})
	}

	// Seed EPG: program currently airing for every channel via raw
	// SQL — there's no test helper for EPGProgram and the schema
	// (`epg_programs`) is plenty stable.
	if _, err := database.ExecContext(ctx, `
		CREATE TEMPORARY TABLE IF NOT EXISTS epg_seed_marker (x INT)`); err == nil {
		// noop — just exercise the connection
	}
	// One favourite so the favourite-first sort exercises.
	if _, err := database.ExecContext(ctx,
		db.RewritePlaceholders(testutil.Driver(),
			`INSERT INTO user_channel_favorites (user_id, channel_id, created_at) VALUES (?, ?, ?)`),
		"u-1", "ch-00000", now); err != nil {
		b.Fatalf("seed favourite: %v", err)
	}

	startWindow := now.Add(-15 * time.Minute)
	endWindow := now.Add(45 * time.Minute)
	for i := 0; i < n; i++ {
		if _, err := database.ExecContext(ctx,
			db.RewritePlaceholders(testutil.Driver(),
				`INSERT INTO epg_programs
				    (id, channel_id, title, start_time, end_time)
				 VALUES (?, ?, ?, ?, ?)`),
			fmt.Sprintf("ep-%05d", i),
			fmt.Sprintf("ch-%05d", i),
			fmt.Sprintf("Program %d", i),
			startWindow, endWindow,
		); err != nil {
			b.Fatalf("seed epg %d: %v", i, err)
		}
	}

	return db.NewHomeRepository(testutil.Driver(), database), "u-1"
}
