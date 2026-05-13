package library_test

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/event"
	"hubplay/internal/library"
	"hubplay/internal/testutil"
)

// stubFingerprintComputer feeds the orchestrator pre-baked hashes
// keyed on (item_id, window). Available() always returns true so
// the Start subscription path is exercised in tests, and Compute
// returns nil with no error for keys we didn't program — that's
// the same shape a real fpcalc miss has, so the orchestrator's
// "skip episodes we couldn't fingerprint" branch is covered too.
type stubFingerprintComputer struct {
	prints map[string][]uint32 // key: itemID + "/" + window
}

func (s *stubFingerprintComputer) Available() bool { return true }
func (s *stubFingerprintComputer) Compute(_ context.Context, itemID, _ string, window library.FingerprintWindow) ([]uint32, error) {
	return s.prints[itemID+"/"+string(window)], nil
}

// TestSegmentFingerprinter_DetectLibrary_EndToEnd wires the real
// repos plus a stub fingerprint computer to confirm the full path:
// list episodes, group by season, run the matcher, write segments
// scoped to source='fingerprint'. Crucially, it verifies that
// chapter-source rows written by SegmentDetector are NOT clobbered
// by the fingerprint pass — they coexist under the (item_id, kind,
// source) primary key.
func TestSegmentFingerprinter_DetectLibrary_EndToEnd(t *testing.T) {
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(testutil.Driver(), database)
	bus := event.NewBus(slog.Default())
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	now := time.Now()
	must(repos.Libraries.Create(context.Background(), &db.Library{
		ID: "lib-fp", Name: "TV", ContentType: "shows",
		ScanMode: "auto", ScanInterval: "6h",
		CreatedAt: now, UpdatedAt: now, Paths: []string{"/tv"},
	}))
	mustItem := func(id, parent, kind string) {
		t.Helper()
		must(repos.Items.Create(context.Background(), &db.Item{
			ID:            id,
			LibraryID:     "lib-fp",
			ParentID:      parent,
			Type:          kind,
			Title:         id,
			Path:          "/tv/" + id + ".mkv",
			AddedAt:       now,
			UpdatedAt:     now,
			IsAvailable:   true,
			DurationTicks: ticks(1800),
		}))
	}
	mustItem("series-fp", "", "series")
	mustItem("season-fp", "series-fp", "season")
	const epCount = 4
	for i := 1; i <= epCount; i++ {
		mustItem(fmt.Sprintf("ep-fp-%d", i), "season-fp", "episode")
	}

	// Build synthetic intro fingerprints: same 250-frame run shared
	// across every episode (modeling the title sequence) bracketed
	// by per-episode random pre-roll and body. Bit-flip the shared
	// chunk per episode so the matcher exercises Hamming tolerance.
	intro := makeRandHashes(0xC0FFEE, 250)
	prints := make(map[string][]uint32, epCount*2)
	for i := 1; i <= epCount; i++ {
		preroll := makeRandHashes(int64(100+i), 50+i*5)
		body := makeRandHashes(int64(200+i), 600)
		prints[fmt.Sprintf("ep-fp-%d/intro", i)] = concat(
			preroll, flipOneRandomBit(intro, int64(300+i)), body,
		)
	}
	stub := &stubFingerprintComputer{prints: prints}

	// Drop a chapter-source segment on ep-fp-1 to verify the
	// fingerprint pass leaves it alone.
	must(repos.EpisodeSegments.Replace(context.Background(), "ep-fp-1",
		db.EpisodeSegmentSourceChapter,
		[]db.EpisodeSegment{{
			Kind:       db.EpisodeSegmentRecap,
			Source:     db.EpisodeSegmentSourceChapter,
			StartTicks: ticks(5),
			EndTicks:   ticks(20),
			Confidence: 0.95,
			DetectedAt: now.Unix(),
		}},
	))

	fp := library.NewSegmentFingerprinter(repos.Items, repos.EpisodeSegments, stub, bus, slog.Default())
	if err := fp.DetectLibrary(context.Background(), "lib-fp"); err != nil {
		t.Fatalf("DetectLibrary: %v", err)
	}

	// Every episode should have an intro detected via fingerprint.
	for i := 1; i <= epCount; i++ {
		id := fmt.Sprintf("ep-fp-%d", i)
		segs, err := repos.EpisodeSegments.ListByItem(context.Background(), id)
		must(err)
		var fpIntro *db.EpisodeSegment
		for k := range segs {
			if segs[k].Kind == db.EpisodeSegmentIntro && segs[k].Source == db.EpisodeSegmentSourceFingerprint {
				fpIntro = &segs[k]
				break
			}
		}
		if fpIntro == nil {
			t.Errorf("%s: no fingerprint-source intro segment", id)
			continue
		}
		if fpIntro.Confidence < 0.5 {
			t.Errorf("%s: confidence %.2f below 0.5", id, fpIntro.Confidence)
		}
		runSec := float64(fpIntro.EndTicks-fpIntro.StartTicks) / 10_000_000
		if runSec < 20 {
			t.Errorf("%s: run %.1fs shorter than expected ~30s", id, runSec)
		}
	}

	// ep-fp-1 must still have its chapter-source recap.
	ep1, err := repos.EpisodeSegments.ListByItem(context.Background(), "ep-fp-1")
	must(err)
	hasChapterRecap := false
	for _, s := range ep1 {
		if s.Source == db.EpisodeSegmentSourceChapter && s.Kind == db.EpisodeSegmentRecap {
			hasChapterRecap = true
			break
		}
	}
	if !hasChapterRecap {
		t.Errorf("ep-fp-1: chapter-source recap was clobbered by the fingerprint pass")
	}

	// Re-run the detector. The fingerprint-source rows must be
	// replaced (not duplicated) and the chapter-source row must
	// still survive.
	if err := fp.DetectLibrary(context.Background(), "lib-fp"); err != nil {
		t.Fatalf("DetectLibrary rerun: %v", err)
	}
	ep1Rerun, err := repos.EpisodeSegments.ListByItem(context.Background(), "ep-fp-1")
	must(err)
	fpCount := 0
	for _, s := range ep1Rerun {
		if s.Source == db.EpisodeSegmentSourceFingerprint {
			fpCount++
		}
	}
	if fpCount != 1 {
		t.Errorf("ep-fp-1 after rerun: expected 1 fingerprint-source row, got %d", fpCount)
	}
}

// TestSegmentFingerprinter_SeasonWithOneEpisode skips singleton
// seasons silently — the matcher needs at least 2 episodes to
// compare and singletons would otherwise spam zero-result logs.
func TestSegmentFingerprinter_SeasonWithOneEpisode(t *testing.T) {
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(testutil.Driver(), database)
	bus := event.NewBus(slog.Default())
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	must(repos.Libraries.Create(context.Background(), &db.Library{
		ID: "lib-solo", Name: "Solo", ContentType: "shows",
		ScanMode: "auto", ScanInterval: "6h",
		CreatedAt: now, UpdatedAt: now, Paths: []string{"/tv"},
	}))
	for _, item := range []db.Item{
		{ID: "series-solo", LibraryID: "lib-solo", Type: "series", Title: "Solo", Path: "/tv/solo", AddedAt: now, UpdatedAt: now, IsAvailable: true},
		{ID: "season-solo", LibraryID: "lib-solo", ParentID: "series-solo", Type: "season", Title: "S1", Path: "/tv/solo/s1", AddedAt: now, UpdatedAt: now, IsAvailable: true},
		{ID: "ep-solo", LibraryID: "lib-solo", ParentID: "season-solo", Type: "episode", Title: "ep1", Path: "/tv/solo/ep1.mkv", AddedAt: now, UpdatedAt: now, IsAvailable: true, DurationTicks: ticks(1800)},
	} {
		i := item
		must(repos.Items.Create(context.Background(), &i))
	}

	stub := &stubFingerprintComputer{prints: map[string][]uint32{
		"ep-solo/intro": makeRandHashes(1, 800),
	}}
	fp := library.NewSegmentFingerprinter(repos.Items, repos.EpisodeSegments, stub, bus, slog.Default())
	if err := fp.DetectLibrary(context.Background(), "lib-solo"); err != nil {
		t.Fatalf("DetectLibrary: %v", err)
	}
	rows, err := repos.EpisodeSegments.ListByItem(context.Background(), "ep-solo")
	must(err)
	if len(rows) != 0 {
		t.Errorf("singleton season should produce 0 segments, got %d", len(rows))
	}
}

// makeRandHashes mirrors the matcher-test helper but lives in the
// _test package so the integration tests stay self-contained.
func makeRandHashes(seed int64, n int) []uint32 {
	r := rand.New(rand.NewSource(seed))
	out := make([]uint32, n)
	for i := range out {
		out[i] = r.Uint32()
	}
	return out
}

func flipOneRandomBit(in []uint32, seed int64) []uint32 {
	r := rand.New(rand.NewSource(seed))
	out := make([]uint32, len(in))
	for i, h := range in {
		out[i] = h ^ (1 << uint(r.Intn(32)))
	}
	return out
}

func concat(slices ...[]uint32) []uint32 {
	total := 0
	for _, s := range slices {
		total += len(s)
	}
	out := make([]uint32, 0, total)
	for _, s := range slices {
		out = append(out, s...)
	}
	return out
}
