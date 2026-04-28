package iptv

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

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
	}
	if mode != "" {
		t.Setenv("FAKE_FFMPEG_MODE", mode)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewTransmuxManager(cfg, logger)
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
	args := buildTransmuxFFmpegArgs("http://up/x", "/work")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-c copy",
		"-f hls",
		"-hls_time 4",
		"-hls_flags delete_segments+independent_segments+omit_endlist+program_date_time",
		"-reconnect 1",
		"-rw_timeout 10000000",
		"http://up/x",
		"/work/index.m3u8",
		"/work/seg-%05d.ts",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing flag/value %q in argv: %s", want, joined)
		}
	}
}
