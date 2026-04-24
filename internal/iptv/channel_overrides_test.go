package iptv

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

// TestSetChannelTvgID_PersistsAcrossM3URefresh locks down the whole
// point of the override layer: an admin edit to a channel's tvg_id
// survives a full M3U refresh.
func TestSetChannelTvgID_PersistsAcrossM3URefresh(t *testing.T) {
	unblockLoopback(t)
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(database)
	ctx := context.Background()

	// Fixture M3U server with a single channel; stream URL stays the
	// same across refreshes so the override can rematch.
	streamURL := "http://upstream.example/la1.m3u8"
	m3uSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-mpegURL")
		fmt.Fprintf(w, `#EXTM3U
#EXTINF:-1 tvg-id="old.tvg.id" tvg-logo="" group-title="Spain",La 1
%s
`, streamURL)
	}))
	defer m3uSrv.Close()

	libID := "lib-ov"
	now := time.Now()
	if err := repos.Libraries.Create(ctx, &db.Library{
		ID: libID, Name: "L", ContentType: "livetv", ScanMode: "manual",
		M3UURL:    m3uSrv.URL,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewService(repos.Channels, repos.EPGPrograms, repos.Libraries,
		repos.ChannelFavorites, repos.LibraryEPGSources, repos.ChannelOverrides,
		repos.ChannelWatchHistory,
		slog.New(slog.NewTextHandler(new(discard), nil)))

	// First import: creates a channel with tvg_id="old.tvg.id".
	if _, err := svc.RefreshM3U(ctx, libID); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	chans, err := svc.GetChannels(ctx, libID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(chans) != 1 {
		t.Fatalf("want 1 channel, got %d", len(chans))
	}
	originalID := chans[0].ID
	if chans[0].TvgID != "old.tvg.id" {
		t.Fatalf("import tvg_id = %q, want old.tvg.id", chans[0].TvgID)
	}

	// Admin fixes the tvg_id manually.
	if err := svc.SetChannelTvgID(ctx, originalID, "La1.FIXED"); err != nil {
		t.Fatalf("set tvg_id: %v", err)
	}
	chans, _ = svc.GetChannels(ctx, libID, false)
	if chans[0].TvgID != "La1.FIXED" {
		t.Fatalf("post-edit tvg_id = %q, want La1.FIXED", chans[0].TvgID)
	}

	// Second M3U refresh wipes the channels table (new random IDs) but
	// the override layer must re-apply the edit.
	if _, err := svc.RefreshM3U(ctx, libID); err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	chans, _ = svc.GetChannels(ctx, libID, false)
	if len(chans) != 1 {
		t.Fatalf("want 1 channel after refresh, got %d", len(chans))
	}
	if chans[0].ID == originalID {
		t.Errorf("channel ID should have been regenerated on refresh")
	}
	if chans[0].TvgID != "La1.FIXED" {
		t.Errorf("override lost on M3U refresh: got tvg_id = %q", chans[0].TvgID)
	}
}

// Clearing an override (empty tvg_id) must also remove the persistent
// row so the next M3U refresh doesn't resurrect the old value.
func TestSetChannelTvgID_ClearsPersistentOverride(t *testing.T) {
	unblockLoopback(t)
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(database)
	ctx := context.Background()

	streamURL := "http://upstream.example/a3.m3u8"
	m3uSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-mpegURL")
		fmt.Fprintf(w, `#EXTM3U
#EXTINF:-1 tvg-id="m3u.tvg" tvg-logo="" group-title="",A3
%s
`, streamURL)
	}))
	defer m3uSrv.Close()

	libID := "lib-clear"
	now := time.Now()
	_ = repos.Libraries.Create(ctx, &db.Library{
		ID: libID, Name: "C", ContentType: "livetv", ScanMode: "manual",
		M3UURL:    m3uSrv.URL,
		CreatedAt: now, UpdatedAt: now,
	})

	svc := NewService(repos.Channels, repos.EPGPrograms, repos.Libraries,
		repos.ChannelFavorites, repos.LibraryEPGSources, repos.ChannelOverrides,
		repos.ChannelWatchHistory,
		slog.New(slog.NewTextHandler(new(discard), nil)))

	if _, err := svc.RefreshM3U(ctx, libID); err != nil {
		t.Fatal(err)
	}
	chans, _ := svc.GetChannels(ctx, libID, false)
	chID := chans[0].ID

	// Set then clear the override.
	_ = svc.SetChannelTvgID(ctx, chID, "SomeOverride")
	_ = svc.SetChannelTvgID(ctx, chID, "")

	// Row must be gone from the overrides table.
	got, _ := repos.ChannelOverrides.Get(ctx, libID, streamURL)
	if got != nil {
		t.Errorf("override row should be deleted, got %+v", got)
	}

	// Next M3U refresh must NOT resurrect "SomeOverride" — it picks up
	// the tvg_id from the playlist.
	if _, err := svc.RefreshM3U(ctx, libID); err != nil {
		t.Fatal(err)
	}
	chans, _ = svc.GetChannels(ctx, libID, false)
	if chans[0].TvgID != "m3u.tvg" {
		t.Errorf("after clear+refresh, tvg_id = %q, want m3u.tvg", chans[0].TvgID)
	}
}

// ListChannelsWithoutEPG must hide channels that already have an EPG
// entry and surface only the orphans.
func TestListChannelsWithoutEPG_SurfaceOrphansOnly(t *testing.T) {
	unblockLoopback(t)
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(database)
	ctx := context.Background()

	libID := "lib-orphans"
	now := time.Now()
	_ = repos.Libraries.Create(ctx, &db.Library{
		ID: libID, Name: "O", ContentType: "livetv", ScanMode: "manual",
		CreatedAt: now, UpdatedAt: now,
	})
	_ = repos.Channels.Create(ctx, &db.Channel{
		ID: "ch-with-guide", LibraryID: libID, Name: "With",
		StreamURL: "http://x/a.m3u8", IsActive: true, AddedAt: now,
	})
	_ = repos.Channels.Create(ctx, &db.Channel{
		ID: "ch-orphan", LibraryID: libID, Name: "Orphan",
		StreamURL: "http://x/b.m3u8", IsActive: true, AddedAt: now,
	})

	_ = repos.EPGPrograms.ReplaceForChannel(ctx, "ch-with-guide", []*db.EPGProgram{{
		ID: "p-1", ChannelID: "ch-with-guide", Title: "Show",
		StartTime: now, EndTime: now.Add(time.Hour),
	}})

	svc := NewService(repos.Channels, repos.EPGPrograms, repos.Libraries,
		repos.ChannelFavorites, repos.LibraryEPGSources, repos.ChannelOverrides,
		repos.ChannelWatchHistory,
		slog.New(slog.NewTextHandler(new(discard), nil)))

	out, err := svc.ListChannelsWithoutEPG(ctx, libID)
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
