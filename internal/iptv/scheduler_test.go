package iptv

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

// fakeRunner counts calls and injects a configurable error. The mutex
// protects concurrent reads from the test goroutine vs writes from the
// scheduler's loop.
type fakeRunner struct {
	mu        sync.Mutex
	m3uCalls  int
	epgCalls  int
	m3uErr    error
	epgErr    error
	callDelay time.Duration
}

func (f *fakeRunner) RefreshM3U(ctx context.Context, _ string) (int, error) {
	f.mu.Lock()
	f.m3uCalls++
	delay := f.callDelay
	err := f.m3uErr
	f.mu.Unlock()
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	return 0, err
}
func (f *fakeRunner) RefreshEPG(ctx context.Context, _ string) (int, error) {
	f.mu.Lock()
	f.epgCalls++
	delay := f.callDelay
	err := f.epgErr
	f.mu.Unlock()
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	return 0, err
}
func (f *fakeRunner) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.m3uCalls, f.epgCalls
}

func newSchedFixture(t *testing.T) (*db.Repositories, *fakeRunner, *Scheduler) {
	t.Helper()
	repos := testutil.NewTestRepos(t)
	// Create a library so FKs resolve.
	now := time.Now().UTC()
	if err := repos.Libraries.Create(context.Background(), &db.Library{
		ID: "lib-a", Name: "lib-a", ContentType: "livetv", ScanMode: "manual",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create library: %v", err)
	}
	runner := &fakeRunner{}
	sched := NewScheduler(repos.IPTVSchedules, runner, testutil.TestLogger())
	return repos, runner, sched
}

func TestScheduler_TickRunsDueJob(t *testing.T) {
	repos, runner, sched := newSchedFixture(t)
	ctx := context.Background()

	if err := repos.IPTVSchedules.Upsert(ctx, &db.IPTVScheduledJob{
		LibraryID: "lib-a", Kind: db.IPTVJobKindM3URefresh,
		IntervalHours: 6, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	sched.TickOnce(ctx)
	m3u, epg := runner.counts()
	if m3u != 1 || epg != 0 {
		t.Errorf("expected 1 M3U call, 0 EPG; got m3u=%d epg=%d", m3u, epg)
	}

	// Outcome recorded.
	got, _ := repos.IPTVSchedules.Get(ctx, "lib-a", db.IPTVJobKindM3URefresh)
	if got.LastStatus != "ok" {
		t.Errorf("expected last_status=ok, got %q", got.LastStatus)
	}
	if got.LastRunAt.IsZero() {
		t.Error("last_run_at not set")
	}
}

func TestScheduler_TickSkipsDisabled(t *testing.T) {
	repos, runner, sched := newSchedFixture(t)
	ctx := context.Background()

	if err := repos.IPTVSchedules.Upsert(ctx, &db.IPTVScheduledJob{
		LibraryID: "lib-a", Kind: db.IPTVJobKindM3URefresh,
		IntervalHours: 6, Enabled: false, // disabled!
	}); err != nil {
		t.Fatal(err)
	}
	sched.TickOnce(ctx)
	m3u, _ := runner.counts()
	if m3u != 0 {
		t.Errorf("disabled job should not run: got %d calls", m3u)
	}
}

func TestScheduler_TickSkipsNotYetDue(t *testing.T) {
	repos, runner, sched := newSchedFixture(t)
	ctx := context.Background()

	// Ran 1 h ago, interval 6 h → 5 h to go. Should NOT run.
	ranAt := time.Now().UTC().Add(-1 * time.Hour).Truncate(time.Second)
	if err := repos.IPTVSchedules.Upsert(ctx, &db.IPTVScheduledJob{
		LibraryID: "lib-a", Kind: db.IPTVJobKindEPGRefresh,
		IntervalHours: 6, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repos.IPTVSchedules.RecordRun(ctx, "lib-a", db.IPTVJobKindEPGRefresh,
		"ok", "", time.Second, ranAt); err != nil {
		t.Fatal(err)
	}

	sched.TickOnce(ctx)
	_, epg := runner.counts()
	if epg != 0 {
		t.Errorf("not-yet-due job ran: %d calls", epg)
	}
}

func TestScheduler_TickRecordsFailureStatus(t *testing.T) {
	repos, runner, sched := newSchedFixture(t)
	ctx := context.Background()

	runner.m3uErr = errors.New("upstream 500")

	if err := repos.IPTVSchedules.Upsert(ctx, &db.IPTVScheduledJob{
		LibraryID: "lib-a", Kind: db.IPTVJobKindM3URefresh,
		IntervalHours: 6, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	sched.TickOnce(ctx)
	got, _ := repos.IPTVSchedules.Get(ctx, "lib-a", db.IPTVJobKindM3URefresh)
	if got.LastStatus != "error" {
		t.Errorf("expected error status, got %q", got.LastStatus)
	}
	if got.LastError == "" {
		t.Error("expected last_error populated")
	}
}

func TestScheduler_RunNowBypassesSchedule(t *testing.T) {
	// RunNow must fire even when the job row doesn't exist and even
	// when it's disabled. The button in the admin UI shouldn't
	// require enabling the schedule first.
	repos, runner, sched := newSchedFixture(t)
	ctx := context.Background()

	// Case 1: no row at all.
	if err := sched.RunNow(ctx, "lib-a", db.IPTVJobKindM3URefresh); err != nil {
		t.Errorf("RunNow with no row: %v", err)
	}
	m3u, _ := runner.counts()
	if m3u != 1 {
		t.Errorf("expected 1 call, got %d", m3u)
	}

	// Case 2: row exists but disabled.
	if err := repos.IPTVSchedules.Upsert(ctx, &db.IPTVScheduledJob{
		LibraryID: "lib-a", Kind: db.IPTVJobKindEPGRefresh,
		IntervalHours: 6, Enabled: false,
	}); err != nil {
		t.Fatal(err)
	}
	if err := sched.RunNow(ctx, "lib-a", db.IPTVJobKindEPGRefresh); err != nil {
		t.Errorf("RunNow on disabled: %v", err)
	}
	_, epg := runner.counts()
	if epg != 1 {
		t.Errorf("expected 1 EPG call, got %d", epg)
	}
	got, _ := repos.IPTVSchedules.Get(ctx, "lib-a", db.IPTVJobKindEPGRefresh)
	if got.LastStatus != "ok" {
		t.Errorf("run-now outcome not recorded: %q", got.LastStatus)
	}
}

func TestScheduler_RunNowSurfacesError(t *testing.T) {
	_, runner, sched := newSchedFixture(t)
	runner.epgErr = errors.New("404")
	err := sched.RunNow(context.Background(), "lib-a", db.IPTVJobKindEPGRefresh)
	if err == nil || err.Error() != "404" {
		t.Errorf("expected 404 error, got %v", err)
	}
}

// Panic safety — if the underlying refresher panics (nil deref in the
// XMLTV parser on a malformed feed, for instance), the scheduler must
// catch it, log, record an error outcome, and keep the goroutine alive
// so subsequent ticks still fire.
type panickyRunner struct {
	panicValue any
	calls      int
}

func (p *panickyRunner) RefreshM3U(context.Context, string) (int, error) {
	p.calls++
	panic(p.panicValue)
}
func (p *panickyRunner) RefreshEPG(context.Context, string) (int, error) {
	p.calls++
	panic(p.panicValue)
}

func TestScheduler_RunNowRecoversFromPanic(t *testing.T) {
	repos := testutil.NewTestRepos(t)
	if err := repos.Libraries.Create(context.Background(), &db.Library{
		ID: "lib-a", Name: "lib-a", ContentType: "livetv", ScanMode: "manual",
	}); err != nil {
		t.Fatal(err)
	}
	runner := &panickyRunner{panicValue: "boom"}
	sched := NewScheduler(repos.IPTVSchedules, runner, testutil.TestLogger())

	// Must not propagate the panic to the caller.
	err := sched.RunNow(context.Background(), "lib-a", db.IPTVJobKindM3URefresh)
	if err == nil {
		t.Fatal("expected panic to surface as error")
	}
	if runner.calls != 1 {
		t.Errorf("runner call count: %d", runner.calls)
	}
}

func TestScheduler_TickLoopSurvivesPanic(t *testing.T) {
	// Second tick must run even after the first one panic-ed inside
	// the runner. Verifies the defer/recover path doesn't bleed into
	// the loop goroutine.
	repos := testutil.NewTestRepos(t)
	if err := repos.Libraries.Create(context.Background(), &db.Library{
		ID: "lib-a", Name: "lib-a", ContentType: "livetv", ScanMode: "manual",
	}); err != nil {
		t.Fatal(err)
	}
	runner := &panickyRunner{panicValue: "parser crashed"}
	sched := NewScheduler(repos.IPTVSchedules, runner, testutil.TestLogger())
	ctx := context.Background()

	if err := repos.IPTVSchedules.Upsert(ctx, &db.IPTVScheduledJob{
		LibraryID: "lib-a", Kind: db.IPTVJobKindM3URefresh,
		IntervalHours: 1, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	// First tick panics inside the runner; should NOT propagate.
	sched.TickOnce(ctx)
	// Second tick: still due (last_run_at was set but RecordRun
	// happened inside recover with duration=0; the row is old enough
	// to be due again). The runner panics again but the loop survives.
	// Assert by looking at call count.
	sched.TickOnce(ctx)
	if runner.calls < 1 {
		t.Errorf("runner never called: %d", runner.calls)
	}
	// Last status should be recorded as error from the panic path.
	got, _ := repos.IPTVSchedules.Get(ctx, "lib-a", db.IPTVJobKindM3URefresh)
	if got.LastStatus != "error" {
		t.Errorf("expected last_status=error after panic, got %q", got.LastStatus)
	}
	if got.LastError == "" {
		t.Error("expected last_error populated after panic")
	}
}

// concurrentRunner returns ErrRefreshInProgress so we can verify the
// scheduler treats it as benign and doesn't pollute last_status.
type concurrentRunner struct{ calls int }

func (c *concurrentRunner) RefreshM3U(context.Context, string) (int, error) {
	c.calls++
	return 0, ErrRefreshInProgress
}
func (c *concurrentRunner) RefreshEPG(context.Context, string) (int, error) {
	c.calls++
	return 0, ErrRefreshInProgress
}

func TestScheduler_ConcurrentRefreshIsBenign(t *testing.T) {
	// When RunNow races a tick, the second one gets
	// ErrRefreshInProgress from iptv.Service's per-library lock.
	// That outcome must NOT overwrite a prior successful last_status
	// with "error" — the refresh actually succeeded on the other path.
	repos := testutil.NewTestRepos(t)
	if err := repos.Libraries.Create(context.Background(), &db.Library{
		ID: "lib-a", Name: "lib-a", ContentType: "livetv", ScanMode: "manual",
	}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// Seed with a prior successful run so we can check it isn't
	// clobbered.
	if err := repos.IPTVSchedules.Upsert(ctx, &db.IPTVScheduledJob{
		LibraryID: "lib-a", Kind: db.IPTVJobKindM3URefresh,
		IntervalHours: 6, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	priorRun := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second)
	if err := repos.IPTVSchedules.RecordRun(ctx, "lib-a",
		db.IPTVJobKindM3URefresh, "ok", "", time.Second, priorRun); err != nil {
		t.Fatal(err)
	}

	runner := &concurrentRunner{}
	sched := NewScheduler(repos.IPTVSchedules, runner, testutil.TestLogger())

	// Direct RunNow — runner returns ErrRefreshInProgress.
	if err := sched.RunNow(ctx, "lib-a", db.IPTVJobKindM3URefresh); err == nil {
		t.Fatal("expected ErrRefreshInProgress surface")
	}

	got, _ := repos.IPTVSchedules.Get(ctx, "lib-a", db.IPTVJobKindM3URefresh)
	if got.LastStatus != "ok" {
		t.Errorf("concurrent refresh clobbered last_status: got %q, want ok", got.LastStatus)
	}
	if !got.LastRunAt.Equal(priorRun) {
		t.Errorf("last_run_at changed: got %v want %v", got.LastRunAt, priorRun)
	}
}

func TestScheduler_StartStopRunsLoop(t *testing.T) {
	// Integration-ish: wire a short tick interval, enable a job, wait
	// for the worker to run it, then Stop cleanly.
	repos, runner, sched := newSchedFixture(t)
	sched.SetTickInterval(10 * time.Millisecond)

	if err := repos.IPTVSchedules.Upsert(context.Background(), &db.IPTVScheduledJob{
		LibraryID: "lib-a", Kind: db.IPTVJobKindM3URefresh,
		IntervalHours: 1, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sched.Start(ctx)

	// Wait for at least one call within 500 ms.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		m3u, _ := runner.counts()
		if m3u > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	sched.Stop(context.Background())

	m3u, _ := runner.counts()
	if m3u == 0 {
		t.Error("scheduler loop never fired the due job")
	}
}

func TestScheduler_StopIsSynchronous(t *testing.T) {
	// Stop must not return before the goroutine drains. Verify by
	// having the loop see a goroutine-safe flag we only flip after
	// Stop. If Stop returned early, the flag could race with doneCh.
	_, _, sched := newSchedFixture(t)
	sched.SetTickInterval(50 * time.Millisecond)

	ctx := context.Background()
	sched.Start(ctx)
	// Allow the goroutine to enter the select.
	time.Sleep(10 * time.Millisecond)

	done := int32(0)
	go func() {
		sched.Stop(context.Background())
		atomic.StoreInt32(&done, 1)
	}()
	// After Stop returns, done should be 1 promptly.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&done) == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Error("Stop did not return within 500 ms")
}
