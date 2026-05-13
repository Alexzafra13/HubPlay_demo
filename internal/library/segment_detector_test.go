package library_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/event"
	"hubplay/internal/library"
	"hubplay/internal/testutil"
)

// 1s in 10M-tick units. Matches probe.DurationTicks.
const oneSecondTicks int64 = 10_000_000

func ticks(seconds int64) int64 { return seconds * oneSecondTicks }

func TestDetectFromChapters_NoChapters(t *testing.T) {
	got := library.DetectFromChapters(ticks(1800), nil, 0)
	if got != nil {
		t.Errorf("expected nil for no chapters, got %d", len(got))
	}
}

func TestDetectFromChapters_ClassicEpisodeLayout(t *testing.T) {
	// Recap → Opening → content → Credits — the textbook BD/Plex
	// chapter layout. Should yield three segments at confidence 0.95
	// in (recap, intro, outro) order.
	chapters := []*db.Chapter{
		{StartTicks: ticks(0), EndTicks: ticks(45), Title: "Recap"},
		{StartTicks: ticks(45), EndTicks: ticks(135), Title: "Opening Credits"},
		{StartTicks: ticks(135), EndTicks: ticks(1700), Title: "Episode"},
		{StartTicks: ticks(1700), EndTicks: ticks(1800), Title: "End Credits"},
	}
	got := library.DetectFromChapters(ticks(1800), chapters, 1700000000)

	if len(got) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(got))
	}
	if got[0].Kind != db.EpisodeSegmentRecap {
		t.Errorf("expected first segment to be recap, got %q", got[0].Kind)
	}
	if got[1].Kind != db.EpisodeSegmentIntro {
		t.Errorf("expected second segment to be intro, got %q", got[1].Kind)
	}
	if got[1].StartTicks != ticks(45) || got[1].EndTicks != ticks(135) {
		t.Errorf("intro range mismatch: got [%d, %d]", got[1].StartTicks, got[1].EndTicks)
	}
	if got[2].Kind != db.EpisodeSegmentOutro {
		t.Errorf("expected third segment to be outro, got %q", got[2].Kind)
	}
	if got[2].StartTicks != ticks(1700) {
		t.Errorf("expected outro at 1700s, got %d", got[2].StartTicks)
	}
	for i, s := range got {
		if s.Confidence != 0.95 {
			t.Errorf("segment %d confidence = %v, want 0.95", i, s.Confidence)
		}
		if s.DetectedAt != 1700000000 {
			t.Errorf("segment %d detected_at = %d, want 1700000000", i, s.DetectedAt)
		}
	}
}

func TestDetectFromChapters_OutroPositionGuard(t *testing.T) {
	// "Opening Credits" appearing in a flashback IN THE FIRST HALF
	// of a 30-min episode is a real edge case from anthology shows.
	// We pick the LAST outro match in the second half — a leading
	// "Opening Credits" line should NOT be misread as an outro.
	chapters := []*db.Chapter{
		{StartTicks: ticks(120), EndTicks: ticks(150), Title: "Opening Credits"}, // mid-episode flashback
		{StartTicks: ticks(1700), EndTicks: ticks(1800), Title: "Credits"},       // real outro
	}
	got := library.DetectFromChapters(ticks(1800), chapters, 1700000000)

	if len(got) != 2 {
		t.Fatalf("expected 2 segments (intro flashback + real outro), got %d", len(got))
	}
	// First match is "Opening Credits" → intro (since title starts
	// with "Opening"). The flashback is in the first half so it
	// passes the position guard for intros.
	if got[0].Kind != db.EpisodeSegmentIntro {
		t.Errorf("expected intro from flashback, got %q", got[0].Kind)
	}
	if got[1].Kind != db.EpisodeSegmentOutro {
		t.Errorf("expected outro at end, got %q", got[1].Kind)
	}
	if got[1].StartTicks != ticks(1700) {
		t.Errorf("outro start = %d, want %d (the late one, not the flashback)", got[1].StartTicks, ticks(1700))
	}
}

func TestDetectFromChapters_NoMatchingTitles(t *testing.T) {
	// Chapters present but none of the titles match any kind regex.
	// Returns no segments — false positives are worse than misses
	// for this feature (a "Skip intro" button at the wrong moment
	// is a worse UX than no button).
	chapters := []*db.Chapter{
		{StartTicks: ticks(0), EndTicks: ticks(600), Title: "Chapter 1"},
		{StartTicks: ticks(600), EndTicks: ticks(1200), Title: "Chapter 2"},
		{StartTicks: ticks(1200), EndTicks: ticks(1800), Title: "Chapter 3"},
	}
	got := library.DetectFromChapters(ticks(1800), chapters, 0)
	if len(got) != 0 {
		t.Errorf("expected zero matches for unnamed chapters, got %d", len(got))
	}
}

func TestDetectFromChapters_FixesMissingEndTicks(t *testing.T) {
	// Some encoders emit chapters with end_ticks == 0. The CHECK
	// constraint requires end > start, so the detector's emit step
	// substitutes a 1-second window so the segment can be persisted.
	chapters := []*db.Chapter{
		{StartTicks: ticks(60), EndTicks: 0, Title: "Intro"},
	}
	got := library.DetectFromChapters(ticks(1800), chapters, 0)
	if len(got) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(got))
	}
	if got[0].EndTicks <= got[0].StartTicks {
		t.Errorf("end_ticks = %d must be greater than start_ticks = %d",
			got[0].EndTicks, got[0].StartTicks)
	}
	if got[0].EndTicks != ticks(60)+oneSecondTicks {
		t.Errorf("expected default 1s window, got %d ticks", got[0].EndTicks-got[0].StartTicks)
	}
}

func TestDetectFromChapters_ZeroDurationSkipsPositionGuard(t *testing.T) {
	// Item with durationTicks=0 (e.g. unprobed) — we still want title
	// matching to work, just without the position sanity check.
	chapters := []*db.Chapter{
		{StartTicks: ticks(60), EndTicks: ticks(150), Title: "Intro"},
		{StartTicks: ticks(200), EndTicks: ticks(220), Title: "Credits"},
	}
	got := library.DetectFromChapters(0, chapters, 0)
	if len(got) != 2 {
		t.Fatalf("expected 2 segments without position guard, got %d", len(got))
	}
}

// TestSegmentDetector_DetectLibrary_EndToEnd wires the real repos
// against a temp DB to confirm the full path: list episodes, read
// chapters, write segments. It also asserts that re-running the
// detector replaces the prior chapter-source rows cleanly.
func TestSegmentDetector_DetectLibrary_EndToEnd(t *testing.T) {
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(testutil.Driver(), database)
	bus := event.NewBus(slog.Default())

	// Seed: one library, one series, one season, two episodes.
	now := time.Now()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(repos.Libraries.Create(context.Background(), &db.Library{
		ID: "lib-1", Name: "TV", ContentType: "shows",
		ScanMode: "auto", ScanInterval: "6h",
		CreatedAt: now, UpdatedAt: now, Paths: []string{"/tv"},
	}))
	mustItem := func(id, parent, kind, title string) {
		t.Helper()
		must(repos.Items.Create(context.Background(), &db.Item{
			ID:            id,
			LibraryID:     "lib-1",
			ParentID:      parent,
			Type:          kind,
			Title:         title,
			Path:          "/tv/" + id,
			AddedAt:       now,
			UpdatedAt:     now,
			IsAvailable:   true,
			DurationTicks: ticks(1800),
		}))
	}
	mustItem("series-1", "", "series", "Show")
	mustItem("season-1", "series-1", "season", "Season 1")
	mustItem("ep-1", "season-1", "episode", "S01E01")
	mustItem("ep-2", "season-1", "episode", "S01E02")

	// ep-1 has chapters → should produce intro + outro.
	must(repos.Chapters.Replace(context.Background(), "ep-1", []db.Chapter{
		{ItemID: "ep-1", StartTicks: ticks(45), EndTicks: ticks(135), Title: "Intro"},
		{ItemID: "ep-1", StartTicks: ticks(1700), EndTicks: ticks(1800), Title: "Credits"},
	}))
	// ep-2 has no chapters → should produce nothing.

	det := library.NewSegmentDetector(repos.Items, repos.Chapters, repos.EpisodeSegments, bus, slog.Default())
	if err := det.DetectLibrary(context.Background(), "lib-1"); err != nil {
		t.Fatalf("DetectLibrary: %v", err)
	}

	gotEp1, err := repos.EpisodeSegments.ListByItem(context.Background(), "ep-1")
	must(err)
	if len(gotEp1) != 2 {
		t.Errorf("ep-1 expected 2 segments, got %d", len(gotEp1))
	}
	gotEp2, err := repos.EpisodeSegments.ListByItem(context.Background(), "ep-2")
	must(err)
	if len(gotEp2) != 0 {
		t.Errorf("ep-2 should have no segments, got %d", len(gotEp2))
	}

	// Re-run with a different chapter set on ep-1 — the prior
	// chapter-source rows must be replaced, not duplicated.
	must(repos.Chapters.Replace(context.Background(), "ep-1", []db.Chapter{
		{ItemID: "ep-1", StartTicks: ticks(60), EndTicks: ticks(150), Title: "Opening"},
	}))
	if err := det.DetectLibrary(context.Background(), "lib-1"); err != nil {
		t.Fatalf("DetectLibrary rerun: %v", err)
	}
	gotRerun, err := repos.EpisodeSegments.ListByItem(context.Background(), "ep-1")
	must(err)
	if len(gotRerun) != 1 {
		t.Errorf("ep-1 after rerun expected 1 segment (intro only), got %d", len(gotRerun))
	}
	if len(gotRerun) > 0 && gotRerun[0].StartTicks != ticks(60) {
		t.Errorf("ep-1 after rerun expected intro at 60s, got %d", gotRerun[0].StartTicks)
	}
}
