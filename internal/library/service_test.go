package library_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/db"
	"hubplay/internal/domain"
	"hubplay/internal/event"
	"hubplay/internal/library"
	"hubplay/internal/probe"
	"hubplay/internal/scanner"
	"hubplay/internal/testutil"
)

type mockProber struct{}

func (m *mockProber) Probe(ctx context.Context, path string) (*probe.Result, error) {
	return &probe.Result{
		Format: probe.Format{Size: 1000, FormatName: "matroska,webm"},
	}, nil
}

func newTestLibraryService(t *testing.T) *library.Service {
	t.Helper()
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(testutil.Driver(), database)
	bus := event.NewBus(slog.Default())
	prober := &mockProber{}
	scnr := scanner.New(scanner.Config{
		Items: repos.Items, Streams: repos.MediaStreams, Metadata: repos.Metadata,
		ExternalIDs: repos.ExternalIDs, Images: repos.Images, Chapters: repos.Chapters,
		People: repos.People, ItemValues: repos.ItemValues, Studios: repos.Studios,
		Collections: repos.Collections, MetaLocks: repos.ItemMetadataLocks,
		Prober: prober, Bus: bus, Logger: slog.Default(),
	})
	svc := library.NewService(repos.Libraries, repos.Items, repos.MediaStreams, repos.Images, repos.Channels, repos.ItemValues, scnr, nil, slog.Default())
	// Cancel in-flight auto-scan goroutines BEFORE the DB teardown fires,
	// otherwise the goroutine races the "sql: database is closed" error and
	// the TempDir cleanup fails with "directory not empty". t.Cleanup runs
	// in LIFO order, so registering Shutdown here ensures it runs before
	// NewTestDB's own Close cleanup.
	t.Cleanup(svc.Shutdown)
	return svc
}

func TestService_Create(t *testing.T) {
	svc := newTestLibraryService(t)

	lib, err := svc.Create(context.Background(), library.CreateRequest{
		Name:        "Movies",
		ContentType: "movies",
		Paths:       []string{"/media/movies"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if lib.Name != "Movies" {
		t.Errorf("expected name 'Movies', got %q", lib.Name)
	}
	if lib.ID == "" {
		t.Error("expected ID to be generated")
	}
	if lib.ContentType != "movies" {
		t.Errorf("expected content_type 'movies', got %q", lib.ContentType)
	}
	if len(lib.Paths) != 1 {
		t.Errorf("expected 1 path, got %d", len(lib.Paths))
	}
}

func TestService_Create_Validation(t *testing.T) {
	svc := newTestLibraryService(t)

	tests := []struct {
		name string
		req  library.CreateRequest
	}{
		{"missing name", library.CreateRequest{ContentType: "movies", Paths: []string{"/a"}}},
		{"invalid type", library.CreateRequest{Name: "X", ContentType: "invalid", Paths: []string{"/a"}}},
		{"no paths", library.CreateRequest{Name: "X", ContentType: "movies"}},
		// livetv-specific URL validation. The shape check happens at
		// the create boundary so a typo in /admin/libraries or
		// /admin/users/{id}/iptv-libraries surfaces as an inline 400
		// instead of being deferred to the next M3U fetch attempt.
		{"livetv missing m3u", library.CreateRequest{Name: "X", ContentType: "livetv"}},
		{"livetv non-http m3u", library.CreateRequest{
			Name: "X", ContentType: "livetv", M3UURL: "file:///etc/passwd",
		}},
		{"livetv gopher m3u", library.CreateRequest{
			Name: "X", ContentType: "livetv", M3UURL: "gopher://example.com/list.m3u",
		}},
		{"livetv typo m3u (no scheme)", library.CreateRequest{
			Name: "X", ContentType: "livetv", M3UURL: "example.com/list.m3u",
		}},
		{"livetv https without host", library.CreateRequest{
			Name: "X", ContentType: "livetv", M3UURL: "https://",
		}},
		{"livetv bad epg URL", library.CreateRequest{
			Name: "X", ContentType: "livetv",
			M3UURL: "https://example.com/x.m3u", EPGURL: "not-a-url",
		}},
		{"livetv ftp epg URL", library.CreateRequest{
			Name: "X", ContentType: "livetv",
			M3UURL: "https://example.com/x.m3u", EPGURL: "ftp://example.com/epg.xml",
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Create(context.Background(), tt.req)
			if !errors.Is(err, domain.ErrValidation) {
				t.Errorf("expected ErrValidation, got %v", err)
			}
		})
	}
}

// TestService_CreatePersonalIPTV_URLValidation pins the URL-shape
// guard for the admin "personal IPTV list" shortcut so a bogus M3U
// (or EPG) never reaches the DB. Mirrors the generic Create cases
// because CreatePersonalIPTV reuses validateCreateRequest after
// forcing content_type=livetv.
func TestService_CreatePersonalIPTV_URLValidation(t *testing.T) {
	svc := newTestLibraryService(t)
	ctx := context.Background()

	t.Run("rejects file:// M3U", func(t *testing.T) {
		_, err := svc.CreatePersonalIPTV(ctx, "u-1", library.CreateRequest{
			Name: "X", M3UURL: "file:///etc/passwd",
		})
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("expected ErrValidation, got %v", err)
		}
	})
	t.Run("rejects bad EPG URL", func(t *testing.T) {
		_, err := svc.CreatePersonalIPTV(ctx, "u-1", library.CreateRequest{
			Name: "X", M3UURL: "https://example.com/x.m3u", EPGURL: "totally-bogus",
		})
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("expected ErrValidation, got %v", err)
		}
	})
}

// Update should reject the same bogus URLs as Create so an admin
// can't sneak a file:// M3U past the validator by editing an
// existing library.
func TestService_Update_URLValidation(t *testing.T) {
	svc := newTestLibraryService(t)
	ctx := context.Background()

	lib, err := svc.Create(ctx, library.CreateRequest{
		Name: "TV", ContentType: "livetv", M3UURL: "https://example.com/a.m3u",
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	bad := "file:///etc/passwd"
	if _, err := svc.Update(ctx, lib.ID, library.UpdateRequest{M3UURL: &bad}); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation on M3U file://, got %v", err)
	}

	badEPG := "javascript:alert(1)"
	if _, err := svc.Update(ctx, lib.ID, library.UpdateRequest{EPGURL: &badEPG}); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation on bogus EPG, got %v", err)
	}

	// Clearing both URLs (passing "") must remain legal — that's how
	// the admin "unset EPG" path works.
	empty := ""
	if _, err := svc.Update(ctx, lib.ID, library.UpdateRequest{EPGURL: &empty}); err != nil {
		t.Errorf("clearing EPG must be allowed, got %v", err)
	}
}

func TestService_GetByID(t *testing.T) {
	svc := newTestLibraryService(t)

	lib, _ := svc.Create(context.Background(), library.CreateRequest{
		Name: "Shows", ContentType: "shows", Paths: []string{"/media/shows"},
	})

	got, err := svc.GetByID(context.Background(), lib.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "Shows" {
		t.Errorf("expected 'Shows', got %q", got.Name)
	}
}

func TestService_GetByID_NotFound(t *testing.T) {
	svc := newTestLibraryService(t)

	_, err := svc.GetByID(context.Background(), "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestService_List(t *testing.T) {
	svc := newTestLibraryService(t)

	if _, err := svc.Create(context.Background(), library.CreateRequest{
		Name: "Movies", ContentType: "movies", Paths: []string{"/a"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(context.Background(), library.CreateRequest{
		Name: "Shows", ContentType: "shows", Paths: []string{"/b"},
	}); err != nil {
		t.Fatal(err)
	}

	libs, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(libs) != 2 {
		t.Errorf("expected 2 libraries, got %d", len(libs))
	}
}

func TestService_Update(t *testing.T) {
	svc := newTestLibraryService(t)

	lib, _ := svc.Create(context.Background(), library.CreateRequest{
		Name: "Movies", ContentType: "movies", Paths: []string{"/a"},
	})

	updated, err := svc.Update(context.Background(), lib.ID, library.UpdateRequest{
		Name: "Updated Movies",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Name != "Updated Movies" {
		t.Errorf("expected 'Updated Movies', got %q", updated.Name)
	}
}

func TestService_Delete(t *testing.T) {
	svc := newTestLibraryService(t)

	lib, _ := svc.Create(context.Background(), library.CreateRequest{
		Name: "Movies", ContentType: "movies", Paths: []string{"/a"},
	})

	if err := svc.Delete(context.Background(), lib.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := svc.GetByID(context.Background(), lib.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestService_Delete_NotFound(t *testing.T) {
	svc := newTestLibraryService(t)

	err := svc.Delete(context.Background(), "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestService_ItemCount(t *testing.T) {
	svc := newTestLibraryService(t)

	lib, _ := svc.Create(context.Background(), library.CreateRequest{
		Name: "Movies", ContentType: "movies", Paths: []string{"/a"},
	})

	count, err := svc.ItemCount(context.Background(), lib.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 items, got %d", count)
	}
}

// TestService_SeasonUniqueConstraintRejectsDuplicates pins the new
// structural guarantee from migration 018: two season rows with the
// same (parent_id, season_number) must be rejected at the DB layer.
// Replaces the previous TestService_DedupeSeasonsByChildCount which
// asserted the now-deleted runtime dedupe — once the constraint is
// in place, the dedupe code became dead defence and was removed.
func TestService_SeasonUniqueConstraintRejectsDuplicates(t *testing.T) {
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(testutil.Driver(), database)
	ctx := context.Background()

	now := time.Now()
	if err := repos.Libraries.Create(ctx, &librarymodel.Library{
		ID: "lib-shows", Name: "Shows", ContentType: "shows",
		ScanMode: "auto", ScanInterval: "6h",
		CreatedAt: now, UpdatedAt: now, Paths: []string{"/tv"},
	}); err != nil {
		t.Fatalf("create lib: %v", err)
	}

	one := 1
	series := &librarymodel.Item{
		ID: "series-1", LibraryID: "lib-shows", Type: "series",
		Title: "Show", SortTitle: "show", AddedAt: now, UpdatedAt: now,
		IsAvailable: true,
	}
	if err := repos.Items.Create(ctx, series); err != nil {
		t.Fatalf("insert series: %v", err)
	}

	first := &librarymodel.Item{
		ID: "season-1a", LibraryID: "lib-shows", ParentID: series.ID,
		Type: "season", Title: "Season 1", SortTitle: "season 1",
		SeasonNumber: &one, AddedAt: now, UpdatedAt: now, IsAvailable: true,
	}
	if err := repos.Items.Create(ctx, first); err != nil {
		t.Fatalf("first season insert: %v", err)
	}

	dup := &librarymodel.Item{
		ID: "season-1b", LibraryID: "lib-shows", ParentID: series.ID,
		Type: "season", Title: "Season 1", SortTitle: "season 1",
		SeasonNumber: &one, AddedAt: now, UpdatedAt: now, IsAvailable: true,
	}
	err := repos.Items.Create(ctx, dup)
	if err == nil {
		t.Fatal("inserting a duplicate (parent_id, season_number) should fail; the partial UNIQUE index from migration 018 was bypassed")
	}
	// SQLite reports "UNIQUE constraint failed"; Postgres reports
	// "duplicate key value violates unique constraint" (SQLSTATE 23505).
	// Both forms are accepted — the assertion is "the DB rejected the
	// dup", not "the message looks like SQLite's wording".
	msg := err.Error()
	if !strings.Contains(msg, "UNIQUE constraint failed") &&
		!strings.Contains(msg, "duplicate key value violates unique constraint") {
		t.Errorf("expected UNIQUE constraint error (sqlite or postgres form), got: %v", err)
	}
}
