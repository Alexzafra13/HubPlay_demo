package db_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

// BenchmarkChannelRepository_ListByLibrary measures the cost of the
// hot channel listing under three realistic library sizes. Run with:
//
//	go test -bench=BenchmarkChannelRepository_ListByLibrary -benchmem \
//	        -run=^$ ./internal/db/...
//
// Validates the impact of the UUU-mig migration (044) that added the
// composite index `idx_channels_library_number(library_id, number)`.
// Before the index, the ORDER BY clause forced an in-memory sort over
// the filtered set; with it, the planner walks the B-tree already
// ordered.
//
// Each sub-benchmark seeds N channels into one library, then resets
// the timer and exercises `ListByLibrary` repeatedly. b.ReportAllocs
// captures bytes/op + allocs/op so we can see Scan + struct-allocation
// overhead separately from the SQL planner cost.
func BenchmarkChannelRepository_ListByLibrary(b *testing.B) {
	for _, n := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("size=%d/active=false", n), func(b *testing.B) {
			repo, libID := newBenchChannelRepo(b, n)
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rows, err := repo.ListByLibrary(ctx, libID, false)
				if err != nil {
					b.Fatalf("list: %v", err)
				}
				if len(rows) != n {
					b.Fatalf("expected %d rows, got %d", n, len(rows))
				}
			}
		})

		b.Run(fmt.Sprintf("size=%d/active=true", n), func(b *testing.B) {
			repo, libID := newBenchChannelRepo(b, n)
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rows, err := repo.ListByLibrary(ctx, libID, true)
				if err != nil {
					b.Fatalf("list: %v", err)
				}
				if len(rows) != n {
					b.Fatalf("expected %d rows, got %d", n, len(rows))
				}
			}
		})
	}
}

// newBenchChannelRepo creates a fresh test DB + seeds N channels in
// one library. Returns the repo + the library id. The seed phase
// dominates setup time (each Create is one INSERT round-trip) but it's
// excluded from the benchmark timer via b.ResetTimer().
func newBenchChannelRepo(b *testing.B, n int) (*db.ChannelRepository, string) {
	b.Helper()
	database := testutil.NewTestDB(b)
	repos := db.NewRepositories(testutil.Driver(), database)
	ctx := context.Background()

	now := time.Now().UTC()
	if err := repos.Libraries.Create(ctx, &db.Library{
		ID: "lib-bench", Name: "Bench TV", ContentType: "livetv",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		b.Fatalf("create library: %v", err)
	}

	repo := db.NewChannelRepository(testutil.Driver(), database)
	for i := 0; i < n; i++ {
		ch := &db.Channel{
			ID:        fmt.Sprintf("ch-%05d", i),
			LibraryID: "lib-bench",
			Name:      fmt.Sprintf("Channel %05d", i),
			// Number staggered so the ORDER BY actually has work to
			// do — alphabetical-id ordering of `ch-NNNNN` would
			// already be `number` ordered if Number == i.
			Number:    (n - i) * 10,
			GroupName: "Bench",
			LogoURL:   fmt.Sprintf("http://logo.example.com/%d.png", i),
			StreamURL: fmt.Sprintf("http://stream.example.com/%d.ts", i),
			TvgID:     fmt.Sprintf("tv-%d", i),
			Language:  "es",
			Country:   "ES",
			IsActive:  true,
			AddedAt:   now,
		}
		if err := repo.Create(ctx, ch); err != nil {
			b.Fatalf("seed channel %d: %v", i, err)
		}
	}
	return repo, "lib-bench"
}
