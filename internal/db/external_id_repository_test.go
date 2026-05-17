package db_test

import (
	"context"
	"testing"

	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

// TestExternalIDRepository_GetItemIDByExternalID_RoundTrip is a
// regression guard for a sqlc parser bug: v1.31.1 truncates the
// trailing identifier of the final query in a file, which silently
// corrupted `LIMIT 1` into `LIMIT` and broke the recommendations
// rail (every cross-reference returned an error â†’ every suggestion
// rendered as a TMDb-only badge even when the user already had it).
//
// The repo now sidesteps sqlc with raw SQL, so this test pins the
// contract end-to-end: an Upsert + GetItemIDByExternalID must round-trip
// the item id, and a missing pair must return ("", nil) rather than
// an error.
func TestExternalIDRepository_GetItemIDByExternalID_RoundTrip(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(testutil.Driver(), database)
	itemRepo := db.NewItemRepository(testutil.Driver(), database)
	extRepo := db.NewExternalIDRepository(testutil.Driver(), database)
	seedLibraryForItems(t, libRepo)

	item := newTestItem("item-rec", "lib-1", "Black Panther")
	if err := itemRepo.Create(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	if err := extRepo.Upsert(context.Background(), &librarymodel.ExternalID{
		ItemID:     "item-rec",
		Provider:   "tmdb",
		ExternalID: "284054",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	gotID, err := extRepo.GetItemIDByExternalID(context.Background(), "tmdb", "284054")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if gotID != "item-rec" {
		t.Errorf("expected item-rec, got %q", gotID)
	}

	// Missing pair must NOT error â€” the recommendations handler treats
	// any error as "lookup failed" and falls through to "external"
	// rendering, so the absent-row signal must come back as ("", nil).
	missingID, err := extRepo.GetItemIDByExternalID(context.Background(), "tmdb", "999999")
	if err != nil {
		t.Errorf("missing pair returned error: %v", err)
	}
	if missingID != "" {
		t.Errorf("expected empty id for missing pair, got %q", missingID)
	}
}
