package db_test

import (
	"context"
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

func seedItemForStreams(t *testing.T, database *db.LibraryRepository, itemRepo *db.ItemRepository) {
	t.Helper()
	now := time.Now()
	database.Create(context.Background(), &db.Library{
		ID: "lib-s", Name: "Movies", ContentType: "movies", ScanMode: "auto",
		ScanInterval: "6h", CreatedAt: now, UpdatedAt: now, Paths: []string{"/media"},
	})
	itemRepo.Create(context.Background(), &db.Item{
		ID: "item-s", LibraryID: "lib-s", Type: "movie", Title: "Test",
		SortTitle: "test", Path: "/media/test.mkv",
		AddedAt: now, UpdatedAt: now, IsAvailable: true,
	})
}

func TestMediaStreamRepository_ReplaceAndList(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	itemRepo := db.NewItemRepository(database)
	repo := db.NewMediaStreamRepository(database)
	seedItemForStreams(t, libRepo, itemRepo)

	streams := []*db.MediaStream{
		{
			ItemID: "item-s", StreamIndex: 0, StreamType: "video",
			Codec: "h264", Width: 1920, Height: 1080, FrameRate: 23.976,
			IsDefault: true,
		},
		{
			ItemID: "item-s", StreamIndex: 1, StreamType: "audio",
			Codec: "aac", Channels: 6, SampleRate: 48000, Language: "eng",
			IsDefault: true,
		},
		{
			ItemID: "item-s", StreamIndex: 2, StreamType: "subtitle",
			Codec: "srt", Language: "spa",
		},
	}

	if err := repo.ReplaceForItem(context.Background(), "item-s", streams); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := repo.ListByItem(context.Background(), "item-s")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 streams, got %d", len(got))
	}
	if got[0].Codec != "h264" {
		t.Errorf("expected video codec 'h264', got %q", got[0].Codec)
	}
	if got[1].Channels != 6 {
		t.Errorf("expected 6 audio channels, got %d", got[1].Channels)
	}
	if got[2].Language != "spa" {
		t.Errorf("expected language 'spa', got %q", got[2].Language)
	}
}

func TestMediaStreamRepository_Replace_OverwritesPrevious(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	itemRepo := db.NewItemRepository(database)
	repo := db.NewMediaStreamRepository(database)
	seedItemForStreams(t, libRepo, itemRepo)

	// First set
	repo.ReplaceForItem(context.Background(), "item-s", []*db.MediaStream{
		{ItemID: "item-s", StreamIndex: 0, StreamType: "video", Codec: "h264"},
		{ItemID: "item-s", StreamIndex: 1, StreamType: "audio", Codec: "aac"},
	})

	// Replace with new set
	repo.ReplaceForItem(context.Background(), "item-s", []*db.MediaStream{
		{ItemID: "item-s", StreamIndex: 0, StreamType: "video", Codec: "hevc"},
	})

	got, _ := repo.ListByItem(context.Background(), "item-s")
	if len(got) != 1 {
		t.Fatalf("expected 1 stream after replace, got %d", len(got))
	}
	if got[0].Codec != "hevc" {
		t.Errorf("expected codec 'hevc', got %q", got[0].Codec)
	}
}

func TestMediaStreamRepository_ListByItem_Empty(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewMediaStreamRepository(database)

	got, err := repo.ListByItem(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 streams, got %d", len(got))
	}
}
