package db_test

import (
	"context"
	"testing"

	"hubplay/internal/db"
	"hubplay/internal/domain"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/testutil"

	"errors"
)

func TestItemRepository_IngestItem_WritesItemStreamsChapters(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(testutil.Driver(), database)
	itemRepo := db.NewItemRepository(testutil.Driver(), database)
	streamRepo := db.NewMediaStreamRepository(testutil.Driver(), database)
	chapterRepo := db.NewChapterRepository(testutil.Driver(), database)
	seedLibraryForItems(t, libRepo)

	item := newTestItem("item-ing", "lib-1", "Ingested")
	streams := []*librarymodel.MediaStream{
		{ItemID: "item-ing", StreamIndex: 0, StreamType: "video", Codec: "h264"},
		{ItemID: "item-ing", StreamIndex: 1, StreamType: "audio", Codec: "aac"},
	}
	chapters := []librarymodel.Chapter{
		{ItemID: "item-ing", StartTicks: 0, EndTicks: 6000000000, Title: "Intro"},
	}

	if err := itemRepo.IngestItem(context.Background(), item, streams, chapters); err != nil {
		t.Fatalf("IngestItem: %v", err)
	}

	got, err := itemRepo.GetByID(context.Background(), "item-ing")
	if err != nil || got.Title != "Ingested" {
		t.Fatalf("item not persisted: got=%v err=%v", got, err)
	}
	gotStreams, err := streamRepo.ListByItem(context.Background(), "item-ing")
	if err != nil || len(gotStreams) != 2 {
		t.Fatalf("streams: got %d err %v", len(gotStreams), err)
	}
	gotChapters, err := chapterRepo.ListByItem(context.Background(), "item-ing")
	if err != nil || len(gotChapters) != 1 {
		t.Fatalf("chapters: got %d err %v", len(gotChapters), err)
	}
}

func TestItemRepository_IngestItem_NoChildren(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(testutil.Driver(), database)
	itemRepo := db.NewItemRepository(testutil.Driver(), database)
	seedLibraryForItems(t, libRepo)

	if err := itemRepo.IngestItem(context.Background(), newTestItem("bare", "lib-1", "Bare"), nil, nil); err != nil {
		t.Fatalf("IngestItem (no children): %v", err)
	}
	if _, err := itemRepo.GetByID(context.Background(), "bare"); err != nil {
		t.Fatalf("item not persisted: %v", err)
	}
}

// A failing child write (duplicate media-stream PK) must roll back the
// whole item — the atomicity guarantee that the per-table writes never
// gave (previously the item landed and the stream rows were dropped).
func TestItemRepository_IngestItem_RollsBackOnBadStream(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(testutil.Driver(), database)
	itemRepo := db.NewItemRepository(testutil.Driver(), database)
	seedLibraryForItems(t, libRepo)

	item := newTestItem("item-bad", "lib-1", "Bad")
	dupStreams := []*librarymodel.MediaStream{
		{ItemID: "item-bad", StreamIndex: 0, StreamType: "video", Codec: "h264"},
		{ItemID: "item-bad", StreamIndex: 0, StreamType: "audio", Codec: "aac"}, // duplicate PK
	}

	if err := itemRepo.IngestItem(context.Background(), item, dupStreams, nil); err == nil {
		t.Fatal("expected IngestItem to fail on duplicate stream index")
	}

	_, err := itemRepo.GetByID(context.Background(), "item-bad")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("item must be rolled back (not found), got err=%v", err)
	}
}
