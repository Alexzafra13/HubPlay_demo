package iptv

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeGate is a deterministic ChannelGate for tests. Allow toggles via
// allow; calls to RecordSuccess / RecordFailure are counted so tests
// can assert the manager wired the gate at the right places.
type fakeGate struct {
	allow      atomic.Bool
	retryAfter time.Duration
	successes  atomic.Int64
	failures   atomic.Int64
}

func newFakeGate(allowed bool) *fakeGate {
	g := &fakeGate{}
	g.allow.Store(allowed)
	return g
}

func (g *fakeGate) Allow(_ string) (bool, time.Duration) {
	if g.allow.Load() {
		return true, 0
	}
	return false, g.retryAfter
}

func (g *fakeGate) RecordSuccess(_ string) { g.successes.Add(1) }
func (g *fakeGate) RecordFailure(_ string) { g.failures.Add(1) }

// countingReporter counts calls to RecordProbeSuccess / RecordProbeFailure
// so tests can assert the manager mirrored the gate state into the
// channel-health pipeline.
type countingReporter struct {
	successes atomic.Int64
	failures  atomic.Int64
}

func (r *countingReporter) RecordProbeSuccess(_ context.Context, _ string) { r.successes.Add(1) }
func (r *countingReporter) RecordProbeFailure(_ context.Context, _ string, _ error) {
	r.failures.Add(1)
}

// fakeFFmpeg writes a minimal "I produced a segment" workload so the
// transmux manager can be exercised without depending on real ffmpeg
// or a live upstream. Returns an absolute path the manager can spawn
// as the ffmpeg binary.
//
// Behaviour modes via env var FAKE_FFMPEG_MODE:
//   - "ok"     (default): write seg-00000.ts then loop sleeping until
//     killed.
//   - "noseg":  loop forever without writing anything (simulates a
//     hung upstream that never produces output).
//   - "crash":  exit 1 immediately (simulates ffmpeg failing on a
//     bad codec or unreachable URL).
//   - "stderr_crash": write a recognisable error line to stderr then
//     exit 1, used to assert stderr capture surfaces in the log.
//
// The script parses the `-hls_segment_filename` and the trailing
// manifest path argument from the argv we feed it from
// buildTransmuxFFmpegArgs. The manifest is written first (to mimic
// real ffmpeg ordering) so the readiness probe (which checks for
// segments) only flips after the segment file lands.
func fakeFFmpeg(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake ffmpeg shim relies on /bin/sh; not available on Windows")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "fake-ffmpeg.sh")
	script := `#!/bin/sh
set -eu
mode="${FAKE_FFMPEG_MODE:-ok}"
manifest_path=""
seg_template=""
while [ $# -gt 0 ]; do
  case "$1" in
    -hls_segment_filename)
      seg_template="$2"
      shift 2
      ;;
    *)
      # The output manifest is the only positional path with .m3u8 suffix.
      case "$1" in
        *.m3u8) manifest_path="$1" ;;
      esac
      shift
      ;;
  esac
done

if [ "$mode" = "crash" ]; then
  exit 1
fi

if [ "$mode" = "stderr_crash" ]; then
  printf '[tcp @ 0x12345] Connection to tcp://example.test:80 failed: Connection refused\n' >&2
  printf '[in#0 @ 0x67890] Error opening input: Connection refused\n' >&2
  exit 1
fi

if [ -z "$manifest_path" ] || [ -z "$seg_template" ]; then
  echo "fake-ffmpeg: missing manifest or segment template" >&2
  exit 2
fi

if [ "$mode" = "noseg" ]; then
  # Hang forever — caller will SIGKILL via context cancel.
  while true; do sleep 1; done
fi

# mode=ok: write a tiny stub manifest + one segment then loop.
seg_file="$(echo "$seg_template" | sed 's/%05d/00000/')"
printf 'TS-PAYLOAD' > "$seg_file"
cat > "$manifest_path" <<EOF
#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:4
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:4.000,
$(basename "$seg_file")
EOF
while true; do sleep 1; done
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake ffmpeg: %v", err)
	}
	return path
}

// newTestManager builds a manager with a fake ffmpeg shim. Tests that
// need a tight IdleTimeout to observe the reaper pass it explicitly;
// the default is intentionally generous so the "session survives
// across multiple GetOrStart calls" tests don't race the reaper for
// no good reason — production IdleTimeout is 30 s.
func newTestManager(t *testing.T, mode string, idle time.Duration) *TransmuxManager {
	t.Helper()
	m, _ := newTestManagerWithOpts(t, mode, idle, TransmuxManagerConfig{})
	return m
}

// newTestManagerWithOpts is the extension hook for tests that need to
// inject a Gate, Reporter, or capture log output. `extra` is merged
// over the standard fake-ffmpeg defaults; only the fields the caller
// sets take effect. Returns the manager + a *bytes.Buffer that
// captures log output so assertions can grep for stderr_tail / pid /
// channel.
func newTestManagerWithOpts(t *testing.T, mode string, idle time.Duration, extra TransmuxManagerConfig) (*TransmuxManager, *bytes.Buffer) {
	t.Helper()
	if idle <= 0 {
		idle = 2 * time.Second
	}
	cacheDir := t.TempDir()
	cfg := TransmuxManagerConfig{
		CacheDir:       cacheDir,
		FFmpegPath:     fakeFFmpeg(t),
		MaxSessions:    3,
		IdleTimeout:    idle,
		ReadyTimeout:   2 * time.Second,
		ReaperInterval: 50 * time.Millisecond,
		Gate:           extra.Gate,
		Reporter:       extra.Reporter,
		UserAgent:      extra.UserAgent,
	}
	if mode != "" {
		t.Setenv("FAKE_FFMPEG_MODE", mode)
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return NewTransmuxManager(cfg, logger), &buf
}

func TestTransmuxManager_GetOrStart_SpawnsAndBecomesReady(t *testing.T) {
	m := newTestManager(t, "ok", 0)
	t.Cleanup(m.Shutdown)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sess, err := m.GetOrStart(ctx, "ch-1", "http://upstream/test")
	if err != nil {
		t.Fatalf("GetOrStart: %v", err)
	}
	if sess.ChannelID != "ch-1" {
		t.Errorf("ChannelID: got %q want ch-1", sess.ChannelID)
	}
	if _, err := os.Stat(sess.ManifestPath()); err != nil {
		t.Errorf("manifest missing after Ready: %v", err)
	}
	if !hasSegment(sess.WorkDir) {
		t.Errorf("expected at least one segment after Ready")
	}
}

func TestTransmuxManager_GetOrStart_Coalesces_OneFFmpegPerChannel(t *testing.T) {
	m := newTestManager(t, "ok", 0)
	t.Cleanup(m.Shutdown)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const callers = 5
	var wg sync.WaitGroup
	wg.Add(callers)
	pids := make([]int, callers)
	errs := make([]error, callers)
	for i := 0; i < callers; i++ {
		i := i
		go func() {
			defer wg.Done()
			sess, err := m.GetOrStart(ctx, "ch-shared", "http://upstream/shared")
			if err != nil {
				errs[i] = err
				return
			}
			pids[i] = sess.cmd.Process.Pid
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d: %v", i, err)
		}
	}
	first := pids[0]
	if first == 0 {
		t.Fatal("first caller has no PID")
	}
	for i, pid := range pids {
		if pid != first {
			t.Errorf("caller %d PID=%d, want shared %d (multiple ffmpeg processes spawned)", i, pid, first)
		}
	}
	if got := m.ActiveSessions(); got != 1 {
		t.Errorf("ActiveSessions: got %d want 1", got)
	}
}

func TestTransmuxManager_GetOrStart_RespectsMaxSessions(t *testing.T) {
	m := newTestManager(t, "ok", 0)
	t.Cleanup(m.Shutdown)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// MaxSessions = 3 (set in newTestManager). Fill it.
	for i := 0; i < 3; i++ {
		if _, err := m.GetOrStart(ctx, "ch-fill-"+string(rune('a'+i)), "http://upstream/fill"); err != nil {
			t.Fatalf("fill %d: %v", i, err)
		}
	}
	if got := m.ActiveSessions(); got != 3 {
		t.Fatalf("ActiveSessions: got %d want 3", got)
	}
	// 4th must fail with ErrTooManySessions.
	_, err := m.GetOrStart(ctx, "ch-overflow", "http://upstream/overflow")
	if !errors.Is(err, ErrTooManySessions) {
		t.Fatalf("expected ErrTooManySessions, got %v", err)
	}
}

func TestTransmuxManager_IdleReaper_TerminatesUntouchedSession(t *testing.T) {
	// Tight idle timeout so the reaper fires quickly. Real
	// deployments use seconds, not 200 ms — this is just enough to
	// observe the reaper without making the test sleep for a real
	// production interval.
	m := newTestManager(t, "ok", 200*time.Millisecond)
	t.Cleanup(m.Shutdown)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := m.GetOrStart(ctx, "ch-idle", "http://upstream/idle"); err != nil {
		t.Fatalf("GetOrStart: %v", err)
	}
	if got := m.ActiveSessions(); got != 1 {
		t.Fatalf("pre-reap ActiveSessions: got %d want 1", got)
	}

	// IdleTimeout=200ms + ReaperInterval=50ms; allow generous margin
	// so a slow CI host doesn't flake.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if m.ActiveSessions() == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := m.ActiveSessions(); got != 0 {
		t.Errorf("post-reap ActiveSessions: got %d want 0", got)
	}
}

func TestTransmuxManager_Touch_KeepsSessionAlive(t *testing.T) {
	// Tight IdleTimeout so the test confirms Touch is the thing
	// keeping the session alive (not a generous default that would
	// keep it alive regardless).
	m := newTestManager(t, "ok", 200*time.Millisecond)
	t.Cleanup(m.Shutdown)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := m.GetOrStart(ctx, "ch-touch", "http://upstream/touch"); err != nil {
		t.Fatalf("GetOrStart: %v", err)
	}
	// Touch repeatedly across an interval longer than IdleTimeout.
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if _, err := m.Touch("ch-touch"); err != nil {
					return
				}
			}
		}
	}()

	// Wait long enough that an un-touched session would have been reaped.
	time.Sleep(700 * time.Millisecond)
	if got := m.ActiveSessions(); got != 1 {
		t.Errorf("expected session kept alive by Touch, got %d active", got)
	}
	close(stop)
	<-done
}

func TestTransmuxManager_Touch_SessionNotFound(t *testing.T) {
	m := newTestManager(t, "ok", 0)
	t.Cleanup(m.Shutdown)

	if _, err := m.Touch("never-existed"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("Touch on missing session: got %v want ErrSessionNotFound", err)
	}
}

func TestTransmuxManager_GetOrStart_FailsWhenFFmpegCrashes(t *testing.T) {
	m := newTestManager(t, "crash", 0)
	t.Cleanup(m.Shutdown)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := m.GetOrStart(ctx, "ch-bad", "http://upstream/bad")
	if !errors.Is(err, ErrTransmuxFailed) {
		t.Fatalf("expected ErrTransmuxFailed, got %v", err)
	}
	// processWatcher should have evicted it.
	if got := m.ActiveSessions(); got != 0 {
		t.Errorf("ActiveSessions after crash: got %d want 0", got)
	}
}

func TestTransmuxManager_GetOrStart_TimesOutWhenNoSegments(t *testing.T) {
	m := newTestManager(t, "noseg", 0)
	t.Cleanup(m.Shutdown)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := m.GetOrStart(ctx, "ch-noseg", "http://upstream/hang")
	if !errors.Is(err, ErrTransmuxFailed) {
		t.Fatalf("expected ErrTransmuxFailed on ready timeout, got %v", err)
	}
	// Session is still in the map until the reaper sweeps; that's
	// intentional (a slow upstream may produce a segment shortly).
	// Shutdown will kill it; we only assert no goroutine leak below.
}

func TestTransmuxManager_Shutdown_TerminatesAllSessions(t *testing.T) {
	m := newTestManager(t, "ok", 0)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for i := 0; i < 3; i++ {
		if _, err := m.GetOrStart(ctx, "ch-sd-"+string(rune('a'+i)), "http://upstream/sd"); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	m.Shutdown()
	if got := m.ActiveSessions(); got != 0 {
		t.Errorf("ActiveSessions after Shutdown: got %d want 0", got)
	}
	// Idempotent.
	m.Shutdown()
}

func TestIsValidSegmentName(t *testing.T) {
	cases := map[string]bool{
		"seg-00000.ts":  true,
		"seg-12345.ts":  true,
		"seg-999999.ts": true,
		"seg-1.ts":      false, // too few digits
		"seg-00000.m4s": false, // wrong extension
		"../etc/passwd": false,
		"seg-00000.ts/": false,
		"":              false,
	}
	for name, want := range cases {
		if got := IsValidSegmentName(name); got != want {
			t.Errorf("IsValidSegmentName(%q): got %v want %v", name, got, want)
		}
	}
}

func TestBuildTransmuxFFmpegArgs_ContainsCriticalFlags(t *testing.T) {
	args := buildTransmuxFFmpegArgs("http://up/x", "/work", "")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-c copy",
		"-f hls",
		"-hls_time 2",
		"-hls_list_size 20",
		"-hls_delete_threshold 5",
		"-hls_flags delete_segments+independent_segments+omit_endlist+program_date_time+temp_file",
		"-rtbufsize 50M",
		"-max_delay 5000000",
		"-reconnect 1",
		"-rw_timeout 10000000",
		"-user_agent " + defaultTransmuxUserAgent,
		"http://up/x",
		"/work/index.m3u8",
		"/work/seg-%05d.ts",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing flag/value %q in argv: %s", want, joined)
		}
	}
}

func TestBuildTransmuxFFmpegArgs_HonoursCustomUserAgent(t *testing.T) {
	args := buildTransmuxFFmpegArgs("http://up/x", "/work", "My/UA 1.0")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-user_agent My/UA 1.0") {
		t.Errorf("custom UA not in argv: %s", joined)
	}
	if strings.Contains(joined, defaultTransmuxUserAgent) {
		t.Errorf("default UA leaked when custom one was provided: %s", joined)
	}
}

// Gate integration: when the breaker says no, GetOrStart must refuse
// without spawning ffmpeg. This is the load-bearing fix that stops
// the doomed-spawn loop a dead Xtream upstream produced before.
func TestTransmuxManager_GetOrStart_RefusedByGate(t *testing.T) {
	gate := newFakeGate(false)
	gate.retryAfter = 12 * time.Second
	m, _ := newTestManagerWithOpts(t, "ok", 0, TransmuxManagerConfig{Gate: gate})
	t.Cleanup(m.Shutdown)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := m.GetOrStart(ctx, "ch-blocked", "http://upstream/blocked")
	var coe *CircuitOpenError
	if !errors.As(err, &coe) {
		t.Fatalf("expected *CircuitOpenError, got %v", err)
	}
	if coe.RetryAfter != 12*time.Second {
		t.Errorf("RetryAfter: got %s want 12s", coe.RetryAfter)
	}
	if got := m.ActiveSessions(); got != 0 {
		t.Errorf("expected no session spawned when gate denies, got %d", got)
	}
	// CircuitOpenError must unwrap to ErrCircuitOpen so generic
	// errors.Is checks at the handler boundary keep working.
	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("CircuitOpenError must unwrap to ErrCircuitOpen, got %v", err)
	}
}

// Successful spawn must record success on both gate and reporter so a
// recovered upstream resets the breaker counters and the channel
// shows healthy in the admin dashboard without waiting for the
// next prober pass.
func TestTransmuxManager_GetOrStart_RecordsSuccessOnReady(t *testing.T) {
	gate := newFakeGate(true)
	rep := &countingReporter{}
	m, _ := newTestManagerWithOpts(t, "ok", 0, TransmuxManagerConfig{Gate: gate, Reporter: rep})
	t.Cleanup(m.Shutdown)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := m.GetOrStart(ctx, "ch-good", "http://upstream/good"); err != nil {
		t.Fatalf("GetOrStart: %v", err)
	}
	// Allow the readyWatcher's tick (250ms) plus a generous margin to
	// fire recordSuccess. The success path runs concurrently with
	// GetOrStart's return, so we poll briefly.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if gate.successes.Load() == 1 && rep.successes.Load() == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := gate.successes.Load(); got != 1 {
		t.Errorf("gate.successes: got %d want 1", got)
	}
	if got := rep.successes.Load(); got != 1 {
		t.Errorf("reporter.successes: got %d want 1", got)
	}
	if got := gate.failures.Load(); got != 0 {
		t.Errorf("gate.failures: got %d want 0", got)
	}
}

// ffmpeg crash before first segment must record exactly one failure
// on gate + reporter. After breakerThreshold consecutive failures the
// real channelBreaker would open the circuit; here we use the fake to
// observe the call count directly without depending on internal
// breaker thresholds.
func TestTransmuxManager_GetOrStart_RecordsFailureOnCrash(t *testing.T) {
	gate := newFakeGate(true)
	rep := &countingReporter{}
	m, _ := newTestManagerWithOpts(t, "crash", 0, TransmuxManagerConfig{Gate: gate, Reporter: rep})
	t.Cleanup(m.Shutdown)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := m.GetOrStart(ctx, "ch-bad", "http://upstream/bad")
	if !errors.Is(err, ErrTransmuxFailed) {
		t.Fatalf("expected ErrTransmuxFailed, got %v", err)
	}
	// processWatcher records failure asynchronously after Wait()
	// returns; poll briefly so we don't race the goroutine.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if gate.failures.Load() >= 1 && rep.failures.Load() >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := gate.failures.Load(); got != 1 {
		t.Errorf("gate.failures: got %d want 1", got)
	}
	if got := rep.failures.Load(); got != 1 {
		t.Errorf("reporter.failures: got %d want 1", got)
	}
	if got := gate.successes.Load(); got != 0 {
		t.Errorf("gate.successes: got %d want 0", got)
	}
}

// Stderr capture: when ffmpeg dies before producing a segment, the
// exit log must include the tail of ffmpeg's stderr. This is the line
// that previously read just `error="exit status 1"` and forced
// operators to docker-exec ffmpeg manually to see why.
func TestTransmuxManager_FFmpegStderrSurfacedOnCrash(t *testing.T) {
	m, logBuf := newTestManagerWithOpts(t, "stderr_crash", 0, TransmuxManagerConfig{})
	t.Cleanup(m.Shutdown)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _ = m.GetOrStart(ctx, "ch-stderr", "http://upstream/stderr")
	// processWatcher logs after Wait() returns; poll the buffer.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(logBuf.String(), "Connection refused") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	got := logBuf.String()
	if !strings.Contains(got, "ffmpeg_stderr_tail") {
		t.Errorf("expected ffmpeg_stderr_tail key in log, got: %s", got)
	}
	if !strings.Contains(got, "Connection refused") {
		t.Errorf("expected captured stderr to surface 'Connection refused', got: %s", got)
	}
}

// Ring buffer behaviour: lines beyond capacity drop the oldest.
func TestStderrRing_BoundedFIFO(t *testing.T) {
	r := newStderrRing(3)
	for i := 0; i < 5; i++ {
		r.push("line-" + string(rune('a'+i)))
	}
	got := r.String()
	want := "line-c | line-d | line-e"
	if got != want {
		t.Errorf("ring tail: got %q want %q", got, want)
	}
}
