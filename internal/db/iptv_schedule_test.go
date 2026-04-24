package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

// createTestLibrary inserts a minimal livetv library so the schedule
// rows can FK against it. Returns the library id.
func createTestLibrary(t *testing.T, repos *db.Repositories, id string) {
	t.Helper()
	now := time.Now().UTC()
	if err := repos.Libraries.Create(context.Background(), &db.Library{
		ID: id, Name: id, ContentType: "livetv", ScanMode: "manual",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create library: %v", err)
	}
}

func TestIPTVSchedule_UpsertCreatesRow(t *testing.T) {
	repos := testutil.NewTestRepos(t)
	createTestLibrary(t, repos, "lib-a")
	ctx := context.Background()

	job := &db.IPTVScheduledJob{
		LibraryID:     "lib-a",
		Kind:          db.IPTVJobKindM3URefresh,
		IntervalHours: 12,
		Enabled:       true,
	}
	if err := repos.IPTVSchedules.Upsert(ctx, job); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := repos.IPTVSchedules.Get(ctx, "lib-a", db.IPTVJobKindM3URefresh)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.IntervalHours != 12 || !got.Enabled {
		t.Errorf("unexpected row: %+v", got)
	}
}

func TestIPTVSchedule_UpsertPreservesLastRun(t *testing.T) {
	// Regression: changing the interval from the UI should NOT reset
	// the last_run_at / last_status fields. The admin expects the
	// "last ran 3 h ago" signal to persist across reconfiguration.
	repos := testutil.NewTestRepos(t)
	createTestLibrary(t, repos, "lib-a")
	ctx := context.Background()

	if err := repos.IPTVSchedules.Upsert(ctx, &db.IPTVScheduledJob{
		LibraryID: "lib-a", Kind: db.IPTVJobKindEPGRefresh,
		IntervalHours: 6, Enabled: true,
	}); err != nil {
		t.Fatalf("initial upsert: %v", err)
	}

	ranAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	if err := repos.IPTVSchedules.RecordRun(ctx, "lib-a",
		db.IPTVJobKindEPGRefresh, "ok", "", 500*time.Millisecond, ranAt); err != nil {
		t.Fatalf("record run: %v", err)
	}

	// Change interval; enabled stays true.
	if err := repos.IPTVSchedules.Upsert(ctx, &db.IPTVScheduledJob{
		LibraryID: "lib-a", Kind: db.IPTVJobKindEPGRefresh,
		IntervalHours: 12, Enabled: true,
	}); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, err := repos.IPTVSchedules.Get(ctx, "lib-a", db.IPTVJobKindEPGRefresh)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.IntervalHours != 12 {
		t.Errorf("interval not updated: %d", got.IntervalHours)
	}
	if got.LastStatus != "ok" {
		t.Errorf("last_status lost across upsert: %q", got.LastStatus)
	}
	if !got.LastRunAt.Equal(ranAt) {
		t.Errorf("last_run_at lost: got %v want %v", got.LastRunAt, ranAt)
	}
}

func TestIPTVSchedule_GetMissingReturnsSentinel(t *testing.T) {
	repos := testutil.NewTestRepos(t)
	createTestLibrary(t, repos, "lib-a")

	_, err := repos.IPTVSchedules.Get(context.Background(), "lib-a", db.IPTVJobKindM3URefresh)
	if !errors.Is(err, db.ErrIPTVScheduledJobNotFound) {
		t.Errorf("expected ErrIPTVScheduledJobNotFound, got %v", err)
	}
}

func TestIPTVSchedule_ListByLibrary(t *testing.T) {
	repos := testutil.NewTestRepos(t)
	createTestLibrary(t, repos, "lib-a")
	createTestLibrary(t, repos, "lib-b")
	ctx := context.Background()

	// Two rows on lib-a, one on lib-b → list for lib-a returns exactly
	// the two lib-a rows.
	for _, kind := range []string{db.IPTVJobKindM3URefresh, db.IPTVJobKindEPGRefresh} {
		if err := repos.IPTVSchedules.Upsert(ctx, &db.IPTVScheduledJob{
			LibraryID: "lib-a", Kind: kind, IntervalHours: 6, Enabled: true,
		}); err != nil {
			t.Fatalf("upsert lib-a %s: %v", kind, err)
		}
	}
	if err := repos.IPTVSchedules.Upsert(ctx, &db.IPTVScheduledJob{
		LibraryID: "lib-b", Kind: db.IPTVJobKindM3URefresh,
		IntervalHours: 24, Enabled: false,
	}); err != nil {
		t.Fatalf("upsert lib-b: %v", err)
	}

	rows, err := repos.IPTVSchedules.ListByLibrary(ctx, "lib-a")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows for lib-a, got %d", len(rows))
	}
	for _, r := range rows {
		if r.LibraryID != "lib-a" {
			t.Errorf("cross-library row leaked: %+v", r)
		}
	}
}

func TestIPTVSchedule_ListDueDropsDisabled(t *testing.T) {
	repos := testutil.NewTestRepos(t)
	createTestLibrary(t, repos, "lib-a")
	ctx := context.Background()

	// Two jobs, only one enabled.
	if err := repos.IPTVSchedules.Upsert(ctx, &db.IPTVScheduledJob{
		LibraryID: "lib-a", Kind: db.IPTVJobKindM3URefresh,
		IntervalHours: 1, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repos.IPTVSchedules.Upsert(ctx, &db.IPTVScheduledJob{
		LibraryID: "lib-a", Kind: db.IPTVJobKindEPGRefresh,
		IntervalHours: 1, Enabled: false,
	}); err != nil {
		t.Fatal(err)
	}

	due, err := repos.IPTVSchedules.ListDue(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("list due: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("expected 1 due row, got %d", len(due))
	}
	if due[0].Kind != db.IPTVJobKindM3URefresh {
		t.Errorf("wrong kind returned: %s", due[0].Kind)
	}
}

func TestIPTVSchedule_ListDueRespectsInterval(t *testing.T) {
	repos := testutil.NewTestRepos(t)
	createTestLibrary(t, repos, "lib-a")
	ctx := context.Background()

	if err := repos.IPTVSchedules.Upsert(ctx, &db.IPTVScheduledJob{
		LibraryID: "lib-a", Kind: db.IPTVJobKindM3URefresh,
		IntervalHours: 6, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	ranAt := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Second)
	if err := repos.IPTVSchedules.RecordRun(ctx, "lib-a",
		db.IPTVJobKindM3URefresh, "ok", "", time.Second, ranAt); err != nil {
		t.Fatal(err)
	}

	// Only 3 h elapsed, interval is 6 h → NOT due.
	due, err := repos.IPTVSchedules.ListDue(ctx, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 0 {
		t.Errorf("expected 0 due rows (3 h elapsed < 6 h interval), got %d", len(due))
	}

	// Jump forward: 7 h elapsed since ranAt → due.
	due, err = repos.IPTVSchedules.ListDue(ctx, ranAt.Add(7*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 {
		t.Errorf("expected 1 due row, got %d", len(due))
	}
}

func TestIPTVSchedule_ListDueIncludesNeverRun(t *testing.T) {
	// Newly-enabled jobs with no last_run_at should always be due on
	// the first tick so the admin sees immediate feedback.
	repos := testutil.NewTestRepos(t)
	createTestLibrary(t, repos, "lib-a")
	ctx := context.Background()

	if err := repos.IPTVSchedules.Upsert(ctx, &db.IPTVScheduledJob{
		LibraryID: "lib-a", Kind: db.IPTVJobKindEPGRefresh,
		IntervalHours: 24, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	due, err := repos.IPTVSchedules.ListDue(ctx, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 {
		t.Errorf("never-run job should be due: got %d", len(due))
	}
}

func TestIPTVSchedule_UpsertRejectsInvalid(t *testing.T) {
	repos := testutil.NewTestRepos(t)
	createTestLibrary(t, repos, "lib-a")
	ctx := context.Background()

	cases := []struct {
		name string
		job  *db.IPTVScheduledJob
	}{
		{"zero interval", &db.IPTVScheduledJob{
			LibraryID: "lib-a", Kind: db.IPTVJobKindM3URefresh, IntervalHours: 0,
		}},
		{"negative interval", &db.IPTVScheduledJob{
			LibraryID: "lib-a", Kind: db.IPTVJobKindM3URefresh, IntervalHours: -5,
		}},
		{"unknown kind", &db.IPTVScheduledJob{
			LibraryID: "lib-a", Kind: "bogus", IntervalHours: 6,
		}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if err := repos.IPTVSchedules.Upsert(ctx, tc.job); err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestIPTVSchedule_RecordRunTrimsLongError(t *testing.T) {
	repos := testutil.NewTestRepos(t)
	createTestLibrary(t, repos, "lib-a")
	ctx := context.Background()

	if err := repos.IPTVSchedules.Upsert(ctx, &db.IPTVScheduledJob{
		LibraryID: "lib-a", Kind: db.IPTVJobKindM3URefresh,
		IntervalHours: 6, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	longErr := make([]byte, 2000)
	for i := range longErr {
		longErr[i] = 'x'
	}
	if err := repos.IPTVSchedules.RecordRun(ctx, "lib-a",
		db.IPTVJobKindM3URefresh, "error", string(longErr),
		time.Second, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	got, err := repos.IPTVSchedules.Get(ctx, "lib-a", db.IPTVJobKindM3URefresh)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.LastError) > 512 {
		t.Errorf("error message not trimmed: len=%d", len(got.LastError))
	}
}

func TestIPTVSchedule_DeleteIsIdempotent(t *testing.T) {
	repos := testutil.NewTestRepos(t)
	createTestLibrary(t, repos, "lib-a")
	ctx := context.Background()

	// Delete a row that was never created → no-op, no error.
	if err := repos.IPTVSchedules.Delete(ctx, "lib-a", db.IPTVJobKindM3URefresh); err != nil {
		t.Fatalf("delete missing: %v", err)
	}

	if err := repos.IPTVSchedules.Upsert(ctx, &db.IPTVScheduledJob{
		LibraryID: "lib-a", Kind: db.IPTVJobKindM3URefresh,
		IntervalHours: 6, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repos.IPTVSchedules.Delete(ctx, "lib-a", db.IPTVJobKindM3URefresh); err != nil {
		t.Fatalf("delete existing: %v", err)
	}
	if _, err := repos.IPTVSchedules.Get(ctx, "lib-a", db.IPTVJobKindM3URefresh); !errors.Is(err, db.ErrIPTVScheduledJobNotFound) {
		t.Errorf("row still present after delete: %v", err)
	}
}
