package iptv

// Transmux session manager: convierte MPEG-TS upstream (formato de
// Xtream M3U_PLUS que los browsers no reproducen) a HLS vía ffmpeg
// `-c copy -f hls`. CPU ~0 porque no re-codifica.
//
// Decisiones clave:
//  - Una sesión por canal, compartida entre viewers (1 ffmpeg, 1 upstream).
//  - Lazy spawn, idle reap (idleTimeout sin segment requests).
//  - maxSessions limita ffmpeg concurrentes (evita fork-bomb).
//  - Ready signal: el handler espera al primer segmento con timeout.
//  - Aislamiento de fallos: crash marca la sesión como fallida y la
//    elimina; el circuit breaker toma el control.

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

// ErrTooManySessions — el manager está al máximo de sesiones (503).
var ErrTooManySessions = errors.New("iptv-transmux: max sessions reached")

// ErrTooManyReencodeSessions — cap de reencode alcanzado (distinto
// de ErrTooManySessions para métricas separadas).
var ErrTooManyReencodeSessions = errors.New("iptv-transmux: max reencode sessions reached")

// ErrSessionNotFound — no hay sesión viva para el canal (404).
var ErrSessionNotFound = errors.New("iptv-transmux: no session")

// ErrTransmuxFailed — ffmpeg salió sin producir ningún segmento.
var ErrTransmuxFailed = errors.New("iptv-transmux: session failed before ready")

// ChannelGate — contrato del circuit breaker compartido entre el
// proxy y el transmux manager. Goroutine-safe, barato de llamar.
// La implementación concreta está en circuit_breaker.go.
type ChannelGate interface {
	Allow(channelID string) (bool, time.Duration)
	RecordSuccess(channelID string)
	RecordFailure(channelID string)
}

// decodeMode — cómo ffmpeg convierte MPEG-TS a HLS. `direct` (-c copy,
// ~0 CPU) para la mayoría. Algunos codecs (HEVC main10, AC3) crashean
// en copy mode; para esos se cae a `reencode` transparentemente.
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

// decodeModeFallbackTTL — duración del pin en reencode tras auto-promoción.
// Expirado el TTL se reintenta direct por si el upstream se recuperó.
const decodeModeFallbackTTL = 1 * time.Hour

// TransmuxMetrics — sink de métricas opcional. Nil-tolerant.
type TransmuxMetrics interface {
	IncStarts(outcome string)        // ok, crash, gate_denied, busy
	IncDecodeMode(mode string)       // direct, reencode
	IncReencodePromotions()          // sticky flips after a copy-mode crash
}

// segmentNamePattern — validar nombres de segmento contra path-traversal.
var segmentNamePattern = regexp.MustCompile(`^seg-\d{5,6}\.ts$`)

// IsValidSegmentName — exportado para que el handler rechace path-traversal.
func IsValidSegmentName(name string) bool {
	return segmentNamePattern.MatchString(name)
}

// TransmuxManagerConfig — knobs de comportamiento runtime.
// NewTransmuxManager rellena defaults para valores zero.
type TransmuxManagerConfig struct {
	// CacheDir — directorio padre para work dirs per-canal. Se crea con 0755.
	CacheDir string

	// FFmpegPath — ruta al binario ffmpeg. Default "ffmpeg" (de $PATH).
	FFmpegPath string

	// MaxSessions — máximo de sesiones simultáneas. Default 10.
	MaxSessions int

	// IdleTimeout — tiempo sin segment requests antes de reap. Default 30s.
	IdleTimeout time.Duration

	// ReadyTimeout — espera máxima al primer segmento. Default 15s.
	ReadyTimeout time.Duration

	// ReaperInterval — frecuencia del barrido de idle. Default 5s.
	ReaperInterval time.Duration

	// UserAgent — enviado al upstream por ffmpeg. Vacío = default.
	UserAgent string

	// Gate — circuit breaker per-canal opcional.
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
		// Distinct metric outcome from `crash` (which is a process
		// that DID start and exited before first segment): spawn_error
		// flags problems on our side (mkdir, fork, pipe) vs upstream
		// problems flagged by `crash`. Reencode-cap denials are
		// already counted as `reencode_busy` from inside startLocked
		// before we get here, so don't double-count those.
		if m.cfg.Metrics != nil && !errors.Is(err, ErrTooManyReencodeSessions) {
			m.cfg.Metrics.IncStarts("spawn_error")
		}
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
	// Per-spawn versioned workdir. Each session lives in its own
	// timestamped subdirectory so a concurrent evict (RemoveAll on
	// the previous spawn's dir) can never race a new spawn's MkdirAll
	// for the same channel. Without this, a quick channel-zap could
	// see ffmpeg writing to a directory that gets blown away mid-
	// flight. cleanup deletes the spawn-specific subdir only; the
	// parent <channelID>/ dir is best-effort cleaned when empty.
	startedAt := time.Now()
	workDir := filepath.Join(m.cfg.CacheDir, channelID, fmt.Sprintf("%d", startedAt.UnixNano()))
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, fmt.Errorf("iptv-transmux: mkdir work dir: %w", err)
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
		StartedAt:   startedAt,
		cmd:         cmd,
		cancel:      cancel,
		done:        make(chan struct{}),
		ready:       make(chan struct{}),
		stderrTail:  newStderrRing(ffmpegStderrTailLines),
		mode:        mode,
	}
	session.lastTouchUnixNano.Store(startedAt.UnixNano())

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
	// Synchronise with the stderr consumer goroutine before reading
	// the ring. cmd.Wait() returns when the process is reaped but
	// does NOT wait for our pipe consumer to drain its remaining
	// buffered bytes. Without this barrier the captured tail can
	// miss the fatal line ffmpeg prints right before exit, which is
	// exactly the line we need for the breaker log AND for the
	// codec-fallback classifier (looksLikeCodecError below).
	s.stderrTail.wait()
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

// startupGraceMultiplier bounds how long a session is allowed to stay
// in the "starting" state (alive but no first segment yet) before the
// reaper force-terminates it. Multiplier of 2 over ReadyTimeout gives
// a misbehaving upstream a generous second window without leaving
// genuinely-stuck sessions hogging a MaxSessions slot forever.
//
// Why this matters: ffmpeg's -rw_timeout (10 s) only fires when an
// individual read blocks; an upstream that trickles 1 byte every few
// seconds keeps the I/O alive without ever producing a usable stream.
// Without this cap such sessions would never be reaped (readyWatcher
// returns silently, processWatcher is blocked on cmd.Wait), and each
// channel-zap to a misbehaving stream would consume one slot
// permanently.
const startupGraceMultiplier = 2

func (m *TransmuxManager) reapOnce() {
	now := time.Now()
	nowNanos := now.UnixNano()
	cutoff := nowNanos - m.cfg.IdleTimeout.Nanoseconds()
	startupDeadline := startupGraceMultiplier * m.cfg.ReadyTimeout

	m.mu.Lock()
	var toStop []*TransmuxSession
	var toStopReason []string
	for id, s := range m.sessions {
		ready := isReady(s)
		if !ready {
			// Sessions that never reached first segment have their
			// own readyWatcher / ReadyTimeout protection for the
			// happy path, but a wedged ffmpeg (upstream trickling
			// bytes, evading -rw_timeout) keeps them alive forever.
			// Cap them at startupGraceMultiplier × ReadyTimeout so
			// stuck spawns can't pin a MaxSessions slot indefinitely.
			if now.Sub(s.StartedAt) > startupDeadline {
				toStop = append(toStop, s)
				toStopReason = append(toStopReason, "startup_timeout")
				delete(m.sessions, id)
			}
			continue
		}
		if s.lastTouchUnixNano.Load() < cutoff {
			toStop = append(toStop, s)
			toStopReason = append(toStopReason, "idle")
			delete(m.sessions, id)
		}
	}
	m.mu.Unlock()

	for i, s := range toStop {
		switch toStopReason[i] {
		case "startup_timeout":
			m.logger.Warn("transmux startup timeout reap",
				"channel", s.ChannelID,
				"alive_for", now.Sub(s.StartedAt),
				"ready_timeout", m.cfg.ReadyTimeout)
			// Pre-ready stuck spawns count as a failure for the
			// breaker so a chronically broken channel trips the
			// cooldown instead of cycling reaper kills forever.
			m.recordFailure(s, s.ChannelID, fmt.Errorf("startup timeout after %s", now.Sub(s.StartedAt)))
		default:
			m.logger.Info("transmux idle reap",
				"channel", s.ChannelID,
				"idle_for", time.Duration(nowNanos-s.lastTouchUnixNano.Load()))
		}
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
	cleanupWorkDir(s.WorkDir)
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
	cleanupWorkDir(s.WorkDir)
}

// cleanupWorkDir removes the per-spawn versioned subdirectory and,
// best-effort, its parent <channelID> dir if it ends up empty (no
// other concurrent spawns for the same channel). os.Remove on a
// non-empty dir returns ENOTEMPTY which we silently swallow — leaving
// an empty parent for a moment is harmless and far simpler than
// reference-counting concurrent spawns.
func cleanupWorkDir(workDir string) {
	if workDir == "" {
		return
	}
	_ = os.RemoveAll(workDir)
	parent := filepath.Dir(workDir)
	_ = os.Remove(parent) // ENOTEMPTY is fine; we just clean if last spawn out
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
