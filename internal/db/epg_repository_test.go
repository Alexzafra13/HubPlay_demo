package db_test

import (
	"context"
	"fmt"
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

// TestEPG_BulkSchedule_LargeList exercises the repo-level chunker: we
// push more than a single chunk's worth of channel IDs through the
// IN() clause and verify every seeded program surfaces. Catches any
// regression where the chunker drops a boundary row or re-queries the
// same chunk twice.
func TestEPG_BulkSchedule_LargeList(t *testing.T) {
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(database)
	ctx := context.Background()

	libID := "lib-epg-large"
	now := time.Now()
	_ = repos.Libraries.Create(ctx, &db.Library{
		ID: libID, Name: "Large", ContentType: "livetv",
		CreatedAt: now, UpdatedAt: now,
	})

	// 1,200 channels comfortably exceeds the 500-chunk size, so the
	// chunker has to run at least three iterations.
	const total = 1200
	ids := make([]string, total)
	for i := 0; i < total; i++ {
		id := fmt.Sprintf("ch-big-%04d", i)
		ids[i] = id
		if err := repos.Channels.Create(ctx, makeChannel(id, libID, "C"+id, i+1, true)); err != nil {
			t.Fatalf("create channel %s: %v", id, err)
		}
		prog := makeProgram("p-"+id, id, "T-"+id, now.Add(-30*time.Minute), now.Add(30*time.Minute))
		if err := repos.EPGPrograms.ReplaceForChannel(ctx, id, []*db.EPGProgram{prog}); err != nil {
			t.Fatalf("seed epg %s: %v", id, err)
		}
	}

	got, err := repos.EPGPrograms.BulkSchedule(ctx, ids, now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("bulk schedule: %v", err)
	}
	if len(got) != total {
		t.Fatalf("channels returned = %d, want %d", len(got), total)
	}
	for _, id := range ids {
		progs, ok := got[id]
		if !ok {
			t.Fatalf("channel %s missing from bulk result", id)
		}
		if len(progs) != 1 || progs[0].Title != "T-"+id {
			t.Fatalf("channel %s programs = %v", id, progs)
		}
	}
}

// TestEPG_XMLTVTimeRoundtrip covers the runtime bug the user hit in
// their Docker deployment: programs parsed from davidmuma's XMLTV
// carry a named timezone (+0200 for CET), which modernc.org/sqlite
// persisted via time.Time.String() — "2026-04-24 12:00:00 +0200 +0200"
// — a format the driver's default Scan path can't deserialise. The
// repository now normalises all times to UTC before insert, and every
// read coerces whatever the driver hands us.
func TestEPG_XMLTVTimeRoundtrip(t *testing.T) {
	repo, _, _ := setupEPGTest(t)
	ctx := context.Background()

	// Shape mirrors internal/iptv/xmltv.go parseXMLTVTime output.
	start, err := time.Parse("20060102150405 -0700", "20260424120000 +0200")
	if err != nil {
		t.Fatalf("parse start: %v", err)
	}
	end, err := time.Parse("20060102150405 -0700", "20260424130000 +0200")
	if err != nil {
		t.Fatalf("parse end: %v", err)
	}

	prog := &db.EPGProgram{
		ID: "p-xmltv", ChannelID: "ch-epg-1", Title: "Telediario",
		StartTime: start, EndTime: end,
	}
	if err := repo.ReplaceForChannel(ctx, "ch-epg-1", []*db.EPGProgram{prog}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// BulkSchedule round-trip — the path the frontend hits.
	bulk, err := repo.BulkSchedule(ctx,
		[]string{"ch-epg-1"}, start.Add(-1*time.Hour), end.Add(1*time.Hour))
	if err != nil {
		t.Fatalf("bulk: %v", err)
	}
	if len(bulk["ch-epg-1"]) != 1 {
		t.Fatalf("bulk rows = %d, want 1", len(bulk["ch-epg-1"]))
	}
	if !bulk["ch-epg-1"][0].StartTime.Equal(start) {
		t.Errorf("bulk start = %v, want equal to %v", bulk["ch-epg-1"][0].StartTime, start)
	}

	// Single-channel Schedule and NowPlaying — same storage, would hit
	// the same bug if the adapter still used sqlc-generated scans.
	sched, err := repo.Schedule(ctx, "ch-epg-1", start.Add(-1*time.Hour), end.Add(1*time.Hour))
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if len(sched) != 1 || !sched[0].EndTime.Equal(end) {
		t.Fatalf("schedule unexpected: %v", sched)
	}
}

// TestEPG_CoerceSQLiteTime_LegacyString asserts the scanner handles
// rows still persisted in the Go-stringer format by older builds. The
// scenario: a user upgrades, has EPG rows from the pre-UTC-fix build,
// opens the guide before running "Refresh EPG". Those reads must not 500.
func TestEPG_CoerceSQLiteTime_LegacyString(t *testing.T) {
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(database)
	ctx := context.Background()

	now := time.Now()
	_ = repos.Libraries.Create(ctx, &db.Library{
		ID: "lib-legacy", Name: "L", ContentType: "livetv",
		CreatedAt: now, UpdatedAt: now,
	})
	_ = repos.Channels.Create(ctx, makeChannel("ch-legacy", "lib-legacy", "L", 1, true))

	// Plant a row with the exact legacy text form a pre-fix build
	// would have written when the driver's time encoder fell through
	// to fmt.Sprint. This bypasses the UTC-normalising ReplaceForChannel
	// so we can reproduce the legacy shape deterministically.
	_, err := database.ExecContext(ctx, `INSERT INTO epg_programs
		(id, channel_id, title, description, category, icon_url, start_time, end_time)
		VALUES ('p-legacy', 'ch-legacy', 'Legacy', '', '', '',
		        '2026-04-24 10:00:00 +0000 UTC', '2026-04-24 11:00:00 +0000 UTC')`)
	if err != nil {
		t.Fatalf("seed legacy: %v", err)
	}

	bulk, err := repos.EPGPrograms.BulkSchedule(ctx,
		[]string{"ch-legacy"},
		time.Date(2026, 4, 24, 9, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("bulk: %v", err)
	}
	if len(bulk["ch-legacy"]) != 1 {
		t.Fatalf("legacy row not parsed: %v", bulk)
	}
	want := time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC)
	if !bulk["ch-legacy"][0].StartTime.Equal(want) {
		t.Errorf("parsed legacy start = %v, want %v", bulk["ch-legacy"][0].StartTime, want)
	}
}

// TestEPG_BulkSchedule_DedupesDuplicateIDs guards against the same
// channel id landing in two different chunks and doubling up in the
// merged map.
func TestEPG_BulkSchedule_DedupesDuplicateIDs(t *testing.T) {
	repo, _, _ := setupEPGTest(t)
	ctx := context.Background()

	now := time.Now()
	_ = repo.ReplaceForChannel(ctx, "ch-epg-1", []*db.EPGProgram{
		makeProgram("p1", "ch-epg-1", "Show", now.Add(-1*time.Hour), now.Add(1*time.Hour)),
	})

	got, err := repo.BulkSchedule(ctx,
		[]string{"ch-epg-1", "ch-epg-1", "ch-epg-1"},
		now.Add(-2*time.Hour), now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got["ch-epg-1"]) != 1 {
		t.Fatalf("duplicate ids produced %d rows, want 1", len(got["ch-epg-1"]))
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
