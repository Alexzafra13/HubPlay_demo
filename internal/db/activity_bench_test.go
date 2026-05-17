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

// BenchmarkActivityRepository_DailyWatchActivity measures the
// admin /admin/system/stream-activity sparkline query. Run with:
//
//	go test -bench=BenchmarkActivityRepository -benchmem \
//	        -run=^$ ./internal/db/...
//
// The query is a per-day GROUP BY rollup over `user_data` joined
// against `items`, scoped to the trailing N days. Dataset shape
// reflects a busy household: M users × N items each touched once
// in the window.
func BenchmarkActivityRepository_DailyWatchActivity(b *testing.B) {
	for _, n := range []int{1000, 5000} {
		b.Run(fmt.Sprintf("rows=%d", n), func(b *testing.B) {
			activity, _ := newBenchActivityRepo(b, n)
			ctx := context.Background()
			cutoff := time.Now().UTC().AddDate(0, 0, -14)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				buckets, err := activity.DailyWatchActivity(ctx, cutoff)
				if err != nil {
					b.Fatalf("daily watch: %v", err)
				}
				if len(buckets) == 0 {
					b.Fatalf("expected non-empty buckets, got 0")
				}
			}
		})
	}
}

// BenchmarkActivityRepository_TopItems measures the admin "most
// watched in trailing N days" rail. The query has an inner CTE
// folding episodes up to their series — heavier than the per-day
// sparkline.
func BenchmarkActivityRepository_TopItems(b *testing.B) {
	for _, n := range []int{1000, 5000} {
		b.Run(fmt.Sprintf("rows=%d", n), func(b *testing.B) {
			activity, _ := newBenchActivityRepo(b, n)
			ctx := context.Background()
			cutoff := time.Now().UTC().AddDate(0, 0, -7)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rows, err := activity.TopItems(ctx, cutoff, 10)
				if err != nil {
					b.Fatalf("top items: %v", err)
				}
				if len(rows) == 0 {
					b.Fatalf("expected non-empty rows, got 0")
				}
			}
		})
	}
}

// newBenchActivityRepo seeds `n` distinct (user, item) pairs into a
// fresh DB and returns the ActivityRepository. Each pair gets one
// user_data row with last_played_at scattered across the trailing
// 30 days so the day-bucket rollup has work to do.
//
// We use a handful of users (max 50) reused across items so the
// "DISTINCT user_id" count in the rollup is bounded — closer to
// reality where a household has 1-10 active accounts, not n.
func newBenchActivityRepo(b *testing.B, n int) (*db.ActivityRepository, *db.Repositories) {
	b.Helper()
	database := testutil.NewTestDB(b)
	repos := db.NewRepositories(testutil.Driver(), database)
	ctx := context.Background()

	now := time.Now().UTC()
	if err := repos.Libraries.Create(ctx, &db.Library{
		ID: "lib-bench", Name: "Bench Lib", ContentType: "movies",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		b.Fatalf("create library: %v", err)
	}

	// Cap users at 50 — realistic household size. Items can scale
	// independently.
	numUsers := n / 20
	if numUsers < 1 {
		numUsers = 1
	}
	if numUsers > 50 {
		numUsers = 50
	}
	for i := 0; i < numUsers; i++ {
		if err := repos.Users.Create(ctx, &authmodel.User{
			ID:           fmt.Sprintf("u-%03d", i),
			Username:     fmt.Sprintf("user%03d", i),
			DisplayName:  fmt.Sprintf("User %d", i),
			PasswordHash: "x", Role: "viewer", IsActive: true,
			CreatedAt: now,
		}); err != nil {
			b.Fatalf("create user %d: %v", i, err)
		}
	}

	// Items: 80 % movies, 20 % episodes (mix so the TopItems CTE
	// has rollup work). All flagged available so the WHERE clause
	// keeps them. Duration 90 minutes in 100ns ticks.
	const ticks90min int64 = 90 * 60 * 10_000_000
	for i := 0; i < n; i++ {
		typ := "movie"
		var parentID string
		if i%5 == 0 {
			typ = "episode"
			parentID = fmt.Sprintf("series-%03d", i/100) // rolls up to one of ~10 series
			// Seed the parent series + season if not present.
			if i%100 == 0 {
				if err := repos.Items.Create(ctx, &db.Item{
					ID: fmt.Sprintf("series-%03d", i/100), LibraryID: "lib-bench",
					Type: "series", Title: fmt.Sprintf("Series %d", i/100),
					SortTitle: fmt.Sprintf("Series %d", i/100),
					Path:      fmt.Sprintf("/m/s%d", i/100),
					AddedAt:   now, UpdatedAt: now, IsAvailable: true,
				}); err != nil {
					b.Fatalf("create series: %v", err)
				}
			}
		}
		if err := repos.Items.Create(ctx, &db.Item{
			ID:            fmt.Sprintf("i-%05d", i),
			LibraryID:     "lib-bench",
			ParentID:      parentID,
			Type:          typ,
			Title:         fmt.Sprintf("Title %d", i),
			SortTitle:     fmt.Sprintf("Title %05d", i),
			Path:          fmt.Sprintf("/m/i%d.mkv", i),
			AddedAt:       now,
			UpdatedAt:     now,
			IsAvailable:   true,
			DurationTicks: ticks90min,
		}); err != nil {
			b.Fatalf("create item %d: %v", i, err)
		}

		// user_data row: scatter last_played_at across the last 14
		// days so the rollup has variance per day.
		userID := fmt.Sprintf("u-%03d", i%numUsers)
		playedAt := now.AddDate(0, 0, -((i * 13) % 14)) // pseudo-random 0..13 days ago
		// Half the rows are 50 % watched, the rest 5 % (cold open).
		var pos int64 = ticks90min / 20
		if i%2 == 0 {
			pos = ticks90min / 2
		}
		if err := repos.UserData.Upsert(ctx, &db.UserData{
			UserID:        userID,
			ItemID:        fmt.Sprintf("i-%05d", i),
			PositionTicks: pos,
			PlayCount:     1,
			LastPlayedAt:  &playedAt,
			UpdatedAt:     now,
		}); err != nil {
			b.Fatalf("upsert user_data: %v", err)
		}
	}

	return db.NewActivityRepository(testutil.Driver(), database), repos
}
