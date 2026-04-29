package db_test

import (
	"context"
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

func setupChannelTest(t *testing.T) (*db.ChannelRepository, string) {
	t.Helper()
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(database)

	now := time.Now()
	_ = repos.Libraries.Create(context.Background(), &db.Library{
		ID: "lib-iptv", Name: "Live TV", ContentType: "livetv",
		CreatedAt: now, UpdatedAt: now,
	})

	return repos.Channels, "lib-iptv"
}

func makeChannel(id, libraryID, name string, number int, active bool) *db.Channel {
	return &db.Channel{
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

func TestChannel_ReplaceForLibrary(t *testing.T) {
	repo, libID := setupChannelTest(t)
	ctx := context.Background()

	// Insert initial channels
	_ = repo.Create(ctx, makeChannel("old-1", libID, "Old 1", 1, true))
	_ = repo.Create(ctx, makeChannel("old-2", libID, "Old 2", 2, true))

	// Replace with new set
	newChannels := []*db.Channel{
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
	repos := db.NewRepositories(database)
	ctx := context.Background()
	now := time.Now()

	// Two livetv libraries + one non-livetv (movies). The non-livetv
	// library's channels (if any leaked in) must NOT appear in the
	// global EPG matcher's view.
	_ = repos.Libraries.Create(ctx, &db.Library{
		ID: "lib-iptv-a", Name: "IPTV A", ContentType: "livetv",
		CreatedAt: now, UpdatedAt: now,
	})
	_ = repos.Libraries.Create(ctx, &db.Library{
		ID: "lib-iptv-b", Name: "IPTV B", ContentType: "livetv",
		CreatedAt: now, UpdatedAt: now,
	})
	_ = repos.Libraries.Create(ctx, &db.Library{
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

	// Library distribution check — both livetv libs must be
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
	repos := db.NewRepositories(database)
	ctx := context.Background()
	now := time.Now()

	_ = repos.Libraries.Create(ctx, &db.Library{
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
