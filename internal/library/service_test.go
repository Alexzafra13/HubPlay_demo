package library_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

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
	repos := db.NewRepositories(database)
	bus := event.NewBus(slog.Default())
	prober := &mockProber{}
	scnr := scanner.New(repos.Items, repos.MediaStreams, repos.Metadata, repos.ExternalIDs, repos.Images, repos.Chapters, nil, prober, bus, "", nil, slog.Default())
	svc := library.NewService(repos.Libraries, repos.Items, repos.MediaStreams, repos.Images, repos.Channels, scnr, slog.Default())
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

// TestService_DedupeSeasonsByChildCount pins the dedupe contract:
// for every (parent_id, season_number) group with > 1 row, keep the
// one with the most direct children; non-season rows pass through;
// non-duplicates are returned unchanged. The dedupe used to live in
// the Children handler — now it's owned by the items domain.
func TestService_DedupeSeasonsByChildCount(t *testing.T) {
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(database)
	bus := event.NewBus(slog.Default())
	scnr := scanner.New(repos.Items, repos.MediaStreams, repos.Metadata,
		repos.ExternalIDs, repos.Images, repos.Chapters, nil,
		&mockProber{}, bus, "", nil, slog.Default())
	svc := library.NewService(repos.Libraries, repos.Items, repos.MediaStreams,
		repos.Images, repos.Channels, scnr, slog.Default())
	t.Cleanup(svc.Shutdown)
	ctx := context.Background()

	lib, err := svc.Create(ctx, library.CreateRequest{
		Name: "Shows", ContentType: "shows", Paths: []string{"/tv"},
	})
	if err != nil {
		t.Fatalf("create lib: %v", err)
	}

	insert := func(item *db.Item) *db.Item {
		t.Helper()
		if err := repos.Items.Create(ctx, item); err != nil {
			t.Fatalf("insert %s: %v", item.ID, err)
		}
		return item
	}

	one := 1
	series := insert(&db.Item{ID: "series-1", LibraryID: lib.ID, Type: "series", Title: "Show"})
	loser := insert(&db.Item{
		ID: "season-loser", LibraryID: lib.ID, ParentID: series.ID,
		Type: "season", Title: "Season 1", SeasonNumber: &one,
	})
	winner := insert(&db.Item{
		ID: "season-winner", LibraryID: lib.ID, ParentID: series.ID,
		Type: "season", Title: "Season 1", SeasonNumber: &one,
	})
	soloSeries := insert(&db.Item{ID: "series-2", LibraryID: lib.ID, Type: "series", Title: "Other"})
	soloSeason := insert(&db.Item{
		ID: "season-solo", LibraryID: lib.ID, ParentID: soloSeries.ID,
		Type: "season", Title: "Season 1", SeasonNumber: &one,
	})
	movie := insert(&db.Item{ID: "movie-1", LibraryID: lib.ID, Type: "movie", Title: "Film"})

	// 3 episodes under winner, 1 under loser.
	for i := 0; i < 3; i++ {
		ep := i + 1
		insert(&db.Item{
			ID: "ep-w-" + string(rune('a'+i)), LibraryID: lib.ID,
			ParentID: winner.ID, Type: "episode", Title: "ep",
			SeasonNumber: &one, EpisodeNumber: &ep,
		})
	}
	insert(&db.Item{
		ID: "ep-l-1", LibraryID: lib.ID, ParentID: loser.ID,
		Type: "episode", Title: "ep", SeasonNumber: &one, EpisodeNumber: &one,
	})

	in := []*db.Item{loser, winner, soloSeason, movie}
	out := svc.DedupeSeasonsByChildCount(ctx, in)

	if len(out) != 3 {
		t.Fatalf("expected 3 rows after dedupe (winner + soloSeason + movie), got %d", len(out))
	}
	for _, item := range out {
		if item.ID == loser.ID {
			t.Errorf("loser season-loser was kept; expected to be dropped")
		}
	}
	// Non-duplicate path returns the same slice (identity is fine).
	none := []*db.Item{soloSeason, movie}
	out2 := svc.DedupeSeasonsByChildCount(ctx, none)
	if len(out2) != 2 {
		t.Errorf("non-duplicate input lost rows: got %d, want 2", len(out2))
	}
}
