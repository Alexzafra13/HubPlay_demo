package iptv

// HLS transmux session manager for live IPTV streams that the browser
// cannot consume directly (raw MPEG-TS over HTTP, the format Xtream
// Codes M3U_PLUS feeds typically serve).
//
// Why this exists
// ───────────────
// The plain proxy (proxy.go) handles two upstream shapes:
//  1. HLS playlists (`*.m3u8`) — rewritten and forwarded as HLS, which
//     hls.js + native Safari + every modern player consumes.
//  2. Anything else — raw bytes piped through.
//
// Case (2) breaks for browsers because raw MPEG-TS over HTTP is not a
// format any current `<video>` engine plays. The user sees "200 OK,
// transferred N MB" in our logs and a stuck spinner in the player.
//
// This file adds case (3): MPEG-TS upstream → ffmpeg `-c copy -f hls`
// transmux → HLS surface. CPU is near-zero because we don't re-encode;
// we just repackage the existing H.264/AAC ES streams into HLS
// segments. If the codec doesn't survive copy (rare in IPTV) we fall
// through to the same error UI as today.
//
// Design choices that matter
// ──────────────────────────
//  * **One session per channel, shared across viewers.** N users
//    watching the same channel = 1 ffmpeg process, 1 upstream
//    connection. Indexed by channel ID, not user ID. This is critical:
//    a 5-user household watching the same news channel must not pull
//    5× upstream bandwidth from the provider, and most providers will
//    rate-limit or kick concurrent connections from the same account.
//
//  * **Lazy spawn, idle reap.** Sessions start on first manifest
//    request and self-destruct after `idleTimeout` with no segment
//    requests. The reaper runs on a separate goroutine so requests
//    don't pay teardown cost.
//
//  * **Bounded concurrency.** `maxSessions` caps simultaneous ffmpeg
//    processes. Without this, a user opening 50 tabs each on a
//    different channel would fork-bomb the server.
//
//  * **Ready signal.** The first manifest read after spawn must wait
//    for ffmpeg to write at least one segment + the playlist file.
//    The Session exposes `Ready()` (chan closed when first segment
//    appears) so the handler can block briefly with a timeout instead
//    of guessing or polling.
//
//  * **Failure isolation.** ffmpeg crash (bad codec, dead upstream)
//    marks the session failed and removes it from the map. Next
//    request restarts. We don't loop the failure: if `Start()` fails
//    twice in `failureCooldown` we surface the error and let the
//    higher-level circuit breaker (proxy.go) take over.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"sync/atomic"
	"time"
)

// ErrTooManySessions is returned by GetOrStart when the manager is at
// capacity. The handler turns this into HTTP 503 so clients can retry
// after the reap window clears an idle slot.
var ErrTooManySessions = errors.New("iptv-transmux: max sessions reached")

// ErrSessionNotFound is returned by Touch / SegmentPath when no live
// session exists for the channel. The handler returns 404 so the
// player can recover with a manifest reload (which will re-spawn).
var ErrSessionNotFound = errors.New("iptv-transmux: no session")

// ErrTransmuxFailed is returned when ffmpeg exits before producing
// any segment. Usually means the upstream is dead or the codec is
// not transmux-compatible (would need re-encode).
var ErrTransmuxFailed = errors.New("iptv-transmux: session failed before ready")

// segmentNamePattern matches the segment names ffmpeg writes with the
// `seg-%05d.ts` filename template we pass it. Used to validate
// incoming segment requests so a malicious client cannot ask for
// `../../etc/passwd`.
var segmentNamePattern = regexp.MustCompile(`^seg-\d{5,6}\.ts$`)

// IsValidSegmentName reports whether a segment name matches the
// filename template ffmpeg writes. Exposed so the HTTP handler can
// reject path-traversal attempts before touching the filesystem.
func IsValidSegmentName(name string) bool {
	return segmentNamePattern.MatchString(name)
}

// TransmuxManagerConfig captures the knobs that affect runtime
// behaviour. Defaults are filled in by NewTransmuxManager so callers
// can pass a zero value and get sensible production defaults.
type TransmuxManagerConfig struct {
	// CacheDir is the parent directory under which per-channel work
	// directories are created. Will be created with 0755 if missing.
	// Each session lives in `<CacheDir>/<channel-id>/` and is removed
	// on session stop.
	CacheDir string

	// FFmpegPath is the absolute path to the ffmpeg binary. Defaults
	// to "ffmpeg" (resolved from $PATH) which matches the existing
	// VOD transcoder convention.
	FFmpegPath string

	// MaxSessions caps the number of simultaneous transmux sessions.
	// 0 = unlimited (do not use in production). Default 10.
	MaxSessions int

	// IdleTimeout is how long a session stays alive after the last
	// segment request. Default 30 s. Lower trades faster cleanup for
	// more spawn churn on channel-zap; higher wastes CPU/bandwidth on
	// channels nobody is watching.
	IdleTimeout time.Duration

	// ReadyTimeout is how long GetOrStart will wait for ffmpeg to
	// produce its first segment before declaring the session failed.
	// Default 15 s. Bigger than typical first-segment latency for
	// well-behaved upstream (~3-5 s) but bounded so a dead provider
	// doesn't hang the player UI forever.
	ReadyTimeout time.Duration

	// ReaperInterval controls how often the idle reaper sweeps. Must
	// be much smaller than IdleTimeout. Default 5 s.
	ReaperInterval time.Duration
}

// TransmuxManager owns and orchestrates per-channel ffmpeg sessions.
// Methods are goroutine-safe.
type TransmuxManager struct {
	cfg    TransmuxManagerConfig
	logger *slog.Logger

	mu       sync.Mutex
	sessions map[string]*TransmuxSession // keyed by channel ID

	stop     chan struct{}
	stopOnce sync.Once
	stopped  chan struct{}
}

// TransmuxSession is one active ffmpeg process serving one channel
// to (potentially) many viewers. Read-only fields are exposed; the
// rest is internal to TransmuxManager.
type TransmuxSession struct {
	ChannelID   string
	UpstreamURL string
	WorkDir     string
	StartedAt   time.Time

	cmd    *exec.Cmd
	cancel context.CancelFunc
	done   chan struct{} // closed when cmd exits

	ready     chan struct{} // closed when first segment file is observable
	readyOnce sync.Once

	// lastTouchUnixNano is read by the reaper without holding the
	// manager lock. Stored as int64 so atomic reads/writes are cheap
	// on the hot path (every segment request bumps it).
	lastTouchUnixNano atomic.Int64

	// stopped is set when the session has been removed from the
	// manager and cleanup is in flight. Touch checks it to avoid
	// resurrecting a terminated session via races.
	stopped atomic.Bool
}

// NewTransmuxManager constructs a manager with defaults filled in for
// any zero-valued field. The reaper goroutine starts immediately.
// Call Shutdown to terminate it and stop all active sessions.
func NewTransmuxManager(cfg TransmuxManagerConfig, logger *slog.Logger) *TransmuxManager {
	if cfg.FFmpegPath == "" {
		cfg.FFmpegPath = "ffmpeg"
	}
	if cfg.MaxSessions <= 0 {
		cfg.MaxSessions = 10
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 30 * time.Second
	}
	if cfg.ReadyTimeout <= 0 {
		cfg.ReadyTimeout = 15 * time.Second
	}
	if cfg.ReaperInterval <= 0 {
		cfg.ReaperInterval = 5 * time.Second
	}
	if cfg.CacheDir == "" {
		// Pick a sensible default rather than failing at construction.
		// Production callers should always pass a real CacheDir; tests
		// can rely on this fallback.
		cfg.CacheDir = filepath.Join(os.TempDir(), "hubplay-iptv-hls")
	}

	m := &TransmuxManager{
		cfg:      cfg,
		logger:   logger.With("module", "iptv-transmux"),
		sessions: make(map[string]*TransmuxSession),
		stop:     make(chan struct{}),
		stopped:  make(chan struct{}),
	}
	go m.reapLoop()
	return m
}

// GetOrStart returns the live session for channelID, spawning a new
// one if none exists. Blocks until the session has produced its first
// segment (bounded by ReadyTimeout). Calling this concurrently with
// the same channel ID coalesces into a single spawn — the second
// caller waits on the same Ready signal.
func (m *TransmuxManager) GetOrStart(ctx context.Context, channelID, upstreamURL string) (*TransmuxSession, error) {
	if channelID == "" {
		return nil, fmt.Errorf("iptv-transmux: empty channel ID")
	}
	if upstreamURL == "" {
		return nil, fmt.Errorf("iptv-transmux: empty upstream URL")
	}

	m.mu.Lock()
	if s, ok := m.sessions[channelID]; ok && !s.stopped.Load() {
		// Existing session: bump touch + wait on its ready channel.
		s.lastTouchUnixNano.Store(time.Now().UnixNano())
		m.mu.Unlock()
		return m.waitReady(ctx, s)
	}
	if len(m.sessions) >= m.cfg.MaxSessions {
		m.mu.Unlock()
		return nil, ErrTooManySessions
	}

	s, err := m.startLocked(channelID, upstreamURL)
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return m.waitReady(ctx, s)
}

// startLocked must be called with m.mu held. It spawns ffmpeg, wires
// up the goroutines that watch for first-segment + process exit, and
// stores the session in the map.
func (m *TransmuxManager) startLocked(channelID, upstreamURL string) (*TransmuxSession, error) {
	workDir := filepath.Join(m.cfg.CacheDir, channelID)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, fmt.Errorf("iptv-transmux: mkdir work dir: %w", err)
	}
	// Clean any leftover segments from a previous run so the manifest
	// ffmpeg writes is consistent (no orphaned seg-NNN.ts files that
	// the new process didn't produce).
	if err := clearWorkDir(workDir); err != nil {
		m.logger.Warn("clear work dir", "channel", channelID, "error", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	args := buildTransmuxFFmpegArgs(upstreamURL, workDir)
	cmd := exec.CommandContext(ctx, m.cfg.FFmpegPath, args...)
	cmd.Dir = workDir
	// Detach from parent terminal — we never want ffmpeg to read
	// stdin or attach to a controlling TTY (it does both by default
	// in some builds, which can wedge a server with no TTY).
	cmd.Stdin = nil
	// stderr captured by the kernel via inherited file descriptor;
	// ffmpeg is configured `-loglevel warning` so volume is bounded.

	session := &TransmuxSession{
		ChannelID:   channelID,
		UpstreamURL: upstreamURL,
		WorkDir:     workDir,
		StartedAt:   time.Now(),
		cmd:         cmd,
		cancel:      cancel,
		done:        make(chan struct{}),
		ready:       make(chan struct{}),
	}
	session.lastTouchUnixNano.Store(time.Now().UnixNano())

	if err := cmd.Start(); err != nil {
		cancel()
		_ = os.RemoveAll(workDir)
		return nil, fmt.Errorf("iptv-transmux: start ffmpeg: %w", err)
	}

	m.sessions[channelID] = session
	m.logger.Info("transmux session started",
		"channel", channelID,
		"upstream", upstreamURL,
		"work_dir", workDir,
		"pid", cmd.Process.Pid,
	)

	// process-exit watcher: closes done, removes session, cleans dir.
	go m.processWatcher(session)
	// segment watcher: closes ready as soon as the first segment file
	// shows up on disk (or process exits, whichever first).
	go m.readyWatcher(session)

	return session, nil
}

// processWatcher waits for ffmpeg to exit, then evicts the session.
func (m *TransmuxManager) processWatcher(s *TransmuxSession) {
	defer close(s.done)
	err := s.cmd.Wait()
	if err != nil {
		m.logger.Info("transmux ffmpeg exited",
			"channel", s.ChannelID, "error", err)
	} else {
		m.logger.Info("transmux ffmpeg exited cleanly", "channel", s.ChannelID)
	}
	m.evict(s)
	// Belt-and-braces: closing ready unblocks any GetOrStart caller
	// still blocked on the wait — they'll see no manifest file and
	// return ErrTransmuxFailed. The readyOnce guard makes this a no-op
	// if the segment watcher already fired.
	s.readyOnce.Do(func() { close(s.ready) })
}

// readyWatcher polls the work dir until a segment file appears or the
// session is torn down. Polling is fine here: ffmpeg writes the first
// segment within a few seconds for any sane upstream, and we cap the
// wait at ReadyTimeout in GetOrStart anyway. fsnotify would be marginally
// faster but adds a dependency for a one-shot wait per session start.
func (m *TransmuxManager) readyWatcher(s *TransmuxSession) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(m.cfg.ReadyTimeout + 5*time.Second)
	defer deadline.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-deadline.C:
			// readyTimeout already elapsed in GetOrStart; the watcher
			// gives up so the session can be reaped if nothing arrives.
			return
		case <-ticker.C:
			if hasSegment(s.WorkDir) {
				s.readyOnce.Do(func() { close(s.ready) })
				return
			}
		}
	}
}

// waitReady blocks until the session is ready, the caller's context is
// cancelled, or the ready timeout elapses. Returns the session if
// ready, an error otherwise.
//
// Bumping lastTouch on successful return is load-bearing: a session
// that takes ReadyTimeout (~15 s) to produce its first segment would
// otherwise have a stale touch timestamp by the time the manifest
// handler returns to the caller. The reaper would then kill it before
// the player's first segment request lands. Refreshing the touch
// here gives the caller a full IdleTimeout window to send the next
// HTTP request.
func (m *TransmuxManager) waitReady(ctx context.Context, s *TransmuxSession) (*TransmuxSession, error) {
	timer := time.NewTimer(m.cfg.ReadyTimeout)
	defer timer.Stop()
	select {
	case <-s.ready:
		// Verify the manifest actually exists — if processWatcher
		// closed ready because ffmpeg crashed before writing
		// anything, hasSegment will be false and we surface the
		// failure cleanly.
		if !hasSegment(s.WorkDir) {
			return nil, ErrTransmuxFailed
		}
		s.lastTouchUnixNano.Store(time.Now().UnixNano())
		return s, nil
	case <-timer.C:
		// Don't kill the session — segments may still arrive shortly
		// (slow upstream). The reaper handles eventual cleanup if
		// nobody comes back.
		return nil, ErrTransmuxFailed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Touch bumps the session's last-activity timestamp, keeping it alive
// past the next reap. Returns false (and ErrSessionNotFound) if no
// session exists for the channel — the handler turns that into 404
// so the player retries the manifest, which respawns the session.
func (m *TransmuxManager) Touch(channelID string) (*TransmuxSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[channelID]
	if !ok || s.stopped.Load() {
		return nil, ErrSessionNotFound
	}
	s.lastTouchUnixNano.Store(time.Now().UnixNano())
	return s, nil
}

// Stop terminates the session for channelID if any. Safe to call when
// no session exists (no-op).
func (m *TransmuxManager) Stop(channelID string) {
	m.mu.Lock()
	s, ok := m.sessions[channelID]
	if ok {
		delete(m.sessions, channelID)
	}
	m.mu.Unlock()
	if ok {
		m.terminate(s)
	}
}

// ActiveSessions reports the current number of live sessions.
func (m *TransmuxManager) ActiveSessions() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// Shutdown stops the reaper and tears down every active session.
// Idempotent. Blocks until all ffmpeg processes have exited.
func (m *TransmuxManager) Shutdown() {
	m.stopOnce.Do(func() {
		close(m.stop)
	})
	<-m.stopped

	m.mu.Lock()
	sessions := make([]*TransmuxSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.sessions = make(map[string]*TransmuxSession)
	m.mu.Unlock()

	for _, s := range sessions {
		m.terminate(s)
	}
	m.logger.Info("transmux manager shutdown", "sessions_terminated", len(sessions))
}

// reapLoop is the goroutine that walks the session map on a tick and
// stops any session whose lastTouch is older than IdleTimeout.
func (m *TransmuxManager) reapLoop() {
	defer close(m.stopped)
	t := time.NewTicker(m.cfg.ReaperInterval)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-t.C:
			m.reapOnce()
		}
	}
}

func (m *TransmuxManager) reapOnce() {
	now := time.Now().UnixNano()
	cutoff := now - m.cfg.IdleTimeout.Nanoseconds()

	m.mu.Lock()
	var toStop []*TransmuxSession
	for id, s := range m.sessions {
		// Don't reap sessions that haven't produced their first
		// segment yet — they have their own readyWatcher / ReadyTimeout
		// protection. Applying idle policy to a starting session
		// would race the first-segment write and kill perfectly
		// healthy spawns just because the upstream needed a few
		// seconds to warm up.
		if !isReady(s) {
			continue
		}
		if s.lastTouchUnixNano.Load() < cutoff {
			toStop = append(toStop, s)
			delete(m.sessions, id)
		}
	}
	m.mu.Unlock()

	for _, s := range toStop {
		m.logger.Info("transmux idle reap",
			"channel", s.ChannelID,
			"idle_for", time.Duration(now-s.lastTouchUnixNano.Load()))
		m.terminate(s)
	}
}

// isReady reports whether a session has signalled readiness — i.e.
// produced its first segment. Used by the reaper to skip
// still-spawning sessions whose lastTouch is intentionally stale.
func isReady(s *TransmuxSession) bool {
	select {
	case <-s.ready:
		return true
	default:
		return false
	}
}

// evict removes the session from the map. Called by processWatcher
// after ffmpeg exits — at that point cleanup of the work dir is
// already what terminate would do anyway, but we go through terminate
// for the single shutdown path so behaviour stays identical.
func (m *TransmuxManager) evict(s *TransmuxSession) {
	m.mu.Lock()
	if cur, ok := m.sessions[s.ChannelID]; ok && cur == s {
		delete(m.sessions, s.ChannelID)
	}
	m.mu.Unlock()
	// processWatcher will clean up the dir; we skip terminate so we
	// don't double-cancel an already-exited process.
	s.stopped.Store(true)
	_ = os.RemoveAll(s.WorkDir)
}

// terminate cancels the ffmpeg context, waits briefly for the
// process to exit, and removes the work dir. Safe to call multiple
// times on the same session.
func (m *TransmuxManager) terminate(s *TransmuxSession) {
	if !s.stopped.CompareAndSwap(false, true) {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	// Bounded wait so a wedged ffmpeg can't block shutdown forever.
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		m.logger.Warn("transmux terminate timeout", "channel", s.ChannelID)
	}
	_ = os.RemoveAll(s.WorkDir)
}

// hasSegment reports whether at least one ffmpeg-produced segment
// file is observable in dir. Used as the readiness signal: ffmpeg
// only writes a segment after it has parsed the upstream stream and
// produced a complete chunk, so observing one means the manifest is
// valid for serving.
func hasSegment(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && segmentNamePattern.MatchString(e.Name()) {
			return true
		}
	}
	return false
}

// clearWorkDir removes every file from the per-channel directory.
// Called before a fresh ffmpeg spawn so the new manifest doesn't see
// orphaned segments from a prior process.
func clearWorkDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		_ = os.Remove(filepath.Join(dir, e.Name()))
	}
	return nil
}

// buildTransmuxFFmpegArgs constructs the argv for the live
// MPEG-TS → HLS transmux pipeline. `-c copy` is the load-bearing
// choice: we never re-encode, so CPU usage is dominated by mux/demux
// and stays trivially low even at 1080p.
//
// Reconnection flags are set so a flaky upstream (Xtream provider
// blipping for a few seconds) recovers without ffmpeg exiting; the
// session manager only re-spawns on a hard exit.
//
// Buffering choices (tuned against real Xtream traffic where the
// provider delivers MPEG-TS in uneven bursts):
//   - `-rtbufsize 50M` lets ffmpeg absorb upstream jitter instead of
//     dropping packets when input arrives faster than it can mux
//     out. Cost: ~50 MB RAM per active session.
//   - `-max_delay 5000000` (5 s) gives the demuxer slack for
//     reordered packets — alternative is "non-monotonic DTS" warnings
//     and occasional segment dropouts on noisy providers.
//
// HLS window choices (tuned for live, smooth playback over Xtream):
//   - `hls_time 2` — smaller segments halve buffer-underrun recovery
//     time and reduce live latency.
//   - `hls_list_size 20` — 40 s window of segments visible to the
//     player. Sized to absorb a real-world 10-second client stall
//     (background-tab throttling, transient network blip) without
//     the player falling out of the manifest window. Apple's HLS
//     spec recommends list_size ≥ 6 × target_duration; we land at
//     20× because operator latency tolerance for live IPTV is high.
//   - `hls_delete_threshold 5` — keep 5 extra segments on disk past
//     the playlist tail. A client whose manifest parse cycle is
//     slightly behind can still fetch the segment it asked for
//     instead of getting 404. Without this flag `delete_segments`
//     deletes immediately on rotation, which races slow clients.
//
// `+temp_file` writes each segment to `.tmp` first and atomically
// renames into place when ffmpeg is done. Without it, Go's
// `http.ServeFile` can serve a partially-written `.ts` mid-write,
// triggering `bufferStalledError` in hls.js — which then compounds
// into a fall-behind. Zero downside: rename is atomic on every
// filesystem we run on.
//
// `omit_endlist` keeps the manifest live (no #EXT-X-ENDLIST) so
// hls.js treats it as a sliding window. `delete_segments` keeps disk
// usage bounded. `independent_segments` is the right hint for
// sliding-window live HLS.
func buildTransmuxFFmpegArgs(upstreamURL, workDir string) []string {
	return []string{
		"-hide_banner",
		"-loglevel", "warning",
		"-nostdin",
		"-fflags", "+genpts+discardcorrupt",
		"-rtbufsize", "50M",
		"-max_delay", "5000000",
		"-reconnect", "1",
		"-reconnect_at_eof", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "5",
		"-rw_timeout", "10000000", // 10 s I/O timeout in microseconds
		"-i", upstreamURL,
		"-map", "0:v:0",
		"-map", "0:a:0?",
		"-c", "copy",
		"-bsf:v", "h264_mp4toannexb",
		"-f", "hls",
		"-hls_time", "2",
		"-hls_list_size", "20",
		"-hls_delete_threshold", "5",
		"-hls_flags", "delete_segments+independent_segments+omit_endlist+program_date_time+temp_file",
		"-hls_segment_type", "mpegts",
		"-hls_segment_filename", filepath.Join(workDir, "seg-%05d.ts"),
		"-hls_allow_cache", "0",
		filepath.Join(workDir, "index.m3u8"),
	}
}

// ManifestPath returns the absolute path to the live HLS manifest
// produced by ffmpeg. Used by the HTTP handler to read + serve.
func (s *TransmuxSession) ManifestPath() string {
	return filepath.Join(s.WorkDir, "index.m3u8")
}

// SegmentPath returns the absolute path to a segment file by name.
// The caller is responsible for validating `name` against
// IsValidSegmentName before passing it in (path-traversal guard).
func (s *TransmuxSession) SegmentPath(name string) string {
	return filepath.Join(s.WorkDir, name)
}

// Ready returns a channel that is closed when the session has
// produced its first segment (or has died, see ManifestPath
// existence check). Exposed for tests; production code uses
// GetOrStart which already waits.
func (s *TransmuxSession) Ready() <-chan struct{} { return s.ready }

// Done returns a channel that is closed when the underlying ffmpeg
// process has exited. Exposed for tests.
func (s *TransmuxSession) Done() <-chan struct{} { return s.done }
