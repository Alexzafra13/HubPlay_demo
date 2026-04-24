package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

// TestLibraryEPGSources_DuplicateURLReturnsSentinel pins down the
// UNIQUE-constraint detection: a second Create for the same URL must
// return ErrEPGSourceAlreadyAttached so the handler can map it to a
// clean 409 instead of surfacing the raw SQLite error.
func TestLibraryEPGSources_DuplicateURLReturnsSentinel(t *testing.T) {
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(database)
	ctx := context.Background()

	now := time.Now()
	libID := "lib-dup"
	if err := repos.Libraries.Create(ctx, &db.Library{
		ID: libID, Name: "D", ContentType: "livetv", ScanMode: "manual",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	first := &db.LibraryEPGSource{
		ID: "src-a", LibraryID: libID, URL: "https://example/epg.xml",
	}
	if err := repos.LibraryEPGSources.Create(ctx, first); err != nil {
		t.Fatalf("first create: %v", err)
	}

	second := &db.LibraryEPGSource{
		ID: "src-b", LibraryID: libID, URL: "https://example/epg.xml",
	}
	err := repos.LibraryEPGSources.Create(ctx, second)
	if !errors.Is(err, db.ErrEPGSourceAlreadyAttached) {
		t.Fatalf("got %v, want ErrEPGSourceAlreadyAttached", err)
	}
}
