package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/domain"
	"hubplay/internal/testutil"
)

func seedLibraryForItems(t *testing.T, repo *db.LibraryRepository) {
	t.Helper()
	now := time.Now()
	if err := repo.Create(context.Background(), &db.Library{
		ID: "lib-1", Name: "Movies", ContentType: "movies", ScanMode: "auto",
		ScanInterval: "6h", CreatedAt: now, UpdatedAt: now, Paths: []string{"/media"},
	}); err != nil {
		t.Fatal(err)
	}
}

func newTestItem(id, libraryID, title string) *db.Item {
	now := time.Now()
	return &db.Item{
		ID:          id,
		LibraryID:   libraryID,
		Type:        "movie",
		Title:       title,
		SortTitle:   title,
		Path:        "/media/" + id + ".mkv",
		AddedAt:     now,
		UpdatedAt:   now,
		IsAvailable: true,
	}
}

func TestItemRepository_Create_And_GetByID(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	repo := db.NewItemRepository(database)
	seedLibraryForItems(t, libRepo)

	item := newTestItem("item-1", "lib-1", "The Matrix")
	item.Year = 1999
	item.DurationTicks = 81360000000
	item.Container = "matroska,webm"

	if err := repo.Create(context.Background(), item); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := repo.GetByID(context.Background(), "item-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Title != "The Matrix" {
		t.Errorf("expected 'The Matrix', got %q", got.Title)
	}
	if got.Year != 1999 {
		t.Errorf("expected year 1999, got %d", got.Year)
	}
	if got.DurationTicks != 81360000000 {
		t.Errorf("expected duration 81360000000, got %d", got.DurationTicks)
	}
}

func TestItemRepository_GetByID_NotFound(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewItemRepository(database)

	_, err := repo.GetByID(context.Background(), "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestItemRepository_GetByPath(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	repo := db.NewItemRepository(database)
	seedLibraryForItems(t, libRepo)

	item := newTestItem("item-p", "lib-1", "PathTest")
	if err := repo.Create(context.Background(), item); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetByPath(context.Background(), item.Path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "item-p" {
		t.Errorf("expected ID 'item-p', got %q", got.ID)
	}
}

func TestItemRepository_List(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	repo := db.NewItemRepository(database)
	seedLibraryForItems(t, libRepo)

	if err := repo.Create(context.Background(), newTestItem("m1", "lib-1", "Alpha")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(context.Background(), newTestItem("m2", "lib-1", "Beta")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(context.Background(), newTestItem("m3", "lib-1", "Gamma")); err != nil {
		t.Fatal(err)
	}

	items, total, err := repo.List(context.Background(), db.ItemFilter{LibraryID: "lib-1", Limit: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 3 {
		t.Errorf("expected total 3, got %d", total)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}
	// Sorted by sort_title ASC by default
	if items[0].Title != "Alpha" {
		t.Errorf("expected first item 'Alpha', got %q", items[0].Title)
	}
}

func TestItemRepository_List_GenreYearRatingFilters(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	repo := db.NewItemRepository(database)
	values := db.NewItemValueRepository(database)
	seedLibraryForItems(t, libRepo)

	// Three movies with different (genre, year, rating) so each filter
	// can be exercised in isolation without leaking into the others.
	rating := func(v float64) *float64 { return &v }

	a := newTestItem("a", "lib-1", "Alpha")
	a.Year = 2010
	a.CommunityRating = rating(8.5)
	if err := repo.Create(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	b := newTestItem("b", "lib-1", "Beta")
	b.Year = 2005
	b.CommunityRating = rating(7.0)
	if err := repo.Create(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	c := newTestItem("c", "lib-1", "Gamma")
	c.Year = 2020
	c.CommunityRating = rating(6.0)
	if err := repo.Create(context.Background(), c); err != nil {
		t.Fatal(err)
	}

	if err := values.SetGenres(context.Background(), "a", []string{"Action", "Drama"}); err != nil {
		t.Fatal(err)
	}
	if err := values.SetGenres(context.Background(), "b", []string{"Drama"}); err != nil {
		t.Fatal(err)
	}
	if err := values.SetGenres(context.Background(), "c", []string{"Comedy"}); err != nil {
		t.Fatal(err)
	}

	t.Run("genre filters case-insensitively", func(t *testing.T) {
		items, total, err := repo.List(context.Background(), db.ItemFilter{Genre: "drama"})
		if err != nil {
			t.Fatal(err)
		}
		if total != 2 || len(items) != 2 {
			t.Fatalf("expected 2 Drama items, got total=%d len=%d", total, len(items))
		}
	})

	t.Run("year_from excludes older items", func(t *testing.T) {
		_, total, err := repo.List(context.Background(), db.ItemFilter{YearFrom: 2010})
		if err != nil {
			t.Fatal(err)
		}
		if total != 2 {
			t.Errorf("expected 2 items >= 2010, got %d", total)
		}
	})

	t.Run("year_to excludes newer items", func(t *testing.T) {
		_, total, err := repo.List(context.Background(), db.ItemFilter{YearTo: 2010})
		if err != nil {
			t.Fatal(err)
		}
		if total != 2 {
			t.Errorf("expected 2 items <= 2010, got %d", total)
		}
	})

	t.Run("min_rating excludes lower-rated items", func(t *testing.T) {
		_, total, err := repo.List(context.Background(), db.ItemFilter{MinRating: 7.5})
		if err != nil {
			t.Fatal(err)
		}
		if total != 1 {
			t.Errorf("expected 1 item >= 7.5, got %d", total)
		}
	})

	t.Run("filters compose with AND semantics", func(t *testing.T) {
		_, total, err := repo.List(context.Background(), db.ItemFilter{
			Genre:     "Drama",
			MinRating: 8.0,
		})
		if err != nil {
			t.Fatal(err)
		}
		if total != 1 {
			t.Errorf("expected only Alpha (Drama + 8.5), got %d", total)
		}
	})

	t.Run("SetGenres replaces previous tags", func(t *testing.T) {
		// "a" started as Action+Drama; replace with just Action and verify
		// Drama no longer matches it.
		if err := values.SetGenres(context.Background(), "a", []string{"Action"}); err != nil {
			t.Fatal(err)
		}
		_, dramaTotal, err := repo.List(context.Background(), db.ItemFilter{Genre: "Drama"})
		if err != nil {
			t.Fatal(err)
		}
		if dramaTotal != 1 {
			t.Errorf("after replace expected 1 Drama (just b), got %d", dramaTotal)
		}
	})
}

func TestMigration031_BackfillsGenresFromMetadata(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	itemRepo := db.NewItemRepository(database)
	metaRepo := db.NewMetadataRepository(database)
	values := db.NewItemValueRepository(database)
	seedLibraryForItems(t, libRepo)

	// Pre-existing item with metadata.genres_json present but no
	// item_values row — this simulates a library scanned before
	// migration 031 shipped. The migration's INSERT...SELECT needs to
	// pick those rows up so users get filtering immediately without
	// re-scanning their library.
	if err := itemRepo.Create(context.Background(), newTestItem("legacy", "lib-1", "Legacy")); err != nil {
		t.Fatal(err)
	}
	if err := metaRepo.Upsert(context.Background(), &db.Metadata{
		ItemID:     "legacy",
		GenresJSON: `["Action","Drama"]`,
	}); err != nil {
		t.Fatal(err)
	}

	// Re-run the backfill statement (idempotent under INSERT OR IGNORE).
	// Mirrors what the migration runs at upgrade time.
	const upsertValues = `
		INSERT OR IGNORE INTO item_values (id, type, value, clean_value)
		SELECT 'genre:' || LOWER(TRIM(je.value)), 'genre', TRIM(je.value), LOWER(TRIM(je.value))
		FROM metadata m, json_each(m.genres_json) je
		WHERE m.genres_json IS NOT NULL AND m.genres_json != '' AND json_valid(m.genres_json)
		      AND TRIM(je.value) != ''`
	const linkValues = `
		INSERT OR IGNORE INTO item_value_map (item_id, value_id)
		SELECT m.item_id, 'genre:' || LOWER(TRIM(je.value))
		FROM metadata m, json_each(m.genres_json) je
		WHERE m.genres_json IS NOT NULL AND m.genres_json != '' AND json_valid(m.genres_json)
		      AND TRIM(je.value) != ''`
	if _, err := database.Exec(upsertValues); err != nil {
		t.Fatalf("backfill values: %v", err)
	}
	if _, err := database.Exec(linkValues); err != nil {
		t.Fatalf("backfill map: %v", err)
	}

	// The repository's filter query should now find the legacy item by
	// genre — proving the backfill closes the gap.
	itemRepoForList := db.NewItemRepository(database)
	_, total, err := itemRepoForList.List(context.Background(), db.ItemFilter{Genre: "Action"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Errorf("expected 1 backfilled match for Action, got %d", total)
	}

	// Genre vocabulary listing should also reflect the backfill.
	g, err := values.ListGenres(context.Background(), "movie")
	if err != nil {
		t.Fatal(err)
	}
	if len(g) != 2 {
		t.Errorf("expected backfill to surface 2 genres (Action, Drama), got %d", len(g))
	}
}

func TestItemValueRepository_ListGenres(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	itemRepo := db.NewItemRepository(database)
	values := db.NewItemValueRepository(database)
	seedLibraryForItems(t, libRepo)

	// Two movies with overlapping genres + one series with its own genre,
	// so the type scope (movie vs series) demonstrably narrows the
	// returned vocabulary.
	movieA := newTestItem("ma", "lib-1", "MA")
	if err := itemRepo.Create(context.Background(), movieA); err != nil {
		t.Fatal(err)
	}
	movieB := newTestItem("mb", "lib-1", "MB")
	if err := itemRepo.Create(context.Background(), movieB); err != nil {
		t.Fatal(err)
	}
	series := newTestItem("ss", "lib-1", "SS")
	series.Type = "series"
	series.Path = "/media/ss"
	if err := itemRepo.Create(context.Background(), series); err != nil {
		t.Fatal(err)
	}

	if err := values.SetGenres(context.Background(), "ma", []string{"Action", "Drama"}); err != nil {
		t.Fatal(err)
	}
	if err := values.SetGenres(context.Background(), "mb", []string{"Drama"}); err != nil {
		t.Fatal(err)
	}
	if err := values.SetGenres(context.Background(), "ss", []string{"Reality"}); err != nil {
		t.Fatal(err)
	}

	moviesGenres, err := values.ListGenres(context.Background(), "movie")
	if err != nil {
		t.Fatal(err)
	}
	if len(moviesGenres) != 2 {
		t.Fatalf("expected 2 movie genres (Action, Drama), got %d", len(moviesGenres))
	}
	// Drama appears in 2 movies, Action in 1 → Drama first.
	if moviesGenres[0].Name != "Drama" || moviesGenres[0].Count != 2 {
		t.Errorf("expected Drama (2) first, got %+v", moviesGenres[0])
	}

	seriesGenres, err := values.ListGenres(context.Background(), "series")
	if err != nil {
		t.Fatal(err)
	}
	if len(seriesGenres) != 1 || seriesGenres[0].Name != "Reality" {
		t.Errorf("expected only Reality for series, got %+v", seriesGenres)
	}

	all, err := values.ListGenres(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 genres union (Action, Drama, Reality), got %d", len(all))
	}
}

func TestItemRepository_List_ByType(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	repo := db.NewItemRepository(database)
	seedLibraryForItems(t, libRepo)

	movie := newTestItem("m1", "lib-1", "Movie1")
	movie.Type = "movie"
	if err := repo.Create(context.Background(), movie); err != nil {
		t.Fatal(err)
	}

	series := newTestItem("s1", "lib-1", "Series1")
	series.Type = "series"
	series.Path = "/media/s1"
	if err := repo.Create(context.Background(), series); err != nil {
		t.Fatal(err)
	}

	items, total, err := repo.List(context.Background(), db.ItemFilter{Type: "movie"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 {
		t.Errorf("expected 1 movie, got %d", total)
	}
	if len(items) != 1 || items[0].Type != "movie" {
		t.Error("expected only movies")
	}
}

func TestItemRepository_Hierarchy(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	repo := db.NewItemRepository(database)
	seedLibraryForItems(t, libRepo)

	// Series → Season → Episodes
	series := &db.Item{
		ID: "series-1", LibraryID: "lib-1", Type: "series", Title: "Breaking Bad",
		SortTitle: "breaking bad", AddedAt: time.Now(), UpdatedAt: time.Now(), IsAvailable: true,
	}
	if err := repo.Create(context.Background(), series); err != nil {
		t.Fatal(err)
	}

	season := &db.Item{
		ID: "season-1", LibraryID: "lib-1", ParentID: "series-1", Type: "season",
		Title: "Season 1", SortTitle: "season 1",
		AddedAt: time.Now(), UpdatedAt: time.Now(), IsAvailable: true,
	}
	sn := 1
	season.SeasonNumber = &sn
	if err := repo.Create(context.Background(), season); err != nil {
		t.Fatal(err)
	}

	ep1 := &db.Item{
		ID: "ep-1", LibraryID: "lib-1", ParentID: "season-1", Type: "episode",
		Title: "Pilot", SortTitle: "pilot", Path: "/media/bb/s01e01.mkv",
		AddedAt: time.Now(), UpdatedAt: time.Now(), IsAvailable: true,
	}
	en := 1
	ep1.EpisodeNumber = &en
	if err := repo.Create(context.Background(), ep1); err != nil {
		t.Fatal(err)
	}

	ep2 := &db.Item{
		ID: "ep-2", LibraryID: "lib-1", ParentID: "season-1", Type: "episode",
		Title: "Cat's in the Bag", SortTitle: "cat's in the bag", Path: "/media/bb/s01e02.mkv",
		AddedAt: time.Now(), UpdatedAt: time.Now(), IsAvailable: true,
	}
	en2 := 2
	ep2.EpisodeNumber = &en2
	if err := repo.Create(context.Background(), ep2); err != nil {
		t.Fatal(err)
	}

	// Get children of season
	children, err := repo.GetChildren(context.Background(), "season-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("expected 2 episodes, got %d", len(children))
	}
	// Sorted by episode number
	if children[0].Title != "Pilot" {
		t.Errorf("expected first episode 'Pilot', got %q", children[0].Title)
	}
}

func TestItemRepository_Update(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	repo := db.NewItemRepository(database)
	seedLibraryForItems(t, libRepo)

	item := newTestItem("item-upd", "lib-1", "Old Title")
	if err := repo.Create(context.Background(), item); err != nil {
		t.Fatal(err)
	}

	item.Title = "New Title"
	item.SortTitle = "new title"
	item.UpdatedAt = time.Now()

	if err := repo.Update(context.Background(), item); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := repo.GetByID(context.Background(), "item-upd")
	if got.Title != "New Title" {
		t.Errorf("expected 'New Title', got %q", got.Title)
	}
}

func TestItemRepository_Delete(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	repo := db.NewItemRepository(database)
	seedLibraryForItems(t, libRepo)

	item := newTestItem("item-del", "lib-1", "Delete Me")
	if err := repo.Create(context.Background(), item); err != nil {
		t.Fatal(err)
	}

	if err := repo.Delete(context.Background(), "item-del"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := repo.GetByID(context.Background(), "item-del")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Error("item should be deleted")
	}
}

func TestItemRepository_CountByLibrary(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	repo := db.NewItemRepository(database)
	seedLibraryForItems(t, libRepo)

	if err := repo.Create(context.Background(), newTestItem("c1", "lib-1", "A")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(context.Background(), newTestItem("c2", "lib-1", "B")); err != nil {
		t.Fatal(err)
	}

	count, err := repo.CountByLibrary(context.Background(), "lib-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2, got %d", count)
	}
}

func TestItemRepository_List_FTSSearch(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	repo := db.NewItemRepository(database)
	seedLibraryForItems(t, libRepo)

	if err := repo.Create(context.Background(), newTestItem("m1", "lib-1", "The Matrix")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(context.Background(), newTestItem("m2", "lib-1", "Breaking Bad")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(context.Background(), newTestItem("m3", "lib-1", "The Matrix Reloaded")); err != nil {
		t.Fatal(err)
	}

	// Search for "matrix" should find 2 results
	items, total, err := repo.List(context.Background(), db.ItemFilter{Query: "matrix"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 2 {
		t.Errorf("expected 2 results for 'matrix', got %d", total)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}
}

func TestItemRepository_List_FTSSearch_NoResults(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	repo := db.NewItemRepository(database)
	seedLibraryForItems(t, libRepo)

	if err := repo.Create(context.Background(), newTestItem("m1", "lib-1", "The Matrix")); err != nil {
		t.Fatal(err)
	}

	items, total, err := repo.List(context.Background(), db.ItemFilter{Query: "nonexistent"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 0 {
		t.Errorf("expected 0 results, got %d", total)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

func TestItemRepository_List_FTSSearch_PrefixMatch(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	repo := db.NewItemRepository(database)
	seedLibraryForItems(t, libRepo)

	if err := repo.Create(context.Background(), newTestItem("m1", "lib-1", "Breaking Bad")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(context.Background(), newTestItem("m2", "lib-1", "Breakfast Club")); err != nil {
		t.Fatal(err)
	}

	// Prefix "break" should match both
	items, total, err := repo.List(context.Background(), db.ItemFilter{Query: "break"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 2 {
		t.Errorf("expected 2 results for prefix 'break', got %d", total)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}
}

func TestItemRepository_List_FTSSearch_AfterUpdate(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	repo := db.NewItemRepository(database)
	seedLibraryForItems(t, libRepo)

	item := newTestItem("m1", "lib-1", "Old Title")
	if err := repo.Create(context.Background(), item); err != nil {
		t.Fatal(err)
	}

	// Should find with old title
	items, _, _ := repo.List(context.Background(), db.ItemFilter{Query: "Old"})
	if len(items) != 1 {
		t.Fatalf("expected 1 result for 'Old', got %d", len(items))
	}

	// Update title
	item.Title = "New Title"
	item.SortTitle = "new title"
	item.UpdatedAt = time.Now()
	if err := repo.Update(context.Background(), item); err != nil {
		t.Fatal(err)
	}

	// Old title should not match
	items, _, _ = repo.List(context.Background(), db.ItemFilter{Query: "Old"})
	if len(items) != 0 {
		t.Errorf("expected 0 results for 'Old' after update, got %d", len(items))
	}

	// New title should match
	items, _, _ = repo.List(context.Background(), db.ItemFilter{Query: "New"})
	if len(items) != 1 {
		t.Errorf("expected 1 result for 'New' after update, got %d", len(items))
	}
}

func TestItemRepository_List_FTSSearch_AfterDelete(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	repo := db.NewItemRepository(database)
	seedLibraryForItems(t, libRepo)

	item := newTestItem("m1", "lib-1", "Delete Me")
	if err := repo.Create(context.Background(), item); err != nil {
		t.Fatal(err)
	}

	// Should find before delete
	items, _, _ := repo.List(context.Background(), db.ItemFilter{Query: "Delete"})
	if len(items) != 1 {
		t.Fatalf("expected 1 result before delete, got %d", len(items))
	}

	if err := repo.Delete(context.Background(), "m1"); err != nil {
		t.Fatal(err)
	}

	// Should not find after delete
	items, _, _ = repo.List(context.Background(), db.ItemFilter{Query: "Delete"})
	if len(items) != 0 {
		t.Errorf("expected 0 results after delete, got %d", len(items))
	}
}

func TestItemRepository_LatestItems(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	repo := db.NewItemRepository(database)
	seedLibraryForItems(t, libRepo)

	for i, title := range []string{"First", "Second", "Third"} {
		item := newTestItem(title, "lib-1", title)
		item.AddedAt = time.Now().Add(time.Duration(i) * time.Minute)
		if err := repo.Create(context.Background(), item); err != nil {
			t.Fatal(err)
		}
	}

	items, err := repo.LatestItems(context.Background(), "lib-1", "", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].Title != "Third" {
		t.Errorf("expected most recent first, got %q", items[0].Title)
	}
}
