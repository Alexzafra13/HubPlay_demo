package db_test

import (
	"context"
	"testing"

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
	libRepo := db.NewLibraryRepository("sqlite", database)
	itemRepo := db.NewItemRepository("sqlite", database)
	metaRepo := db.NewMetadataRepository("sqlite", database)
	studioRepo := db.NewStudioRepository("sqlite", database)
	seedLibraryForItems(t, libRepo)

	// Two items linked to Marvel Studios (tmdb_id=420), one to a
	// network-only studio with no tmdb_id (legacy backfill path).
	for _, id := range []string{"item-mcu-1", "item-mcu-2"} {
		if err := itemRepo.Create(context.Background(), newTestItem(id, "lib-1", id)); err != nil {
			t.Fatal(err)
		}
		if err := metaRepo.Upsert(context.Background(), &db.Metadata{ItemID: id, Studio: "Marvel Studios"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := itemRepo.Create(context.Background(), newTestItem("item-old", "lib-1", "old")); err != nil {
		t.Fatal(err)
	}
	if err := metaRepo.Upsert(context.Background(), &db.Metadata{ItemID: "item-old", Studio: "Old Studio"}); err != nil {
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

	oldID, err := studioRepo.EnsureStudio(context.Background(), "Old Studio", "", nil)
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
	if list[1].Name != "Old Studio" || list[1].ItemCount != 1 {
		t.Errorf("expected Old Studio second with 1 item, got %+v", list[1])
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
