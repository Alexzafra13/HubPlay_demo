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
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ErrTooManySessions is returned by GetOrStart when the manager is at
// capacity. The handler turns this into HTTP 503 so clients can retry
// after the reap window clears an idle slot.
var ErrTooManySessions = errors.New("iptv-transmux: max sessions reached")

// ErrTooManyReencodeSessions is returned when the per-mode cap on
// reencode sessions is full but the global cap still has room.
// Distinct from ErrTooManySessions so the handler can map it to the
// same 503 + Retry-After while the metrics layer counts it
// separately ("reencode_busy" outcome).
var ErrTooManyReencodeSessions = errors.New("iptv-transmux: max reencode sessions reached")

// ErrSessionNotFound is returned by Touch / SegmentPath when no live
// session exists for the channel. The handler returns 404 so the
// player can recover with a manifest reload (which will re-spawn).
var ErrSessionNotFound = errors.New("iptv-transmux: no session")

// ErrTransmuxFailed is returned when ffmpeg exits before producing
// any segment. Usually means the upstream is dead or the codec is
// not transmux-compatible (would need re-encode).
var ErrTransmuxFailed = errors.New("iptv-transmux: session failed before ready")

// ChannelGate is the per-channel circuit-breaker contract shared by
// the stream proxy and the transmux session manager. Both planes
// punch the same gate so a series of failures on either path triggers
// the cooldown for all subsequent attempts on either path.
//
// Implementations must be goroutine-safe and cheap to call; Allow is
// invoked on every viewer attempt and Record* on every outcome. The
// concrete implementation lives in circuit_breaker.go (channelBreaker);
// the interface is exposed here so transmux can take it without
// importing the proxy struct.
type ChannelGate interface {
	Allow(channelID string) (bool, time.Duration)
	RecordSuccess(channelID string)
	RecordFailure(channelID string)
}

// defaultTransmuxUserAgent is the UA we send upstream when the caller
// doesn't override it. Many Xtream Codes panels gate on UA and serve
// either an HTML error page or a different codec to the default
// `Lavf/<version>` ffmpeg sends, which manifests downstream as the
// dreaded "Invalid data found when processing input" / exit status 8.
// Mirroring the prober's UA keeps both planes consistent.
const defaultTransmuxUserAgent = "VLC/3.0.20 LibVLC/3.0.20"

// ffmpegStderrTailLines is how many of ffmpeg's stderr lines we keep
// in memory per session. Sized to capture the cluster of warnings +
// the actual fatal error line that ffmpeg prints right before exiting
// (typically <20 lines on a real failure) without growing unbounded
// for sessions that log warnings continuously over hours.
const ffmpegStderrTailLines = 64

// decodeMode describes how ffmpeg should turn the upstream MPEG-TS into
// the HLS sliding window we serve. Most sane Xtream feeds work with
// `direct` (`-c copy`), which is near-zero CPU. A subset (HEVC main10,
// AC3 audio in some PMTs, codecs that don't survive the
// `h264_mp4toannexb` bitstream filter) crash ffmpeg with cryptic
// "Invalid data" / "codec not currently supported" errors before the
// first segment, even when the upstream is reachable. For those
// channels we transparently fall back to `reencode` on the next
// attempt, swallowing the CPU hit so the user sees video instead of
// a 502.
type decodeMode int

const (
	decodeModeDirect decodeMode = iota
	decodeModeReencode
)

func (m decodeMode) String() string {
	if m == decodeModeReencode {
		return "reencode"
	}
	return "direct"
}

// decodeModeFallbackTTL is how long a channel stays pinned in
// `reencode` mode after an auto-promotion. After the window the
// manager retries `direct` so a recovered upstream (codec change,
// provider fix, …) reverts to the cheap path automatically.
const decodeModeFallbackTTL = 1 * time.Hour

// TransmuxMetrics is the optional metrics sink that lets the
// observability package count starts by outcome and decode mode
// without pulling Prometheus into this package. Nil-tolerant at all
// call sites.
type TransmuxMetrics interface {
	IncStarts(outcome string)        // ok, crash, gate_denied, busy
	IncDecodeMode(mode string)       // direct, reencode
	IncReencodePromotions()          // sticky flips after a copy-mode crash
}

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

	// UserAgent is sent to upstream by ffmpeg's HTTP demuxer. Empty
	// means use defaultTransmuxUserAgent. Override only when a
	// specific provider needs a different fingerprint.
	UserAgent string

	// Gate is the optional per-channel circuit breaker. When set, the
	// manager refuses GetOrStart with a wrapped CircuitOpenError if
	// Allow returns false, and records success/failure of each spawn
	// so the breaker state stays in sync with the proxy plane.
	Gate ChannelGate

	// Reporter is the optional channel-health sink. Mirrors what the
	// stream proxy does: success when the session reaches first
	// segment, failure when ffmpeg exits before producing one. Lets a
	// transmux-only failure show up as "unhealthy" in the admin UI
	// without the prober having to discover it on its next pass.
	Reporter ChannelHealthReporter

	// Metrics is the optional Prometheus sink. When set, the manager
	// counts spawn outcomes and decode-mode picks so dashboards can
	// show "channel X has been re-encoding for 3 hours" without
	// scraping the DB. Nil-safe.
	Metrics TransmuxMetrics

	// EnableReencodeFallback toggles the auto-promotion to reencode
	// on a copy-mode crash. Default true; settable to false for
	// deployments running on a CPU-starved box where the user prefers
	// to see "channel offline" rather than risk a dozen 720p re-encode
	// pipelines suddenly running. Off-by-default would surprise users
	// who came in expecting Plex/Jellyfin parity.
	EnableReencodeFallback *bool

	// MaxReencodeSessions caps how many sessions can run in
	// `reencode` mode at once, on top of the global MaxSessions
	// total. Reencode is the only path that costs real CPU (or GPU
	// queue time on hwaccel hosts), so capping it separately lets
	// operators bound the worst case without throttling the
	// always-cheap `direct` path. Default = MaxSessions/2 — generous
	// enough that a normal household doesn't notice, tight enough to
	// keep a runaway codec-crash storm from consuming every encoder
	// slot.
	MaxReencodeSessions int

	// ReencodeEncoder is the ffmpeg video encoder name used by the
	// re-encode fallback ("libx264", "h264_nvenc", "h264_vaapi",
	// "h264_qsv", "h264_videotoolbox"). Empty defaults to libx264.
	// main.go normally fills this from stream.Manager.HWAccelInfo()
	// so the IPTV transmux uses the same hardware encoder the VOD
	// transcoder picked.
	ReencodeEncoder string

	// ReencodeHWAccelInputArgs are the `-hwaccel ...` ffmpeg flags
	// that go before `-i` so the decoder runs on the same accelerator
	// as the encoder. Generated by stream.HWAccelInputArgs at boot;
	// nil means software decode.
	ReencodeHWAccelInputArgs []string
}

// TransmuxManager owns and orchestrates per-channel ffmpeg sessions.
// Methods are goroutine-safe.
type TransmuxManager struct {
	cfg    TransmuxManagerConfig
	logger *slog.Logger

	mu       sync.Mutex
	sessions map[string]*TransmuxSession // keyed by channel ID

	// decodeMode caches per-channel auto-promotion to reencode after
	// a copy-mode failure. Read on every spawn, written on every
	// failure-before-ready. Bounded only by the number of channels
	// the operator has played × TTL — small enough that we never
	// bother evicting (entries naturally roll off as TTL expires
	// during the next attempt).
	decodeModeMu sync.Mutex
	decodeMode   map[string]decodeModeEntry

	stop     chan struct{}
	stopOnce sync.Once
	stopped  chan struct{}
}

type decodeModeEntry struct {
	mode      decodeMode
	expiresAt time.Time
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

	// mode is the decodeMode this session was spawned in (`direct` for
	// `-c copy`, `reencode` for `-c:v libx264 -c:a aac`). Captured at
	// spawn so the post-exit logic knows whether a crash means
	// "promote to reencode" or "we already tried reencode, give up".
	mode decodeMode

	// outcomeOnce ensures the breaker / reporter see exactly one
	// success XOR failure per spawn attempt. Without it, a session
	// that produces a segment then crashes minutes later would record
	// success then failure, double-counting.
	outcomeOnce sync.Once

	// stderrTail is the bounded ring of recent ffmpeg stderr lines.
	// Drained on exit so the operator log includes the actual reason
	// ffmpeg died ("Connection refused", "401 Unauthorized", …) and
	// not just "exit status 8".
	stderrTail *stderrRing

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
	if cfg.MaxReencodeSessions <= 0 {
		// Half the global cap by default. Tight enough that a
		// codec-crash storm can't fill every encoder slot, generous
		// enough that a household with one bad-codec channel doesn't
		// notice. Operators can pin to a small number on shared CPU
		// hosts.
		cfg.MaxReencodeSessions = cfg.MaxSessions / 2
		if cfg.MaxReencodeSessions < 1 {
			cfg.MaxReencodeSessions = 1
		}
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
	if cfg.UserAgent == "" {
		cfg.UserAgent = defaultTransmuxUserAgent
	}

	m := &TransmuxManager{
		cfg:        cfg,
		logger:     logger.With("module", "iptv-transmux"),
		sessions:   make(map[string]*TransmuxSession),
		decodeMode: make(map[string]decodeModeEntry),
		stop:       make(chan struct{}),
		stopped:    make(chan struct{}),
	}
	go m.reapLoop()
	return m
}

// reencodeFallbackEnabled returns whether the manager should auto-promote
// to reencode after a copy-mode crash. Defaults to true so the feature
// works out of the box; callers that want to opt out set
// EnableReencodeFallback to a *bool pointing at false.
func (m *TransmuxManager) reencodeFallbackEnabled() bool {
	if m.cfg.EnableReencodeFallback == nil {
		return true
	}
	return *m.cfg.EnableReencodeFallback
}

// pickDecodeMode returns the cached fallback for a channel, defaulting
// to direct. Expired entries are silently evicted on read so the map
// stays small without a sweeper goroutine.
func (m *TransmuxManager) pickDecodeMode(channelID string) decodeMode {
	if !m.reencodeFallbackEnabled() {
		return decodeModeDirect
	}
	m.decodeModeMu.Lock()
	defer m.decodeModeMu.Unlock()
	e, ok := m.decodeMode[channelID]
	if !ok {
		return decodeModeDirect
	}
	if time.Now().After(e.expiresAt) {
		delete(m.decodeMode, channelID)
		return decodeModeDirect
	}
	return e.mode
}

// promoteToReencode pins a channel in reencode mode for
// decodeModeFallbackTTL. Idempotent: re-promotions reset the TTL but
// don't double-count, and reencode → reencode logs nothing new.
func (m *TransmuxManager) promoteToReencode(channelID string) {
	if !m.reencodeFallbackEnabled() {
		return
	}
	m.decodeModeMu.Lock()
	prev, hadEntry := m.decodeMode[channelID]
	m.decodeMode[channelID] = decodeModeEntry{
		mode:      decodeModeReencode,
		expiresAt: time.Now().Add(decodeModeFallbackTTL),
	}
	wasReencode := hadEntry && prev.mode == decodeModeReencode
	m.decodeModeMu.Unlock()
	if wasReencode {
		return
	}
	m.logger.Info("transmux promoted to reencode",
		"channel", channelID,
		"ttl", decodeModeFallbackTTL)
	if m.cfg.Metrics != nil {
		m.cfg.Metrics.IncReencodePromotions()
	}
}

// GetOrStart returns the live session for channelID, spawning a new
// one if none exists. Blocks until the session has produced its first
// segment (bounded by ReadyTimeout). Calling this concurrently with
// the same channel ID coalesces into a single spawn — the second
// caller waits on the same Ready signal.
//
// If a circuit-breaker gate is configured and the channel is in
// cooldown, returns a *CircuitOpenError without spawning. This is the
// load-bearing protection against the fork-bomb scenario where a dead
// upstream causes the player to retry the manifest every second and
// every retry spawned a fresh ffmpeg process that died in 200 ms.
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
		// Skip the gate — an already-running session means we already
		// proved the upstream is reachable; no point fast-failing live
		// viewers because of stale failure history.
		s.lastTouchUnixNano.Store(time.Now().UnixNano())
		m.mu.Unlock()
		return m.waitReady(ctx, s)
	}
	if m.cfg.Gate != nil {
		if allowed, retryAfter := m.cfg.Gate.Allow(channelID); !allowed {
			m.mu.Unlock()
			if m.cfg.Metrics != nil {
				m.cfg.Metrics.IncStarts("gate_denied")
			}
			return nil, &CircuitOpenError{ChannelID: channelID, RetryAfter: retryAfter}
		}
	}
	if len(m.sessions) >= m.cfg.MaxSessions {
		m.mu.Unlock()
		if m.cfg.Metrics != nil {
			m.cfg.Metrics.IncStarts("busy")
		}
		return nil, ErrTooManySessions
	}

	s, err := m.startLocked(channelID, upstreamURL)
	m.mu.Unlock()
	if err != nil {
		// Failure to even spawn ffmpeg counts against the breaker so
		// repeated config / fs / fork errors trip the cooldown.
		m.recordFailure(s, channelID, err)
		return nil, err
	}
	return m.waitReady(ctx, s)
}

// recordFailure routes a single failure outcome to the gate + reporter
// at most once per session. Safe to call with a nil session (covers
// the path where startLocked failed before producing one).
func (m *TransmuxManager) recordFailure(s *TransmuxSession, channelID string, err error) {
	once := func() {
		if m.cfg.Gate != nil {
			m.cfg.Gate.RecordFailure(channelID)
		}
		if m.cfg.Reporter != nil {
			// Use a fresh, bounded context: the original request ctx is
			// usually cancelled by the time we get here, and the
			// reporter's UPDATE shouldn't be tied to a viewer's session.
			rctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			m.cfg.Reporter.RecordProbeFailure(rctx, channelID, err)
		}
	}
	if s == nil {
		once()
		return
	}
	s.outcomeOnce.Do(once)
}

// recordSuccess routes a single success outcome to the gate + reporter.
// Idempotent on the session via outcomeOnce so repeat ready signals
// (during long-lived sessions) don't double-tick.
func (m *TransmuxManager) recordSuccess(s *TransmuxSession, channelID string) {
	if s == nil {
		return
	}
	s.outcomeOnce.Do(func() {
		if m.cfg.Gate != nil {
			m.cfg.Gate.RecordSuccess(channelID)
		}
		if m.cfg.Reporter != nil {
			rctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			m.cfg.Reporter.RecordProbeSuccess(rctx, channelID)
		}
		if m.cfg.Metrics != nil {
			m.cfg.Metrics.IncStarts("ok")
		}
	})
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

	mode := m.pickDecodeMode(channelID)
	// Reencode-specific cap. Counted on top of the global MaxSessions
	// check at the top of GetOrStart, because reencode is the only
	// path that costs real CPU/GPU. Without this, a single codec-
	// crash storm (one bad provider, multiple channels promoted to
	// reencode) could fill every encoder slot and starve the host.
	if mode == decodeModeReencode {
		if active := m.countReencodeLocked(); active >= m.cfg.MaxReencodeSessions {
			if m.cfg.Metrics != nil {
				m.cfg.Metrics.IncStarts("reencode_busy")
			}
			return nil, ErrTooManyReencodeSessions
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	args := buildTransmuxFFmpegArgsForMode(
		upstreamURL,
		workDir,
		m.cfg.UserAgent,
		m.cfg.ReencodeEncoder,
		m.cfg.ReencodeHWAccelInputArgs,
		mode,
	)
	cmd := exec.CommandContext(ctx, m.cfg.FFmpegPath, args...)
	cmd.Dir = workDir
	if m.cfg.Metrics != nil {
		m.cfg.Metrics.IncDecodeMode(mode.String())
	}
	// Detach from parent terminal — we never want ffmpeg to read
	// stdin or attach to a controlling TTY (it does both by default
	// in some builds, which can wedge a server with no TTY).
	cmd.Stdin = nil

	// Capture stderr through a pipe so we can both ring-buffer the
	// last N lines (for the exit log) and tee them back to the
	// container log at debug volume. Without this, "exit status 8"
	// is opaque — operators have to docker exec ffmpeg by hand to
	// reproduce. The ring is intentionally small (~64 lines): ffmpeg's
	// fatal message is always within the last few lines before exit.
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		_ = os.RemoveAll(workDir)
		return nil, fmt.Errorf("iptv-transmux: stderr pipe: %w", err)
	}

	session := &TransmuxSession{
		ChannelID:   channelID,
		UpstreamURL: upstreamURL,
		WorkDir:     workDir,
		StartedAt:   time.Now(),
		cmd:         cmd,
		cancel:      cancel,
		done:        make(chan struct{}),
		ready:       make(chan struct{}),
		stderrTail:  newStderrRing(ffmpegStderrTailLines),
		mode:        mode,
	}
	session.lastTouchUnixNano.Store(time.Now().UnixNano())

	if err := cmd.Start(); err != nil {
		cancel()
		_ = stderrPipe.Close()
		_ = os.RemoveAll(workDir)
		return nil, fmt.Errorf("iptv-transmux: start ffmpeg: %w", err)
	}

	// Drain stderr in a goroutine. The reader exits when the pipe is
	// closed (which happens automatically when ffmpeg exits), so we
	// don't need explicit teardown.
	go session.stderrTail.consume(stderrPipe)

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
//
// Outcome routing: if the session never produced a segment, the spawn
// counts as a failure for the breaker + health reporter. If it did,
// the success was already recorded by readyWatcher and we only log.
// outcomeOnce on the session ensures we don't double-record either way.
func (m *TransmuxManager) processWatcher(s *TransmuxSession) {
	defer close(s.done)
	err := s.cmd.Wait()
	stderrTail := s.stderrTail.String()
	wasReady := isReady(s)
	switch {
	case err != nil && !wasReady:
		// Spawn never reached first segment. This is the case the
		// breaker exists for — repeat occurrences trip the cooldown.
		m.logger.Info("transmux ffmpeg exited before first segment",
			"channel", s.ChannelID,
			"mode", s.mode.String(),
			"error", err,
			"ffmpeg_stderr_tail", stderrTail)
		m.recordFailure(s, s.ChannelID, fmt.Errorf("ffmpeg exit before ready: %w (stderr: %s)", err, stderrTail))
		if m.cfg.Metrics != nil {
			m.cfg.Metrics.IncStarts("crash")
		}
		// If the crash happened in direct (-c copy) mode AND the
		// stderr looks codec-related (so we believe the upstream is
		// reachable but ffmpeg can't repackage it), pin the channel
		// in reencode mode so the next attempt transparently
		// transcodes. We deliberately don't promote on every crash:
		// a TCP-level "Connection refused" should NOT cause us to
		// burn CPU on the next retry — for that the breaker is the
		// right tool, not reencode.
		if s.mode == decodeModeDirect && looksLikeCodecError(stderrTail) {
			m.promoteToReencode(s.ChannelID)
		}
	case err != nil:
		// Late crash after the session served at least one segment.
		// The first-segment success already counted; treat the late
		// exit as informational so a single mid-stream blip doesn't
		// reopen the breaker against a working channel.
		m.logger.Info("transmux ffmpeg exited",
			"channel", s.ChannelID,
			"error", err,
			"ffmpeg_stderr_tail", stderrTail)
	default:
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
				m.recordSuccess(s, s.ChannelID)
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

// ActiveReencodeSessions reports how many of the active sessions are
// in `reencode` mode. Exposed for observability + tests; the cap
// check inside GetOrStart uses countReencodeLocked under the
// existing manager lock to avoid a re-acquire.
func (m *TransmuxManager) ActiveReencodeSessions() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.countReencodeLocked()
}

// MaxReencodeSessions reports the configured cap. Useful for the
// startup log line so operators can see what the manager actually
// landed on (the zero-value default expands to MaxSessions/2 inside
// the constructor).
func (m *TransmuxManager) MaxReencodeSessions() int {
	return m.cfg.MaxReencodeSessions
}

// countReencodeLocked returns the number of reencode-mode sessions.
// Caller must hold m.mu.
func (m *TransmuxManager) countReencodeLocked() int {
	n := 0
	for _, s := range m.sessions {
		if s.mode == decodeModeReencode {
			n++
		}
	}
	return n
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

// stderrRing is a goroutine-safe FIFO of the last N stderr lines from
// ffmpeg. We use it instead of a full buffer because a session that
// runs for hours produces tens of thousands of warning lines and the
// only one operators ever care about is the fatal one right before
// exit. Newline-delimited; binary garbage is silently truncated.
type stderrRing struct {
	mu    sync.Mutex
	lines []string
	max   int
}

func newStderrRing(max int) *stderrRing {
	if max <= 0 {
		max = 16
	}
	return &stderrRing{max: max, lines: make([]string, 0, max)}
}

// consume reads from r line-by-line until EOF, keeping the last `max`
// lines. Lines longer than 4 KiB are truncated to keep ffmpeg's
// occasional megabyte-long debug spew from blowing up RAM. Binary on
// stderr (rare; ffmpeg only emits it with -loglevel debug) is best-
// effort decoded as UTF-8 bytes; bufio.Scanner will return whatever
// bytes precede the next \n.
func (r *stderrRing) consume(rd io.Reader) {
	scanner := bufio.NewScanner(rd)
	// 4 KiB buffer per line covers every ffmpeg warning we've ever
	// seen in production; the ring will rotate before they'd matter.
	scanner.Buffer(make([]byte, 4096), 4096)
	for scanner.Scan() {
		r.push(scanner.Text())
	}
	// Scanner errors (e.g. closed pipe on shutdown) are intentionally
	// ignored — losing the last line of a torn-down session is fine.
}

func (r *stderrRing) push(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.lines) >= r.max {
		// Drop the oldest line (FIFO). Slice trick: re-use the backing
		// array by shifting in place rather than allocating.
		copy(r.lines, r.lines[1:])
		r.lines = r.lines[:len(r.lines)-1]
	}
	r.lines = append(r.lines, line)
}

// String returns the accumulated tail joined by " | ". Empty when the
// session has not produced any stderr (ffmpeg is silent at -loglevel
// warning unless something goes wrong).
func (r *stderrRing) String() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.lines) == 0 {
		return ""
	}
	return strings.Join(r.lines, " | ")
}

// codecErrorPattern matches the stderr fragments ffmpeg emits when a
// `-c copy` pipeline can't repackage the upstream — typically a codec
// the H264 bitstream filter rejects, an audio profile that won't fit
// the destination container, or a missing PMT entry. Matched on the
// captured stderr tail so we promote to reencode mode only when the
// upstream is reachable enough to negotiate codecs.
//
// Patterns are kept conservative: we'd rather miss a few promotion
// opportunities than burn CPU re-encoding a TCP-refused dead host.
// The breaker handles the latter cleanly already.
var codecErrorPattern = regexp.MustCompile(
	`(?i)(invalid data found|could not find codec parameters|h264_mp4toannexb|hevc.*not.*supported|non-monotonic dts.*aborting|stream specifier.*matches no streams|codec not currently supported|" hevc"| ac3 |eac3|hevc_mp4toannexb|invalid nal unit size)`,
)

// looksLikeCodecError reports whether the captured stderr tail
// resembles a codec-incompatibility crash (vs. a network / auth
// problem). Empty tail returns false — we never promote on no
// evidence.
func looksLikeCodecError(stderrTail string) bool {
	if stderrTail == "" {
		return false
	}
	return codecErrorPattern.MatchString(stderrTail)
}

// buildTransmuxFFmpegArgs constructs the argv for the default direct
// (`-c copy`) pipeline. Thin wrapper around the mode-aware variant so
// callers / tests that only care about the fast path don't thread an
// extra parameter.
func buildTransmuxFFmpegArgs(upstreamURL, workDir, userAgent string) []string {
	return buildTransmuxFFmpegArgsForMode(upstreamURL, workDir, userAgent, "", nil, decodeModeDirect)
}

// buildTransmuxFFmpegArgsForMode dispatches to the mode-specific argv
// builder. `direct` is the cheap path (`-c copy`, near-zero CPU);
// `reencode` is the codec-rescue path that turns whatever the upstream
// sends into H.264 + AAC at the lowest CPU preset.
//
// Common flags (input shaping, reconnection, HLS window) live in
// commonTransmuxArgs / hlsOutputArgs so the two modes diverge only on
// the codec section, which is the actually-different decision.
//
// `encoder` and `hwAccelInputArgs` only apply to the reencode path:
// pass "" / nil for direct and they're ignored. Direct mode never
// decodes (`-c copy` is byte-level repackaging), so a `-hwaccel` flag
// would be both pointless and risky on certain backends that demand
// `-hwaccel_output_format`.
func buildTransmuxFFmpegArgsForMode(upstreamURL, workDir, userAgent, encoder string, hwAccelInputArgs []string, mode decodeMode) []string {
	if mode == decodeModeReencode {
		return buildReencodeArgs(upstreamURL, workDir, userAgent, encoder, hwAccelInputArgs)
	}
	return buildDirectArgs(upstreamURL, workDir, userAgent)
}

// commonTransmuxArgs returns the input-side ffmpeg flags shared by
// both decode modes (input URL, reconnection, buffering, UA).
//
// Reconnection: `-reconnect_at_eof 1` + `-reconnect_streamed 1` make
// a flaky upstream recover without ffmpeg exiting; the session manager
// only re-spawns on a hard exit.
//
// Buffering: `-rtbufsize 50M` absorbs upstream jitter (~50 MB RAM per
// active session) so we don't drop packets when input arrives faster
// than the muxer can drain. `-max_delay 5000000` (5 s) gives the
// demuxer slack for reordered packets — without it, noisy providers
// produce "non-monotonic DTS" warnings and segment dropouts.
//
// `-user_agent` matters more than it looks. Many Xtream Codes panels
// gate on UA: with the default `Lavf/<version>` ffmpeg sends they
// return an HTML error page (decoded as "Invalid data" → exit 8) or
// a codec profile that doesn't survive `-c copy`. Mirroring the
// prober's `VLC/3.0.20` UA is the same workaround every IPTV player
// ships.
func commonTransmuxArgs(upstreamURL, workDir, userAgent string) []string {
	if userAgent == "" {
		userAgent = defaultTransmuxUserAgent
	}
	return []string{
		"-hide_banner",
		"-loglevel", "warning",
		"-nostdin",
		"-fflags", "+genpts+discardcorrupt",
		"-user_agent", userAgent,
		"-rtbufsize", "50M",
		"-max_delay", "5000000",
		"-reconnect", "1",
		"-reconnect_at_eof", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "5",
		"-rw_timeout", "10000000", // 10 s I/O timeout in microseconds
		"-i", upstreamURL,
	}
}

// hlsOutputArgs returns the ffmpeg flags that select the HLS muxer +
// sliding-window settings, identical for both decode modes.
//
// HLS window choices (tuned for live Xtream playback):
//   - `hls_time 2` — short segments halve buffer-underrun recovery
//     time and reduce live latency.
//   - `hls_list_size 20` — 40 s manifest window absorbs the
//     ~10 s background-tab stalls Chrome / Firefox produce without
//     the player falling out of range.
//   - `hls_delete_threshold 5` — keep 5 extra segments past the tail
//     so a slow client whose manifest parse cycle is behind can still
//     fetch what it asked for instead of getting 404.
//   - `+temp_file` flag writes each segment to `.tmp` first and
//     atomically renames into place. Without it, http.ServeFile can
//     serve a partially-written `.ts`, triggering bufferStalledError.
//   - `omit_endlist` keeps the manifest live (no EXT-X-ENDLIST).
//     `delete_segments` keeps disk usage bounded.
func hlsOutputArgs(workDir string) []string {
	return []string{
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

func buildDirectArgs(upstreamURL, workDir, userAgent string) []string {
	args := commonTransmuxArgs(upstreamURL, workDir, userAgent)
	args = append(args,
		"-map", "0:v:0",
		"-map", "0:a:0?",
		"-c", "copy",
		"-bsf:v", "h264_mp4toannexb",
	)
	return append(args, hlsOutputArgs(workDir)...)
}

// buildReencodeArgs is the codec-rescue path for upstreams whose
// codec / container combination doesn't survive `-c copy`. We still
// pass through audio when it's already AAC (cheap) and only fall
// back to a video re-encode (the part that actually costs CPU).
//
// `encoder` selects the output video encoder ("libx264" for software,
// "h264_nvenc" / "h264_vaapi" / "h264_qsv" / "h264_videotoolbox" for
// hardware). Empty defaults to libx264. `hwAccelInputArgs` are the
// matching `-hwaccel ...` flags that go before `-i` so the decoder
// runs on the same accelerator — without those, ffmpeg would decode
// in software and only encode on the GPU, losing most of the gain.
//
// Per-encoder tuning: libx264 wants -preset/-tune; the hardware
// encoders use their own preset names. Mirroring the VOD transcoder
// pattern in internal/stream/transcode.go.BuildFFmpegArgs.
func buildReencodeArgs(upstreamURL, workDir, userAgent, encoder string, hwAccelInputArgs []string) []string {
	if encoder == "" {
		encoder = "libx264"
	}
	args := commonTransmuxArgs(upstreamURL, workDir, userAgent)
	// Splice -hwaccel BEFORE the trailing -i URL (which
	// commonTransmuxArgs appended last). The decode-side flag must
	// precede its input or ffmpeg ignores it.
	args = insertBeforeInput(args, hwAccelInputArgs)
	args = append(args,
		"-map", "0:v:0",
		"-map", "0:a:0?",
		"-c:v", encoder,
	)
	args = append(args, encoderTuningArgs(encoder)...)
	args = append(args,
		// Keyframe interval = HLS segment duration × frame rate.
		// hls_time=2, assume 24 fps → 48-frame GOP. -sc_threshold 0
		// disables scenecut so segments stay aligned to GOP
		// boundaries (HLS players require segments to start with an
		// IDR; a stray scenecut produces partial segments).
		"-g", "48",
		"-sc_threshold", "0",
		// Audio: re-encode to AAC stereo. Most Xtream feeds carry
		// AAC LC already which would survive `-c:a copy`, but mixing
		// copy + transcode video produces a/v desync for a few seconds
		// at startup; AAC re-encode is cheap (~1% CPU) and avoids it.
		"-c:a", "aac",
		"-ac", "2",
		"-b:a", "128k",
	)
	return append(args, hlsOutputArgs(workDir)...)
}

// encoderTuningArgs returns encoder-specific quality / latency flags.
// Each hardware encoder has its own preset vocabulary, so the libx264
// "veryfast / zerolatency" doesn't translate directly. Defaults are
// chosen for live transcode (low latency, "good enough" quality) —
// not for archive or top-bitrate masters.
//
// libx264:
//   - veryfast preset, zerolatency tune. Standard Jellyfin / Threadfin
//     trade-off — ~10-20% of one core for 1080p H.264 → H.264.
//   - keyint=48 + scenecut=0 forces predictable GOP boundaries, which
//     -g/-sc_threshold also do globally; we keep both for clarity.
//   - Main profile / Level 4.0 covers every browser + Chromecast.
//
// h264_nvenc:
//   - p4 preset is NVIDIA's "medium" tier under the new perf ladder
//     (p1=fastest/lowest-quality, p7=slowest/highest); p4 matches the
//     CPU/quality trade-off of libx264 veryfast.
//   - tune ll = "low latency". Without this NVENC defaults to high
//     quality with B-frames → adds startup delay we don't want here.
//
// h264_vaapi:
//   - VAAPI exposes a coarse `quality` knob (1-7, lower is faster).
//     `quality 4` is "balanced".
//   - We don't set `-bsf:v h264_mp4toannexb` because VAAPI emits
//     Annex-B natively for HLS.
//
// h264_qsv:
//   - QSV's preset names mirror libx264 (`veryfast`, `fast`, …).
//   - look_ahead=0 disables Intel's lookahead which adds latency.
//
// h264_videotoolbox:
//   - macOS-only. allow_sw=0 forces hardware path; we'd rather fail
//     than silently fall back when the operator asked for VT.
//   - realtime=1 picks the low-latency rate-control mode.
func encoderTuningArgs(encoder string) []string {
	switch encoder {
	case "h264_nvenc":
		return []string{
			"-preset", "p4",
			"-tune", "ll",
			"-rc", "cbr",
			"-pix_fmt", "yuv420p",
			"-profile:v", "main",
			"-level", "4.0",
		}
	case "h264_vaapi":
		return []string{
			"-quality", "4",
			"-profile:v", "main",
			"-level", "40",
		}
	case "h264_qsv":
		return []string{
			"-preset", "veryfast",
			"-look_ahead", "0",
			"-pix_fmt", "yuv420p",
			"-profile:v", "main",
			"-level", "40",
		}
	case "h264_videotoolbox":
		return []string{
			"-allow_sw", "0",
			"-realtime", "1",
			"-pix_fmt", "yuv420p",
			"-profile:v", "main",
			"-level", "4.0",
		}
	default: // libx264 + any unknown
		return []string{
			"-preset", "veryfast",
			"-tune", "zerolatency",
			"-pix_fmt", "yuv420p",
			"-profile:v", "main",
			"-level", "4.0",
			"-x264-params", "keyint=48:min-keyint=48:scenecut=0",
		}
	}
}

// insertBeforeInput returns a copy of args with `extra` inserted just
// before the trailing `-i <url>` pair that commonTransmuxArgs appends.
// Falls back to appending if no `-i` is found (defensive — should
// not happen in practice, but a panic on argv construction would
// take the whole transmux subsystem down).
func insertBeforeInput(args, extra []string) []string {
	if len(extra) == 0 {
		return args
	}
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-i" {
			out := make([]string, 0, len(args)+len(extra))
			out = append(out, args[:i]...)
			out = append(out, extra...)
			out = append(out, args[i:]...)
			return out
		}
	}
	return append(args, extra...)
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
