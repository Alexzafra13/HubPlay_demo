package db_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

// setupChannelHealthTest seeds a library with N channels so the health
// API has real rows to work against. Uses the same helper style as the
// other channel tests in this package.
func setupChannelHealthTest(t *testing.T) (*db.ChannelRepository, string) {
	t.Helper()
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(database)
	ctx := context.Background()

	libID := "lib-health"
	now := time.Now()
	if err := repos.Libraries.Create(ctx, &db.Library{
		ID: libID, Name: "H", ContentType: "livetv", ScanMode: "manual",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed library: %v", err)
	}
	for i, id := range []string{"ch-ok", "ch-flaky", "ch-dead", "ch-disabled"} {
		ch := makeChannel(id, libID, id, i+1, true)
		if id == "ch-disabled" {
			ch.IsActive = false
		}
		if err := repos.Channels.Create(ctx, ch); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	return repos.Channels, libID
}

func TestChannel_RecordProbeSuccessResetsCounter(t *testing.T) {
	repo, libID := setupChannelHealthTest(t)
	ctx := context.Background()

	// Pile up failures first.
	for i := 0; i < 4; i++ {
		if err := repo.RecordProbeFailure(ctx, "ch-flaky", "transient blip"); err != nil {
			t.Fatalf("failure %d: %v", i, err)
		}
	}

	// A successful probe must reset to zero.
	if err := repo.RecordProbeSuccess(ctx, "ch-flaky"); err != nil {
		t.Fatalf("success: %v", err)
	}

	unhealthy, err := repo.ListUnhealthyByLibrary(ctx, libID, db.UnhealthyThreshold)
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range unhealthy {
		if ch.ID == "ch-flaky" {
			t.Errorf("ch-flaky should be healthy again after success, got failures=%d",
				ch.ConsecutiveFailures)
		}
	}
}

func TestChannel_RecordProbeFailureIsAtomic(t *testing.T) {
	repo, libID := setupChannelHealthTest(t)
	ctx := context.Background()

	// Two concurrent writers must together produce exactly 10 failures —
	// the UPDATE uses consecutive_failures+1 so the DB serialises them.
	var wg [10]chan struct{}
	for i := range wg {
		wg[i] = make(chan struct{})
		go func(ch chan struct{}) {
			defer close(ch)
			_ = repo.RecordProbeFailure(ctx, "ch-dead", "upstream 503")
		}(wg[i])
	}
	for _, c := range wg {
		<-c
	}

	unhealthy, err := repo.ListUnhealthyByLibrary(ctx, libID, 1)
	if err != nil {
		t.Fatal(err)
	}
	var found *db.Channel
	for _, ch := range unhealthy {
		if ch.ID == "ch-dead" {
			found = ch
		}
	}
	if found == nil {
		t.Fatal("ch-dead not listed as unhealthy")
	}
	if found.ConsecutiveFailures != 10 {
		t.Errorf("atomic +1 lost an update: got %d, want 10", found.ConsecutiveFailures)
	}
}

func TestChannel_ListUnhealthyByLibrary_FiltersByThreshold(t *testing.T) {
	repo, libID := setupChannelHealthTest(t)
	ctx := context.Background()

	_ = repo.RecordProbeFailure(ctx, "ch-ok", "one blip")
	for i := 0; i < 3; i++ {
		_ = repo.RecordProbeFailure(ctx, "ch-flaky", "three strikes")
	}
	for i := 0; i < 5; i++ {
		_ = repo.RecordProbeFailure(ctx, "ch-dead", "consistently broken")
	}

	// Default threshold (3) excludes ch-ok (1 failure) and includes the other two.
	unhealthy, err := repo.ListUnhealthyByLibrary(ctx, libID, db.UnhealthyThreshold)
	if err != nil {
		t.Fatal(err)
	}
	if len(unhealthy) != 2 {
		t.Fatalf("default threshold: got %d, want 2; %+v", len(unhealthy), unhealthy)
	}
	// Sort order is worst-first.
	if unhealthy[0].ID != "ch-dead" {
		t.Errorf("worst-first order broken: got %v", unhealthy[0].ID)
	}
}

func TestChannel_ListUnhealthyByLibrary_PopulatesHealthFields(t *testing.T) {
	repo, libID := setupChannelHealthTest(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_ = repo.RecordProbeFailure(ctx, "ch-dead", "no such host")
	}

	unhealthy, err := repo.ListUnhealthyByLibrary(ctx, libID, db.UnhealthyThreshold)
	if err != nil {
		t.Fatal(err)
	}
	if len(unhealthy) != 1 {
		t.Fatalf("len = %d", len(unhealthy))
	}
	got := unhealthy[0]
	if got.LastProbeStatus != "error" {
		t.Errorf("last_probe_status = %q, want error", got.LastProbeStatus)
	}
	if !strings.Contains(got.LastProbeError, "no such host") {
		t.Errorf("last_probe_error = %q, should contain 'no such host'", got.LastProbeError)
	}
	if got.LastProbeAt.IsZero() {
		t.Error("last_probe_at should be populated")
	}
	if got.ConsecutiveFailures != 3 {
		t.Errorf("consecutive_failures = %d, want 3", got.ConsecutiveFailures)
	}
}

// Anything over the 500-rune limit must be trimmed at the repo layer
// so a paranoid upstream can't stuff megabytes into the column.
func TestChannel_RecordProbeFailure_TrimsLongErrors(t *testing.T) {
	repo, libID := setupChannelHealthTest(t)
	ctx := context.Background()

	huge := strings.Repeat("x", 10_000)
	if err := repo.RecordProbeFailure(ctx, "ch-dead", huge); err != nil {
		t.Fatalf("record: %v", err)
	}

	unhealthy, _ := repo.ListUnhealthyByLibrary(ctx, libID, 1)
	if len(unhealthy) != 1 {
		t.Fatalf("len = %d", len(unhealthy))
	}
	if len(unhealthy[0].LastProbeError) >= 10_000 {
		t.Errorf("error not trimmed: length = %d", len(unhealthy[0].LastProbeError))
	}
}

func TestChannel_ListHealthyByLibrary_HidesUnhealthyAndDisabled(t *testing.T) {
	repo, libID := setupChannelHealthTest(t)
	ctx := context.Background()

	// Push ch-dead past the threshold.
	for i := 0; i < 5; i++ {
		_ = repo.RecordProbeFailure(ctx, "ch-dead", "x")
	}

	healthy, err := repo.ListHealthyByLibrary(ctx, libID)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool, len(healthy))
	for _, ch := range healthy {
		seen[ch.ID] = true
	}
	if !seen["ch-ok"] {
		t.Error("ch-ok should be visible")
	}
	if !seen["ch-flaky"] {
		t.Error("ch-flaky should be visible (below threshold)")
	}
	if seen["ch-dead"] {
		t.Error("ch-dead is over threshold; should be hidden")
	}
	if seen["ch-disabled"] {
		t.Error("ch-disabled is is_active=false; should be hidden")
	}
}

func TestChannel_ResetHealthClearsEverything(t *testing.T) {
	repo, libID := setupChannelHealthTest(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_ = repo.RecordProbeFailure(ctx, "ch-dead", "x")
	}
	if err := repo.ResetHealth(ctx, "ch-dead"); err != nil {
		t.Fatalf("reset: %v", err)
	}

	unhealthy, _ := repo.ListUnhealthyByLibrary(ctx, libID, 1)
	for _, ch := range unhealthy {
		if ch.ID == "ch-dead" {
			t.Errorf("ch-dead should be cleared; got %+v", ch)
		}
	}
}
