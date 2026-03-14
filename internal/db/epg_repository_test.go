package db_test

import (
	"context"
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

func setupEPGTest(t *testing.T) (*db.EPGProgramRepository, *db.ChannelRepository, string) {
	t.Helper()
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(database)
	ctx := context.Background()

	now := time.Now()
	_ = repos.Libraries.Create(ctx, &db.Library{
		ID: "lib-epg", Name: "Live TV", ContentType: "livetv",
		CreatedAt: now, UpdatedAt: now,
	})

	_ = repos.Channels.Create(ctx, makeChannel("ch-epg-1", "lib-epg", "BBC One", 1, true))
	_ = repos.Channels.Create(ctx, makeChannel("ch-epg-2", "lib-epg", "CNN", 2, true))

	return repos.EPGPrograms, repos.Channels, "lib-epg"
}

func makeProgram(id, channelID, title string, start, end time.Time) *db.EPGProgram {
	return &db.EPGProgram{
		ID: id, ChannelID: channelID, Title: title,
		Description: title + " description", Category: "General",
		StartTime: start, EndTime: end,
	}
}

func TestEPG_ReplaceAndSchedule(t *testing.T) {
	repo, _, _ := setupEPGTest(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Hour)
	programs := []*db.EPGProgram{
		makeProgram("p1", "ch-epg-1", "Morning Show", now.Add(-2*time.Hour), now.Add(-1*time.Hour)),
		makeProgram("p2", "ch-epg-1", "News", now.Add(-1*time.Hour), now.Add(1*time.Hour)),
		makeProgram("p3", "ch-epg-1", "Evening Movie", now.Add(1*time.Hour), now.Add(3*time.Hour)),
	}

	err := repo.ReplaceForChannel(ctx, "ch-epg-1", programs)
	if err != nil {
		t.Fatal(err)
	}

	// Full schedule (wide range)
	schedule, err := repo.Schedule(ctx, "ch-epg-1", now.Add(-3*time.Hour), now.Add(4*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(schedule) != 3 {
		t.Fatalf("expected 3 programs, got %d", len(schedule))
	}

	// Filtered schedule: only programs overlapping [now-30m, now+30m]
	filtered, err := repo.Schedule(ctx, "ch-epg-1", now.Add(-30*time.Minute), now.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	// "News" overlaps this window (-1h to +1h)
	if len(filtered) != 1 {
		t.Fatalf("expected 1 program in window, got %d", len(filtered))
	}
	if filtered[0].Title != "News" {
		t.Errorf("expected News, got %q", filtered[0].Title)
	}
}

func TestEPG_ReplaceDeletesOld(t *testing.T) {
	repo, _, _ := setupEPGTest(t)
	ctx := context.Background()

	now := time.Now()
	old := []*db.EPGProgram{
		makeProgram("old-1", "ch-epg-1", "Old Show", now, now.Add(time.Hour)),
	}
	_ = repo.ReplaceForChannel(ctx, "ch-epg-1", old)

	// Replace with new
	fresh := []*db.EPGProgram{
		makeProgram("new-1", "ch-epg-1", "New Show", now, now.Add(time.Hour)),
	}
	_ = repo.ReplaceForChannel(ctx, "ch-epg-1", fresh)

	schedule, _ := repo.Schedule(ctx, "ch-epg-1", now.Add(-time.Hour), now.Add(2*time.Hour))
	if len(schedule) != 1 {
		t.Fatalf("expected 1 program after replace, got %d", len(schedule))
	}
	if schedule[0].Title != "New Show" {
		t.Errorf("expected New Show, got %q", schedule[0].Title)
	}
}

func TestEPG_NowPlaying(t *testing.T) {
	repo, _, _ := setupEPGTest(t)
	ctx := context.Background()

	now := time.Now()
	programs := []*db.EPGProgram{
		makeProgram("past", "ch-epg-1", "Ended", now.Add(-2*time.Hour), now.Add(-1*time.Hour)),
		makeProgram("current", "ch-epg-1", "Live Now", now.Add(-30*time.Minute), now.Add(30*time.Minute)),
		makeProgram("future", "ch-epg-1", "Coming Up", now.Add(1*time.Hour), now.Add(2*time.Hour)),
	}
	_ = repo.ReplaceForChannel(ctx, "ch-epg-1", programs)

	np, err := repo.NowPlaying(ctx, "ch-epg-1")
	if err != nil {
		t.Fatal(err)
	}
	if np == nil {
		t.Fatal("expected now playing, got nil")
	}
	if np.Title != "Live Now" {
		t.Errorf("now playing = %q, want Live Now", np.Title)
	}

	// Channel with no programs
	np2, err := repo.NowPlaying(ctx, "ch-epg-2")
	if err != nil {
		t.Fatal(err)
	}
	if np2 != nil {
		t.Error("expected nil for channel with no programs")
	}
}

func TestEPG_BulkSchedule(t *testing.T) {
	repo, _, _ := setupEPGTest(t)
	ctx := context.Background()

	now := time.Now()
	_ = repo.ReplaceForChannel(ctx, "ch-epg-1", []*db.EPGProgram{
		makeProgram("p1", "ch-epg-1", "BBC Show", now.Add(-1*time.Hour), now.Add(1*time.Hour)),
	})
	_ = repo.ReplaceForChannel(ctx, "ch-epg-2", []*db.EPGProgram{
		makeProgram("p2", "ch-epg-2", "CNN Show", now.Add(-1*time.Hour), now.Add(1*time.Hour)),
	})

	schedules, err := repo.BulkSchedule(ctx, []string{"ch-epg-1", "ch-epg-2"}, now.Add(-2*time.Hour), now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(schedules) != 2 {
		t.Fatalf("expected 2 channels in bulk schedule, got %d", len(schedules))
	}
	if len(schedules["ch-epg-1"]) != 1 || schedules["ch-epg-1"][0].Title != "BBC Show" {
		t.Errorf("ch-epg-1 schedule unexpected: %v", schedules["ch-epg-1"])
	}
	if len(schedules["ch-epg-2"]) != 1 || schedules["ch-epg-2"][0].Title != "CNN Show" {
		t.Errorf("ch-epg-2 schedule unexpected: %v", schedules["ch-epg-2"])
	}

	// Empty channel list
	empty, err := repo.BulkSchedule(ctx, []string{}, now, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Error("expected empty map for empty channel list")
	}
}

func TestEPG_CleanupOld(t *testing.T) {
	repo, _, _ := setupEPGTest(t)
	ctx := context.Background()

	now := time.Now()
	programs := []*db.EPGProgram{
		makeProgram("old", "ch-epg-1", "Old Show", now.Add(-48*time.Hour), now.Add(-47*time.Hour)),
		makeProgram("recent", "ch-epg-1", "Recent", now.Add(-1*time.Hour), now.Add(1*time.Hour)),
	}
	_ = repo.ReplaceForChannel(ctx, "ch-epg-1", programs)

	deleted, err := repo.CleanupOld(ctx, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}

	// Recent should survive
	schedule, _ := repo.Schedule(ctx, "ch-epg-1", now.Add(-2*time.Hour), now.Add(2*time.Hour))
	if len(schedule) != 1 {
		t.Fatalf("expected 1 surviving program, got %d", len(schedule))
	}
	if schedule[0].Title != "Recent" {
		t.Errorf("surviving program = %q, want Recent", schedule[0].Title)
	}
}
