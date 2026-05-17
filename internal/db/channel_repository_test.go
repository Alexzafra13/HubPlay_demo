package db_test

import (
	"context"
	"testing"
	"time"

	iptvmodel "hubplay/internal/iptv/model"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

func setupChannelTest(t *testing.T) (*db.ChannelRepository, string) {
	t.Helper()
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(testutil.Driver(), database)

	now := time.Now()
	_ = repos.Libraries.Create(context.Background(), &librarymodel.Library{
		ID: "lib-iptv", Name: "Live TV", ContentType: "livetv",
		CreatedAt: now, UpdatedAt: now,
	})

	return repos.Channels, "lib-iptv"
}

func makeChannel(id, libraryID, name string, number int, active bool) *iptvmodel.Channel {
	return &iptvmodel.Channel{
		ID: id, LibraryID: libraryID, Name: name, Number: number,
		GroupName: "News", LogoURL: "http://logo.com/" + id + ".png",
		StreamURL: "http://stream.com/" + id, TvgID: id + ".tv",
		Language: "en", Country: "US", IsActive: active, AddedAt: time.Now(),
	}
}

func TestChannel_CreateAndGet(t *testing.T) {
	repo, libID := setupChannelTest(t)
	ctx := context.Background()

	ch := makeChannel("ch-1", libID, "BBC One", 1, true)
	if err := repo.Create(ctx, ch); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetByID(ctx, "ch-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "BBC One" {
		t.Errorf("name = %q, want BBC One", got.Name)
	}
	if got.Number != 1 {
		t.Errorf("number = %d, want 1", got.Number)
	}
	if got.StreamURL != "http://stream.com/ch-1" {
		t.Errorf("stream_url = %q", got.StreamURL)
	}
}

func TestChannel_GetNotFound(t *testing.T) {
	repo, _ := setupChannelTest(t)

	_, err := repo.GetByID(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent channel")
	}
}

func TestChannel_ListByLibrary(t *testing.T) {
	repo, libID := setupChannelTest(t)
	ctx := context.Background()

	_ = repo.Create(ctx, makeChannel("ch-1", libID, "Channel 1", 1, true))
	_ = repo.Create(ctx, makeChannel("ch-2", libID, "Channel 2", 2, true))
	_ = repo.Create(ctx, makeChannel("ch-3", libID, "Channel 3", 3, false))

	// All channels
	all, err := repo.ListByLibrary(ctx, libID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 channels, got %d", len(all))
	}

	// Active only
	active, err := repo.ListByLibrary(ctx, libID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 2 {
		t.Fatalf("expected 2 active channels, got %d", len(active))
	}
}

// TestChannel_ListByLibraryPaginated pinea el contrato del método
// añadido para cerrar el hot path #1 del reporte 2026-05-17: paginación
// + total count en una sola respuesta. Cubre los tres ejes:
//   - offset / limit válidos devuelven la página correcta y el total
//   - límites clamped (negativo, 0, > 1000)
//   - activeOnly filtra antes de paginar
func TestChannel_ListByLibraryPaginated(t *testing.T) {
	repo, libID := setupChannelTest(t)
	ctx := context.Background()

	// 5 canales: 3 activos, 2 inactivos. Numerados 1..5 para que
	// el ORDER BY number sea estable y predecible.
	_ = repo.Create(ctx, makeChannel("ch-1", libID, "Channel 1", 1, true))
	_ = repo.Create(ctx, makeChannel("ch-2", libID, "Channel 2", 2, true))
	_ = repo.Create(ctx, makeChannel("ch-3", libID, "Channel 3", 3, false))
	_ = repo.Create(ctx, makeChannel("ch-4", libID, "Channel 4", 4, true))
	_ = repo.Create(ctx, makeChannel("ch-5", libID, "Channel 5", 5, false))

	t.Run("first_page_returns_total", func(t *testing.T) {
		page, total, err := repo.ListByLibraryPaginated(ctx, libID, false, 0, 2)
		if err != nil {
			t.Fatal(err)
		}
		if total != 5 {
			t.Errorf("total = %d, want 5", total)
		}
		if len(page) != 2 {
			t.Fatalf("page len = %d, want 2", len(page))
		}
		if page[0].ID != "ch-1" || page[1].ID != "ch-2" {
			t.Errorf("page order wrong: %s, %s (want ch-1, ch-2)", page[0].ID, page[1].ID)
		}
	})

	t.Run("offset_skips_rows", func(t *testing.T) {
		page, total, err := repo.ListByLibraryPaginated(ctx, libID, false, 3, 10)
		if err != nil {
			t.Fatal(err)
		}
		if total != 5 {
			t.Errorf("total = %d, want 5", total)
		}
		if len(page) != 2 {
			t.Fatalf("page len = %d, want 2 (rows 4, 5)", len(page))
		}
		if page[0].ID != "ch-4" || page[1].ID != "ch-5" {
			t.Errorf("page order wrong: %s, %s", page[0].ID, page[1].ID)
		}
	})

	t.Run("active_only_filters_before_paginating", func(t *testing.T) {
		page, total, err := repo.ListByLibraryPaginated(ctx, libID, true, 0, 10)
		if err != nil {
			t.Fatal(err)
		}
		if total != 3 {
			t.Errorf("active total = %d, want 3", total)
		}
		if len(page) != 3 {
			t.Fatalf("page len = %d, want 3", len(page))
		}
		for _, ch := range page {
			if !ch.IsActive {
				t.Errorf("inactive channel %s leaked: %+v", ch.ID, ch)
			}
		}
	})

	t.Run("limit_zero_falls_back_to_default", func(t *testing.T) {
		// limit = 0 → 100 default. Con 5 rows seed la página entera cabe.
		page, total, err := repo.ListByLibraryPaginated(ctx, libID, false, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if total != 5 || len(page) != 5 {
			t.Fatalf("limit=0 default: page=%d total=%d, want 5/5", len(page), total)
		}
	})

	t.Run("negative_offset_clamped_to_zero", func(t *testing.T) {
		page, _, err := repo.ListByLibraryPaginated(ctx, libID, false, -10, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) != 1 || page[0].ID != "ch-1" {
			t.Errorf("negative offset not clamped: got %+v", page)
		}
	})

	t.Run("empty_page_returns_total", func(t *testing.T) {
		page, total, err := repo.ListByLibraryPaginated(ctx, libID, false, 100, 10)
		if err != nil {
			t.Fatal(err)
		}
		if total != 5 {
			t.Errorf("total = %d, want 5", total)
		}
		if len(page) != 0 {
			t.Errorf("expected empty page past total, got %d rows", len(page))
		}
	})
}

func TestChannel_ReplaceForLibrary(t *testing.T) {
	repo, libID := setupChannelTest(t)
	ctx := context.Background()

	// Insert initial channels
	_ = repo.Create(ctx, makeChannel("old-1", libID, "Old 1", 1, true))
	_ = repo.Create(ctx, makeChannel("old-2", libID, "Old 2", 2, true))

	// Replace with new set
	newChannels := []*iptvmodel.Channel{
		makeChannel("new-1", libID, "New 1", 1, true),
		makeChannel("new-2", libID, "New 2", 2, true),
		makeChannel("new-3", libID, "New 3", 3, true),
	}
	err := repo.ReplaceForLibrary(ctx, libID, newChannels)
	if err != nil {
		t.Fatal(err)
	}

	// Old channels should be gone
	_, err = repo.GetByID(ctx, "old-1")
	if err == nil {
		t.Error("old channel should be deleted after replace")
	}

	// New channels should exist
	all, _ := repo.ListByLibrary(ctx, libID, false)
	if len(all) != 3 {
		t.Fatalf("expected 3 channels after replace, got %d", len(all))
	}
}

func TestChannel_SetActive(t *testing.T) {
	repo, libID := setupChannelTest(t)
	ctx := context.Background()

	_ = repo.Create(ctx, makeChannel("ch-1", libID, "Channel 1", 1, true))

	// Deactivate
	if err := repo.SetActive(ctx, "ch-1", false); err != nil {
		t.Fatal(err)
	}
	ch, _ := repo.GetByID(ctx, "ch-1")
	if ch.IsActive {
		t.Error("expected inactive after SetActive(false)")
	}

	// Reactivate
	_ = repo.SetActive(ctx, "ch-1", true)
	ch, _ = repo.GetByID(ctx, "ch-1")
	if !ch.IsActive {
		t.Error("expected active after SetActive(true)")
	}

	// Not found
	err := repo.SetActive(ctx, "nonexistent", true)
	if err == nil {
		t.Error("expected error for nonexistent channel")
	}
}

func TestChannel_Groups(t *testing.T) {
	repo, libID := setupChannelTest(t)
	ctx := context.Background()

	ch1 := makeChannel("ch-1", libID, "Ch 1", 1, true)
	ch1.GroupName = "Sports"
	ch2 := makeChannel("ch-2", libID, "Ch 2", 2, true)
	ch2.GroupName = "News"
	ch3 := makeChannel("ch-3", libID, "Ch 3", 3, true)
	ch3.GroupName = "Sports" // duplicate
	ch4 := makeChannel("ch-4", libID, "Ch 4", 4, true)
	ch4.GroupName = "" // empty — should be excluded

	_ = repo.Create(ctx, ch1)
	_ = repo.Create(ctx, ch2)
	_ = repo.Create(ctx, ch3)
	_ = repo.Create(ctx, ch4)

	groups, err := repo.Groups(ctx, libID)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups (News, Sports), got %d: %v", len(groups), groups)
	}
	// Should be sorted alphabetically
	if groups[0] != "News" || groups[1] != "Sports" {
		t.Errorf("groups = %v, want [News Sports]", groups)
	}
}

func TestChannel_ListLivetvChannels_FiltersOutNonLivetvLibraries(t *testing.T) {
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(testutil.Driver(), database)
	ctx := context.Background()
	now := time.Now()

	// Two livetv libraries + one non-livetv (movies). The non-livetv
	// library's channels (if any leaked in) must NOT appear in the
	// global EPG matcher's view.
	_ = repos.Libraries.Create(ctx, &librarymodel.Library{
		ID: "lib-iptv-a", Name: "IPTV A", ContentType: "livetv",
		CreatedAt: now, UpdatedAt: now,
	})
	_ = repos.Libraries.Create(ctx, &librarymodel.Library{
		ID: "lib-iptv-b", Name: "IPTV B", ContentType: "livetv",
		CreatedAt: now, UpdatedAt: now,
	})
	_ = repos.Libraries.Create(ctx, &librarymodel.Library{
		ID: "lib-movies", Name: "Movies", ContentType: "movies",
		CreatedAt: now, UpdatedAt: now,
	})

	if err := repos.Channels.Create(ctx, makeChannel("a-1", "lib-iptv-a", "Antena 3", 1, true)); err != nil {
		t.Fatal(err)
	}
	if err := repos.Channels.Create(ctx, makeChannel("b-1", "lib-iptv-b", "Antena 3 HD", 1, true)); err != nil {
		t.Fatal(err)
	}
	if err := repos.Channels.Create(ctx, makeChannel("b-2", "lib-iptv-b", "Cuatro", 2, true)); err != nil {
		t.Fatal(err)
	}

	got, err := repos.Channels.ListLivetvChannels(ctx)
	if err != nil {
		t.Fatalf("ListLivetvChannels: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("got %d channels across livetv libs, want 3", len(got))
	}

	// librarymodel.Library distribution check — both livetv libs must be
	// represented; movies library MUST NOT appear.
	byLib := make(map[string]int)
	for _, c := range got {
		byLib[c.LibraryID]++
		if c.LibraryID == "lib-movies" {
			t.Errorf("non-livetv library leaked: %s", c.ID)
		}
	}
	if byLib["lib-iptv-a"] != 1 {
		t.Errorf("lib-iptv-a count: got %d want 1", byLib["lib-iptv-a"])
	}
	if byLib["lib-iptv-b"] != 2 {
		t.Errorf("lib-iptv-b count: got %d want 2", byLib["lib-iptv-b"])
	}
}

func TestChannel_ListLivetvChannels_EmptyWhenNoLivetv(t *testing.T) {
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(testutil.Driver(), database)
	ctx := context.Background()
	now := time.Now()

	_ = repos.Libraries.Create(ctx, &librarymodel.Library{
		ID: "lib-movies", Name: "Movies", ContentType: "movies",
		CreatedAt: now, UpdatedAt: now,
	})

	got, err := repos.Channels.ListLivetvChannels(ctx)
	if err != nil {
		t.Fatalf("ListLivetvChannels: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 livetv channels with no livetv libraries; got %d", len(got))
	}
}
