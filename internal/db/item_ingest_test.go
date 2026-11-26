package db_test

import (
	"context"
	"fmt"
	"sync"
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

// TestItemRepository_IngestItem_ConcurrentWritersDontFail: dos bibliotecas
// escaneando a la vez (o un escaneo y una importación de canales) abren
// transacciones de escritura concurrentes. Con BEGIN DEFERRED, SQLite
// devolvía SQLITE_BUSY al instante sin esperar busy_timeout y el scanner
// se saltaba el fichero (visto en el E2E: "create item: database is
// locked"). Con _txlock=immediate esperan su turno. Sin el fix fallan
// varias inserciones en cada ejecución.
func TestItemRepository_IngestItem_ConcurrentWritersDontFail(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(testutil.Driver(), database)
	repo := db.NewItemRepository(testutil.Driver(), database)
	seedLibraryForItems(t, libRepo)

	const writers, perWriter = 4, 150
	var wg sync.WaitGroup
	errs := make(chan error, writers*perWriter)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				item := newTestItem(fmt.Sprintf("conc-%d-%d", w, i), "lib-1", fmt.Sprintf("Title %d %d", w, i))
				item.Path = fmt.Sprintf("/media/%d/%d.mkv", w, i)
				if err := repo.IngestItem(context.Background(), item, nil, nil); err != nil {
					errs <- err
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)

	failed := 0
	for err := range errs {
		if failed < 3 {
			t.Errorf("ingest concurrente: %v", err)
		}
		failed++
	}
	if failed > 0 {
		t.Fatalf("%d de %d inserciones fallaron", failed, writers*perWriter)
	}
}
