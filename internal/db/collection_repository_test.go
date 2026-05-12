package db_test

import (
	"context"
	"testing"

	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

func TestCollectionRepository_EnsureAndList(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository("sqlite", database)
	itemRepo := db.NewItemRepository(database)
	metaRepo := db.NewMetadataRepository(database)
	colRepo := db.NewCollectionRepository(database)
	seedLibraryForItems(t, libRepo)

	// Three movies in the X-Men collection (TMDb 748), one in Toy
	// Story (TMDb 10194). Each gets its own metadata row first so
	// the FK from metadata.collection_id has a target to point at.
	mcuMovies := []string{"item-xmen-1", "item-xmen-2", "item-xmen-3"}
	for _, id := range mcuMovies {
		if err := itemRepo.Create(context.Background(), newTestItem(id, "lib-1", id)); err != nil {
			t.Fatal(err)
		}
		if err := metaRepo.Upsert(context.Background(), &db.Metadata{ItemID: id}); err != nil {
			t.Fatal(err)
		}
	}
	if err := itemRepo.Create(context.Background(), newTestItem("item-toystory", "lib-1", "ts")); err != nil {
		t.Fatal(err)
	}
	if err := metaRepo.Upsert(context.Background(), &db.Metadata{ItemID: "item-toystory"}); err != nil {
		t.Fatal(err)
	}

	xmenID, err := colRepo.EnsureCollection(context.Background(), 748, "X-Men Collection",
		"Mutants band together.", "https://image.tmdb.org/p.png", "https://image.tmdb.org/b.png")
	if err != nil {
		t.Fatalf("ensure xmen: %v", err)
	}
	if xmenID == "" {
		t.Fatal("expected non-empty collection id")
	}
	if xmenID != db.CollectionID(748) {
		t.Errorf("expected canonical id %q, got %q", db.CollectionID(748), xmenID)
	}
	for _, id := range mcuMovies {
		if err := colRepo.SetItemCollection(context.Background(), id, xmenID); err != nil {
			t.Fatalf("link %s: %v", id, err)
		}
	}

	tsID, err := colRepo.EnsureCollection(context.Background(), 10194, "Toy Story Collection", "", "", "")
	if err != nil {
		t.Fatalf("ensure ts: %v", err)
	}
	if err := colRepo.SetItemCollection(context.Background(), "item-toystory", tsID); err != nil {
		t.Fatal(err)
	}

	// Empty name → ("", nil): scanner uses this to keep the link NULL
	// for movies without a TMDb collection match.
	got, err := colRepo.EnsureCollection(context.Background(), 0, "", "", "", "")
	if err != nil {
		t.Errorf("ensure no-match returned error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty id for no-match, got %q", got)
	}

	// Idempotent: re-ensuring the same tmdb_id with a richer payload
	// keeps the existing row but refreshes overview / artwork (the
	// CASE clauses on the upsert preserve non-empty values rather
	// than overwriting with empty strings).
	repeated, err := colRepo.EnsureCollection(context.Background(), 748, "X-Men Collection",
		"Updated overview.", "https://image.tmdb.org/p2.png", "https://image.tmdb.org/b2.png")
	if err != nil {
		t.Fatalf("re-ensure: %v", err)
	}
	if repeated != xmenID {
		t.Errorf("expected stable id, got %q vs %q", repeated, xmenID)
	}
	xmen, err := colRepo.GetByID(context.Background(), xmenID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if xmen == nil {
		t.Fatal("expected x-men collection to exist")
	}
	if xmen.Overview != "Updated overview." {
		t.Errorf("expected overview to refresh, got %q", xmen.Overview)
	}

	// List: both collections surface, ordered by member count desc.
	list, err := colRepo.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 collections, got %d", len(list))
	}
	if list[0].Name != "X-Men Collection" || list[0].ItemCount != 3 {
		t.Errorf("expected X-Men first with 3 items, got %+v", list[0])
	}
	if list[1].Name != "Toy Story Collection" || list[1].ItemCount != 1 {
		t.Errorf("expected Toy Story second with 1 item, got %+v", list[1])
	}

	// Items grid for the saga page.
	items, err := colRepo.ListItemsForCollection(context.Background(), xmenID)
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 movies in x-men, got %d", len(items))
	}

	// Missing id → (nil, nil) so the handler returns 404 cleanly.
	missing, err := colRepo.GetByID(context.Background(), "collection:99999999")
	if err != nil {
		t.Errorf("missing id returned error: %v", err)
	}
	if missing != nil {
		t.Errorf("expected nil for missing id, got %+v", missing)
	}
}
