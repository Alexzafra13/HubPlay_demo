package db_test

import (
	"context"
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

// seedWatchFixture inserts a user, a library and N channels the
// watch-history tests can point at. Returns the channel IDs for
// convenience; the user id is stable "u-alice" so tests can reference
// it inline.
func seedWatchFixture(t *testing.T, repos *db.Repositories, n int) []string {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()

	if err := repos.Users.Create(ctx, &db.User{
		ID: "u-alice", Username: "alice", PasswordHash: "hash",
		DisplayName: "Alice", Role: "user", IsActive: true, CreatedAt: now,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repos.Libraries.Create(ctx, &db.Library{
		ID: "lib-a", Name: "lib-a", ContentType: "livetv", ScanMode: "manual",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create library: %v", err)
	}
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		id := "ch-" + string(rune('a'+i))
		if err := repos.Channels.Create(ctx, &db.Channel{
			ID: id, LibraryID: "lib-a", Name: "Channel " + id,
			Number: i + 1, StreamURL: "http://example/" + id,
			IsActive: true, AddedAt: now,
		}); err != nil {
			t.Fatalf("create channel %s: %v", id, err)
		}
		ids = append(ids, id)
	}
	return ids
}

// streamURLFor mirrors the seed pattern above so tests can translate
// channel ids to their stream URLs without loading the channel.
func streamURLFor(channelID string) string {
	return "http://example/" + channelID
}

func TestChannelWatchHistory_RecordUpsertsRow(t *testing.T) {
	repos := testutil.NewTestRepos(t)
	ids := seedWatchFixture(t, repos, 1)
	ctx := context.Background()

	first, err := repos.ChannelWatchHistory.RecordByStreamURL(ctx, "u-alice", streamURLFor(ids[0]))
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if first.IsZero() {
		t.Error("timestamp zero")
	}

	// Second record on the same pair must update the timestamp, not
	// create a duplicate row.
	time.Sleep(2 * time.Millisecond)
	second, err := repos.ChannelWatchHistory.RecordByStreamURL(ctx, "u-alice", streamURLFor(ids[0]))
	if err != nil {
		t.Fatal(err)
	}
	if !second.After(first) {
		t.Errorf("second timestamp not newer: %v vs %v", second, first)
	}
	channels, _, err := repos.ChannelWatchHistory.ListChannelsByUser(ctx, "u-alice", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 1 {
		t.Errorf("expected 1 row after 2 records, got %d", len(channels))
	}
}

func TestChannelWatchHistory_ListOrderedByRecency(t *testing.T) {
	repos := testutil.NewTestRepos(t)
	ids := seedWatchFixture(t, repos, 3)
	ctx := context.Background()

	// Record in order a, b, c with a sleep between so the timestamps
	// are distinct. Expect the list to come back c, b, a.
	for _, id := range ids {
		if _, err := repos.ChannelWatchHistory.RecordByStreamURL(ctx, "u-alice", streamURLFor(id)); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	channels, _, err := repos.ChannelWatchHistory.ListChannelsByUser(ctx, "u-alice", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(channels))
	}
	wantOrder := []string{ids[2], ids[1], ids[0]}
	for i, ch := range channels {
		if ch.ID != wantOrder[i] {
			t.Errorf("order[%d]: got %s, want %s", i, ch.ID, wantOrder[i])
		}
	}
}

func TestChannelWatchHistory_ListRespectsLimit(t *testing.T) {
	repos := testutil.NewTestRepos(t)
	ids := seedWatchFixture(t, repos, 5)
	ctx := context.Background()
	for _, id := range ids {
		_, _ = repos.ChannelWatchHistory.RecordByStreamURL(ctx, "u-alice", streamURLFor(id))
		time.Sleep(2 * time.Millisecond)
	}
	channels, _, err := repos.ChannelWatchHistory.ListChannelsByUser(ctx, "u-alice", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 3 {
		t.Errorf("limit not respected: got %d, want 3", len(channels))
	}
}

func TestChannelWatchHistory_ListHidesInactiveChannels(t *testing.T) {
	// Deactivating a channel should drop it from the rail without
	// touching the underlying history row (the admin may re-enable it).
	repos := testutil.NewTestRepos(t)
	ids := seedWatchFixture(t, repos, 2)
	ctx := context.Background()

	_, _ = repos.ChannelWatchHistory.RecordByStreamURL(ctx, "u-alice", streamURLFor(ids[0]))
	_, _ = repos.ChannelWatchHistory.RecordByStreamURL(ctx, "u-alice", streamURLFor(ids[1]))

	if err := repos.Channels.SetActive(ctx, ids[0], false); err != nil {
		t.Fatal(err)
	}
	channels, _, err := repos.ChannelWatchHistory.ListChannelsByUser(ctx, "u-alice", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 1 {
		t.Fatalf("expected 1 active channel, got %d", len(channels))
	}
	if channels[0].ID != ids[1] {
		t.Errorf("wrong channel returned: %s", channels[0].ID)
	}
}

func TestChannelWatchHistory_ZeroLimitReturnsEmpty(t *testing.T) {
	repos := testutil.NewTestRepos(t)
	ids := seedWatchFixture(t, repos, 1)
	ctx := context.Background()
	_, _ = repos.ChannelWatchHistory.RecordByStreamURL(ctx, "u-alice", streamURLFor(ids[0]))

	channels, watched, err := repos.ChannelWatchHistory.ListChannelsByUser(ctx, "u-alice", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 0 || len(watched) != 0 {
		t.Errorf("expected empty slices for limit=0, got %d/%d", len(channels), len(watched))
	}
}

func TestChannelWatchHistory_IsolatedPerUser(t *testing.T) {
	repos := testutil.NewTestRepos(t)
	ids := seedWatchFixture(t, repos, 1)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := repos.Users.Create(ctx, &db.User{
		ID: "u-bob", Username: "bob", PasswordHash: "hash",
		DisplayName: "Bob", Role: "user", IsActive: true, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	_, _ = repos.ChannelWatchHistory.RecordByStreamURL(ctx, "u-alice", streamURLFor(ids[0]))
	_, _ = repos.ChannelWatchHistory.RecordByStreamURL(ctx, "u-bob", streamURLFor(ids[0]))

	aliceList, _, _ := repos.ChannelWatchHistory.ListChannelsByUser(ctx, "u-alice", 10)
	bobList, _, _ := repos.ChannelWatchHistory.ListChannelsByUser(ctx, "u-bob", 10)
	if len(aliceList) != 1 || len(bobList) != 1 {
		t.Errorf("unexpected counts: alice=%d bob=%d", len(aliceList), len(bobList))
	}

	// Alice deletes her row; bob's should be untouched.
	if err := repos.ChannelWatchHistory.DeleteByStreamURL(ctx, "u-alice", streamURLFor(ids[0])); err != nil {
		t.Fatal(err)
	}
	aliceAfter, _, _ := repos.ChannelWatchHistory.ListChannelsByUser(ctx, "u-alice", 10)
	bobAfter, _, _ := repos.ChannelWatchHistory.ListChannelsByUser(ctx, "u-bob", 10)
	if len(aliceAfter) != 0 || len(bobAfter) != 1 {
		t.Errorf("cross-user leak: alice=%d bob=%d", len(aliceAfter), len(bobAfter))
	}
}

func TestChannelWatchHistory_SurvivesM3URefresh(t *testing.T) {
	// The key design choice: stream_url is the stable identity across
	// M3U refreshes. ReplaceForLibrary regenerates channel UUIDs but
	// preserves stream URLs, so the watch history must still resolve
	// after a refresh.
	repos := testutil.NewTestRepos(t)
	ids := seedWatchFixture(t, repos, 2)
	ctx := context.Background()
	now := time.Now().UTC()
	streamA := streamURLFor(ids[0])
	streamB := streamURLFor(ids[1])

	_, _ = repos.ChannelWatchHistory.RecordByStreamURL(ctx, "u-alice", streamA)
	_, _ = repos.ChannelWatchHistory.RecordByStreamURL(ctx, "u-alice", streamB)

	// Simulate a full M3U refresh: both channels re-imported with
	// new UUIDs but the same stream URLs. Keep only streamB so we can
	// also assert the "URL missing → row stays but joins to nothing"
	// half of the contract.
	newID := "ch-renewed"
	if err := repos.Channels.ReplaceForLibrary(ctx, "lib-a", []*db.Channel{
		{ID: newID, LibraryID: "lib-a", Name: "Renewed",
			Number: 1, StreamURL: streamB, IsActive: true, AddedAt: now},
	}); err != nil {
		t.Fatal(err)
	}
	channels, _, err := repos.ChannelWatchHistory.ListChannelsByUser(ctx, "u-alice", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 1 {
		t.Fatalf("expected 1 surviving entry, got %d", len(channels))
	}
	if channels[0].ID != newID || channels[0].StreamURL != streamB {
		t.Errorf("wrong channel surfaced: %+v", channels[0])
	}

	// If streamA comes back later the orphan history row re-joins
	// and the rail shows both. Verifies the "URL returns" half.
	if err := repos.Channels.ReplaceForLibrary(ctx, "lib-a", []*db.Channel{
		{ID: newID, LibraryID: "lib-a", Name: "Renewed",
			Number: 1, StreamURL: streamB, IsActive: true, AddedAt: now},
		{ID: "ch-returned", LibraryID: "lib-a", Name: "Returned",
			Number: 2, StreamURL: streamA, IsActive: true, AddedAt: now},
	}); err != nil {
		t.Fatal(err)
	}
	channels, _, err = repos.ChannelWatchHistory.ListChannelsByUser(ctx, "u-alice", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 2 {
		t.Errorf("orphan didn't re-join: got %d rows", len(channels))
	}
}

func TestChannelWatchHistory_CascadesOnUserDelete(t *testing.T) {
	repos := testutil.NewTestRepos(t)
	ids := seedWatchFixture(t, repos, 1)
	ctx := context.Background()
	_, _ = repos.ChannelWatchHistory.RecordByStreamURL(ctx, "u-alice", streamURLFor(ids[0]))

	if err := repos.Users.Delete(ctx, "u-alice"); err != nil {
		t.Fatal(err)
	}
	channels, _, err := repos.ChannelWatchHistory.ListChannelsByUser(ctx, "u-alice", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 0 {
		t.Errorf("history survived user delete: %d rows", len(channels))
	}
}

func TestChannelWatchHistory_DedupesAcrossLibraries(t *testing.T) {
	// Same stream URL attached to two libraries — e.g. an operator
	// shares one playlist between a "Kids" and a "Main" library. The
	// rail must show the channel once, not twice.
	repos := testutil.NewTestRepos(t)
	ids := seedWatchFixture(t, repos, 1)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := repos.Libraries.Create(ctx, &db.Library{
		ID: "lib-b", Name: "lib-b", ContentType: "livetv", ScanMode: "manual",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Channels.Create(ctx, &db.Channel{
		ID: "ch-dup", LibraryID: "lib-b", Name: "Dup",
		Number: 1, StreamURL: streamURLFor(ids[0]),
		IsActive: true, AddedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	_, _ = repos.ChannelWatchHistory.RecordByStreamURL(ctx, "u-alice", streamURLFor(ids[0]))

	channels, _, err := repos.ChannelWatchHistory.ListChannelsByUser(ctx, "u-alice", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 1 {
		t.Errorf("stream_url duplicated across libraries surfaced %d times", len(channels))
	}
}

func TestChannelWatchHistory_DeleteIsIdempotent(t *testing.T) {
	repos := testutil.NewTestRepos(t)
	ids := seedWatchFixture(t, repos, 1)
	ctx := context.Background()
	stream := streamURLFor(ids[0])

	if err := repos.ChannelWatchHistory.DeleteByStreamURL(ctx, "u-alice", stream); err != nil {
		t.Fatalf("delete-missing: %v", err)
	}
	_, _ = repos.ChannelWatchHistory.RecordByStreamURL(ctx, "u-alice", stream)
	if err := repos.ChannelWatchHistory.DeleteByStreamURL(ctx, "u-alice", stream); err != nil {
		t.Fatalf("delete-existing: %v", err)
	}
	if err := repos.ChannelWatchHistory.DeleteByStreamURL(ctx, "u-alice", stream); err != nil {
		t.Fatalf("delete-again: %v", err)
	}
}
