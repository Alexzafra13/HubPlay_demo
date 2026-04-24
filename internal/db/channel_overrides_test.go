package db_test

import (
	"context"
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

func setupOverrideTest(t *testing.T) (*db.Repositories, string) {
	t.Helper()
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(database)
	ctx := context.Background()

	libID := "lib-ovr"
	now := time.Now()
	if err := repos.Libraries.Create(ctx, &db.Library{
		ID: libID, Name: "O", ContentType: "livetv", ScanMode: "manual",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed library: %v", err)
	}
	return repos, libID
}

func TestChannelOverride_UpsertReplacesExisting(t *testing.T) {
	repos, libID := setupOverrideTest(t)
	ctx := context.Background()

	first := &db.ChannelOverride{
		LibraryID: libID,
		StreamURL: "http://example/la1.m3u8",
		TvgID:     "La1.ES",
	}
	if err := repos.ChannelOverrides.Upsert(ctx, first); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	// Updating the tvg_id replaces the row in place.
	second := &db.ChannelOverride{
		LibraryID: libID,
		StreamURL: "http://example/la1.m3u8",
		TvgID:     "La1HD",
	}
	if err := repos.ChannelOverrides.Upsert(ctx, second); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	got, err := repos.ChannelOverrides.Get(ctx, libID, "http://example/la1.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	if got.TvgID != "La1HD" {
		t.Errorf("tvg_id = %q, want La1HD", got.TvgID)
	}
}

// The post-import hook: ApplyToLibrary must rewrite the tvg_id of
// every matching channel in one transaction. Drives the whole
// "override survives M3U refresh" promise.
func TestChannelOverride_ApplyToLibraryRewritesTvgID(t *testing.T) {
	repos, libID := setupOverrideTest(t)
	ctx := context.Background()

	// Mirror a real import: channels with random IDs but stable stream URLs.
	_ = repos.Channels.Create(ctx, makeChannel("fresh-1", libID, "La 1", 1, true))
	_ = repos.Channels.Create(ctx, makeChannel("fresh-2", libID, "A3", 2, true))
	// The makeChannel helper sets StreamURL to "http://stream.com/<id>"; reuse
	// that so the override row keys cleanly.

	// Admin override written before the import (or persisted across).
	if err := repos.ChannelOverrides.Upsert(ctx, &db.ChannelOverride{
		LibraryID: libID,
		StreamURL: "http://stream.com/fresh-1",
		TvgID:     "La1.OVERRIDE",
	}); err != nil {
		t.Fatal(err)
	}

	applied, err := repos.ChannelOverrides.ApplyToLibrary(ctx, libID)
	if err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatalf("applied = %d, want 1", applied)
	}
	got, _ := repos.Channels.GetByID(ctx, "fresh-1")
	if got.TvgID != "La1.OVERRIDE" {
		t.Errorf("tvg_id after apply = %q, want La1.OVERRIDE", got.TvgID)
	}
	// Other channel untouched.
	untouched, _ := repos.Channels.GetByID(ctx, "fresh-2")
	if untouched.TvgID == "La1.OVERRIDE" {
		t.Errorf("other channel should not have been touched")
	}
}

// Orphaned overrides (stream URL no longer in the playlist) should
// silently no-op, not error. Typical scenario: a CDN rotates a URL
// and the admin's override stays in the table awaiting its return.
func TestChannelOverride_ApplyToLibrary_Orphan(t *testing.T) {
	repos, libID := setupOverrideTest(t)
	ctx := context.Background()
	_ = repos.Channels.Create(ctx, makeChannel("real-1", libID, "Real", 1, true))

	if err := repos.ChannelOverrides.Upsert(ctx, &db.ChannelOverride{
		LibraryID: libID,
		StreamURL: "http://gone/nothing.m3u8",
		TvgID:     "ghost",
	}); err != nil {
		t.Fatal(err)
	}
	applied, err := repos.ChannelOverrides.ApplyToLibrary(ctx, libID)
	if err != nil {
		t.Fatal(err)
	}
	if applied != 0 {
		t.Errorf("orphan override should apply to 0 rows, got %d", applied)
	}
}

func TestChannelOverride_DeleteIsIdempotent(t *testing.T) {
	repos, libID := setupOverrideTest(t)
	ctx := context.Background()

	// Deleting a non-existent row must not error.
	if err := repos.ChannelOverrides.Delete(ctx, libID, "http://never/there"); err != nil {
		t.Fatalf("delete missing: %v", err)
	}

	// Round-trip.
	_ = repos.ChannelOverrides.Upsert(ctx, &db.ChannelOverride{
		LibraryID: libID, StreamURL: "http://x/y", TvgID: "z",
	})
	if err := repos.ChannelOverrides.Delete(ctx, libID, "http://x/y"); err != nil {
		t.Fatal(err)
	}
	got, _ := repos.ChannelOverrides.Get(ctx, libID, "http://x/y")
	if got != nil {
		t.Errorf("row still present after delete: %+v", got)
	}
}

// ─── ListWithoutEPGByLibrary ─────────────────────────────────────

func TestChannel_ListWithoutEPGByLibrary(t *testing.T) {
	repos, libID := setupOverrideTest(t)
	ctx := context.Background()

	// Two channels; only one gets an EPG entry.
	_ = repos.Channels.Create(ctx, makeChannel("ch-guide", libID, "Guide", 1, true))
	_ = repos.Channels.Create(ctx, makeChannel("ch-orphan", libID, "Orphan", 2, true))

	now := time.Now()
	prog := &db.EPGProgram{
		ID:        "p-1",
		ChannelID: "ch-guide",
		Title:     "Show",
		StartTime: now.Add(-30 * time.Minute),
		EndTime:   now.Add(30 * time.Minute),
	}
	if err := repos.EPGPrograms.ReplaceForChannel(ctx, "ch-guide", []*db.EPGProgram{prog}); err != nil {
		t.Fatal(err)
	}

	out, err := repos.Channels.ListWithoutEPGByLibrary(ctx, libID,
		now.Add(-2*time.Hour), now.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 orphan, got %d", len(out))
	}
	if out[0].ID != "ch-orphan" {
		t.Errorf("got %q, want ch-orphan", out[0].ID)
	}
}
