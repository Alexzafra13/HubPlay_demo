package db_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

// TestSlugify pins the recipe used by both the migration's SQL
// backfill and the scanner's Go ensure-or-create. The two paths
// MUST agree: a row inserted by SQL and a row inserted by Go for
// the same studio name need to collide on the same slug, otherwise
// a studio appears twice in /studios with split item counts.
func TestSlugify(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"basic", "Marvel Studios", "marvel-studios"},
		{"trim", "  Lucasfilm Ltd. ", "lucasfilm-ltd"},
		{"ampersand", "Tom & Jerry Productions", "tom-and-jerry-productions"},
		{"apostrophe", "Disney's", "disneys"},
		{"empty", "", ""},
		{"whitespace_only", "   ", ""},
		{"collapses_runs", "A  B   C", "a-b-c"},
		{"non_alnum_collapsed", "20th Century Fox", "20th-century-fox"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := db.Slugify(tc.in)
			if got != tc.want {
				t.Errorf("Slugify(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestStudioRepository_EnsureAndList(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(testutil.Driver(), database)
	itemRepo := db.NewItemRepository(testutil.Driver(), database)
	metaRepo := db.NewMetadataRepository(testutil.Driver(), database)
	studioRepo := db.NewStudioRepository(testutil.Driver(), database)
	seedLibraryForItems(t, libRepo)

	// Two items linked to Marvel Studios (tmdb_id=420), one to a
	// network-only studio with no tmdb_id (legacy backfill path).
	for _, id := range []string{"item-mcu-1", "item-mcu-2"} {
		if err := itemRepo.Create(context.Background(), newTestItem(id, "lib-1", id)); err != nil {
			t.Fatal(err)
		}
		if err := metaRepo.Upsert(context.Background(), &librarymodel.Metadata{ItemID: id, Studio: "Marvel Studios"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := itemRepo.Create(context.Background(), newTestItem("item-old", "lib-1", "old")); err != nil {
		t.Fatal(err)
	}
	if err := metaRepo.Upsert(context.Background(), &librarymodel.Metadata{ItemID: "item-old", Studio: "Old librarymodel.Studio"}); err != nil {
		t.Fatal(err)
	}

	tmdbID := int64(420)
	mcuID, err := studioRepo.EnsureStudio(context.Background(), "Marvel Studios", "https://image.tmdb.org/x.png", &tmdbID)
	if err != nil {
		t.Fatalf("ensure mcu: %v", err)
	}
	if mcuID == "" {
		t.Fatal("expected non-empty studio id for marvel studios")
	}
	for _, id := range []string{"item-mcu-1", "item-mcu-2"} {
		if err := studioRepo.SetItemStudio(context.Background(), id, mcuID); err != nil {
			t.Fatalf("link %s: %v", id, err)
		}
	}

	oldID, err := studioRepo.EnsureStudio(context.Background(), "Old librarymodel.Studio", "", nil)
	if err != nil {
		t.Fatalf("ensure old: %v", err)
	}
	if err := studioRepo.SetItemStudio(context.Background(), "item-old", oldID); err != nil {
		t.Fatal(err)
	}

	// Idempotent: a second EnsureStudio with the same tmdb_id collapses
	// onto the existing row instead of creating a duplicate.
	repeated, err := studioRepo.EnsureStudio(context.Background(), "Marvel Studios", "https://image.tmdb.org/x.png", &tmdbID)
	if err != nil {
		t.Fatalf("re-ensure mcu: %v", err)
	}
	if repeated != mcuID {
		t.Errorf("expected stable id on re-ensure, got %q vs %q", repeated, mcuID)
	}

	// List: both studios surface, sorted by item count desc â€” Marvel
	// (2 items) before Old (1 item).
	list, err := studioRepo.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 studios, got %d", len(list))
	}
	if list[0].Name != "Marvel Studios" || list[0].ItemCount != 2 {
		t.Errorf("expected Marvel Studios first with 2 items, got %+v", list[0])
	}
	if list[1].Name != "Old librarymodel.Studio" || list[1].ItemCount != 1 {
		t.Errorf("expected Old librarymodel.Studio second with 1 item, got %+v", list[1])
	}

	// GetBySlug round-trip drives the /studios/{slug} page.
	got, err := studioRepo.GetBySlug(context.Background(), "marvel-studios")
	if err != nil {
		t.Fatalf("get by slug: %v", err)
	}
	if got == nil {
		t.Fatal("expected studio for slug 'marvel-studios'")
	}
	if got.TMDBID == nil || *got.TMDBID != tmdbID {
		t.Errorf("expected tmdb_id %d, got %v", tmdbID, got.TMDBID)
	}

	// Items grid for the studio's collection page.
	items, err := studioRepo.ListItemsForStudio(context.Background(), got.ID)
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items for marvel, got %d", len(items))
	}

	// Missing slug â†’ (nil, nil) so the handler can render 404.
	missing, err := studioRepo.GetBySlug(context.Background(), "does-not-exist")
	if err != nil {
		t.Errorf("missing slug returned error: %v", err)
	}
	if missing != nil {
		t.Errorf("expected nil for missing slug, got %+v", missing)
	}
}

// Regresión (estudio "Lucasfilm Ltd." con 0 títulos): la migración 032
// creó filas por slug sin tmdb_id. Al refrescar metadatos se borra la fila
// `metadata` (y su studio_id) y el scanner llama a EnsureStudio CON tmdb_id;
// el INSERT chocaba por id/slug (no por tmdb_id), EnsureStudio fallaba y el
// item se quedaba sin enlazar: la ficha seguía enlazando a /studios/<slug>
// pero la página salía vacía. Ahora adopta la fila legacy.
func TestStudioRepository_EnsureStudio_AdoptsLegacySlugRow(t *testing.T) {
	ctx := context.Background()
	database := testutil.NewTestDB(t)
	studioRepo := db.NewStudioRepository(testutil.Driver(), database)

	legacyID, err := studioRepo.EnsureStudio(ctx, "Lucasfilm Ltd.", "", nil)
	if err != nil {
		t.Fatalf("legacy ensure: %v", err)
	}

	tmdbID := int64(1)
	gotID, err := studioRepo.EnsureStudio(ctx, "Lucasfilm Ltd.", "https://image.tmdb.org/lucas.png", &tmdbID)
	if err != nil {
		t.Fatalf("ensure with tmdb_id over a legacy row must not fail: %v", err)
	}
	if gotID != legacyID {
		t.Errorf("expected to adopt legacy row %q, got %q", legacyID, gotID)
	}
	row, err := studioRepo.GetBySlug(ctx, "lucasfilm-ltd")
	if err != nil || row == nil {
		t.Fatalf("get by slug: %v %v", row, err)
	}
	if row.TMDBID == nil || *row.TMDBID != tmdbID {
		t.Errorf("legacy row should now carry tmdb_id %d, got %v", tmdbID, row.TMDBID)
	}
	if row.LogoURL != "https://image.tmdb.org/lucas.png" {
		t.Errorf("logo not refreshed: %q", row.LogoURL)
	}

	// Mismo nombre, otra compañía de TMDb: fila propia con slug
	// desambiguado en vez de error (y sin pisar la primera).
	other := int64(99)
	otherID, err := studioRepo.EnsureStudio(ctx, "Lucasfilm Ltd.", "", &other)
	if err != nil {
		t.Fatalf("ensure homonym studio: %v", err)
	}
	if otherID == legacyID {
		t.Fatalf("homonym with a different tmdb_id must not collapse onto %q", legacyID)
	}
	if s, _ := studioRepo.GetBySlug(ctx, "lucasfilm-ltd-99"); s == nil || s.ID != otherID {
		t.Errorf("expected disambiguated slug lucasfilm-ltd-99 for %q, got %+v", otherID, s)
	}

	// Y una tercera llamada con el tmdb_id ya conocido vuelve a la misma fila.
	again, err := studioRepo.EnsureStudio(ctx, "Lucasfilm", "", &tmdbID)
	if err != nil || again != legacyID {
		t.Errorf("known tmdb_id must resolve to %q, got %q (%v)", legacyID, again, err)
	}
}

// La migración 059 repara los items que el fallo anterior dejó sueltos:
// metadata con nombre de estudio y studio_id NULL vuelve a apuntar a la
// fila con ese slug.
func TestMigration059_RelinksUnlinkedStudios(t *testing.T) {
	ctx := context.Background()
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(testutil.Driver(), database)
	itemRepo := db.NewItemRepository(testutil.Driver(), database)
	metaRepo := db.NewMetadataRepository(testutil.Driver(), database)
	studioRepo := db.NewStudioRepository(testutil.Driver(), database)
	seedLibraryForItems(t, libRepo)

	if err := itemRepo.Create(ctx, newTestItem("item-sw", "lib-1", "Star Wars")); err != nil {
		t.Fatal(err)
	}
	if err := metaRepo.Upsert(ctx, &librarymodel.Metadata{ItemID: "item-sw", Studio: "Lucasfilm Ltd."}); err != nil {
		t.Fatal(err)
	}
	studioID, err := studioRepo.EnsureStudio(ctx, "Lucasfilm Ltd.", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Sin SetItemStudio: el item está suelto, como lo dejaba el fallo.

	dir := "migrations/sqlite"
	if testutil.Driver() == db.DriverPostgres {
		dir = "migrations/postgres"
	}
	sqlText, err := os.ReadFile(filepath.Join("..", "..", dir, "059_relink_unlinked_studios.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, string(sqlText)); err != nil {
		t.Fatalf("apply 059: %v", err)
	}

	items, err := studioRepo.ListItemsForStudio(ctx, studioID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "item-sw" {
		t.Errorf("expected item-sw relinked to %s, got %+v", studioID, items)
	}
}

// El chip de la ficha usa el slug de la fila ENLAZADA, no el del nombre:
// "Lucasfilm" y "Lucasfilm Ltd." comparten tmdb_id y una sola fila.
func TestStudioRepository_SlugForItem(t *testing.T) {
	ctx := context.Background()
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(testutil.Driver(), database)
	itemRepo := db.NewItemRepository(testutil.Driver(), database)
	metaRepo := db.NewMetadataRepository(testutil.Driver(), database)
	studioRepo := db.NewStudioRepository(testutil.Driver(), database)
	seedLibraryForItems(t, libRepo)

	for _, id := range []string{"item-a", "item-b"} {
		if err := itemRepo.Create(ctx, newTestItem(id, "lib-1", id)); err != nil {
			t.Fatal(err)
		}
	}
	if err := metaRepo.Upsert(ctx, &librarymodel.Metadata{ItemID: "item-a", Studio: "Lucasfilm"}); err != nil {
		t.Fatal(err)
	}
	if err := metaRepo.Upsert(ctx, &librarymodel.Metadata{ItemID: "item-b", Studio: "Sin enlazar"}); err != nil {
		t.Fatal(err)
	}
	tmdbID := int64(1)
	ltd, err := studioRepo.EnsureStudio(ctx, "Lucasfilm Ltd.", "", &tmdbID)
	if err != nil {
		t.Fatal(err)
	}
	if err := studioRepo.SetItemStudio(ctx, "item-a", ltd); err != nil {
		t.Fatal(err)
	}

	if slug, err := studioRepo.SlugForItem(ctx, "item-a"); err != nil || slug != "lucasfilm-ltd" {
		t.Errorf("SlugForItem(item-a) = %q, %v; want lucasfilm-ltd", slug, err)
	}
	if slug, err := studioRepo.SlugForItem(ctx, "item-b"); err != nil || slug != "" {
		t.Errorf("unlinked item must give empty slug, got %q, %v", slug, err)
	}
}
