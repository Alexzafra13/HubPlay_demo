package stream

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"hubplay/internal/config"
	"hubplay/internal/db"
	"hubplay/internal/domain"
	"hubplay/internal/event"
)

// MetricsSink interfaz local de observability para evitar ciclo de paquetes.
type MetricsSink interface {
	TranscodeStarted()
	TranscodeBusy()
	TranscodeFailed()
	SetActiveSessions(n int)
}

type noopSink struct{}

func (noopSink) TranscodeStarted()       {}
func (noopSink) TranscodeBusy()          {}
func (noopSink) TranscodeFailed()        {}
func (noopSink) SetActiveSessions(n int) {}

// Manager orquesta sesiones de streaming (direct play, remux, transcode).
// Manager.sessions es la vista lógica (clave compuesta user+item+profile+audio+sub);
// Transcoder.sessions es la vista del proceso ffmpeg. Ambos mapas apuntan
// al mismo *Session — no hay duplicación, solo dos perspectivas.
type Manager struct {
	mu         sync.Mutex
	sessions   map[string]*ManagedSession
	transcoder *Transcoder
	items      *db.ItemRepository
	streams    *db.MediaStreamRepository
	cfg        config.StreamingConfig
	logger     *slog.Logger
	stopClean  chan struct{}
	metrics MetricsSink
	bus     *event.Bus // nil-safe
	// startGroup: singleflight por session key para colapsar inicios concurrentes.
	startGroup singleflight.Group
	// hwAccel: snapshot de detección al arranque; leído por admin stats.
	hwAccel HWAccelResult
	// forceDirectPlayLookup: hook a runtime settings para el toggle admin
	// playback.force_direct_play. Nil-safe.
	forceDirectPlayLookup func(context.Context) bool
}

// ManagedSession envuelve una Session con tracking de acceso y contexto de usuario.
type ManagedSession struct {
	*Session
	UserID           string
	InputPath        string             // ruta fuente; cacheada para evitar re-query en seeks
	AudioStreamIndex int                // per-type index de audio; -1 = auto-pick
	BurnSubtitle     *BurnSubtitleSpec  // nil = sin burn-in
	Decision         PlaybackDecision
	LastAccessed     time.Time

	// restartMu: mutex por-sesión para serializar restarts sin bloquear m.mu.
	restartMu          sync.Mutex
	LastRestartSegment int       // -start_number del ffmpeg actual
	LastRestartTime    time.Time // para la ventana de coalescencia
	// Rate limiter sliding-window contra seeks descontrolados del frontend.
	restartWindowStart time.Time
	restartWindowCount int
}

var ErrRestartRateLimited = errors.New("stream: restart rate limit exceeded")
var ErrSessionNotFound = errors.New("stream: session not found")

// Deps agrupa las dependencias de NewManager. Los 4 primeros son
// obligatorios; Metrics, EventBus y ForceDirectPlayLookup son opcionales
// (nil deja defaults). Los setters siguen existiendo para tests.
type Deps struct {
	Items   *db.ItemRepository
	Streams *db.MediaStreamRepository
	Config  config.StreamingConfig
	Logger  *slog.Logger

	Metrics               MetricsSink              // nil = noopSink
	EventBus              *event.Bus               // nil = sin eventos
	ForceDirectPlayLookup func(context.Context) bool // nil = algoritmo estándar
}

// NewManager crea un streaming manager. Detecta HW accel y aplica auto-tune.
func NewManager(deps Deps) *Manager {
	items := deps.Items
	streams := deps.Streams
	cfg := deps.Config
	logger := deps.Logger
	cacheDir := cfg.EffectiveCacheDir()

	hwAccel := HWAccelNone
	encoder := "libx264"
	hwResult := HWAccelResult{Selected: HWAccelNone, Encoder: "libx264"}
	if cfg.HWAccel.Enabled {
		hwResult = DetectHWAccel(cfg.HWAccel.Preferred, logger)
		hwAccel = hwResult.Selected
		encoder = hwResult.Encoder
	}

	// Auto-tune: runtime.NumCPU() es cgroup-aware en Linux (Go 1.18+).
	tuned := AutoTuneStreaming(cfg, hwAccel, runtime.NumCPU())
	logger.Info("streaming auto-tune applied",
		"hw_accel", hwAccel,
		"cpu_count", runtime.NumCPU(),
		"max_sessions", tuned.MaxTranscodeSessions,
		"max_sessions_per_user", tuned.MaxTranscodeSessionsPerUser,
		"libx264_preset", tuned.TranscodePreset,
	)
	cfg = tuned

	m := &Manager{
		sessions: make(map[string]*ManagedSession),
		transcoder: NewTranscoder(TranscoderConfig{
			BaseDir:          cacheDir,
			TranscodeTimeout: cfg.TranscodeTimeout,
			HWAccel:          hwAccel,
			Encoder:          encoder,
			Libx264Preset:    cfg.TranscodePreset,
			Logger:           logger,
		}),
		items:      items,
		streams:    streams,
		cfg:        cfg,
		logger:     logger.With("module", "stream-manager"),
		stopClean:  make(chan struct{}),
		metrics:    noopSink{},
		hwAccel:    hwResult,
	}

	if deps.Metrics != nil {
		m.metrics = deps.Metrics
		m.metrics.SetActiveSessions(0)
	}
	m.bus = deps.EventBus
	m.forceDirectPlayLookup = deps.ForceDirectPlayLookup

	go m.cleanupLoop()
	return m
}

// SetMetrics conecta un sink de métricas. Nil = no-op.
func (m *Manager) SetMetrics(sink MetricsSink) {
	if sink == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metrics = sink
	m.metrics.SetActiveSessions(len(m.sessions))
}

// SetEventBus conecta un bus de eventos. Nil deshabilita publicación.
func (m *Manager) SetEventBus(bus *event.Bus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bus = bus
}

func (m *Manager) publish(e event.Event) {
	m.mu.Lock()
	bus := m.bus
	m.mu.Unlock()
	if bus != nil {
		bus.Publish(e)
	}
}

// sessionKey: clave compuesta user+item+profile+audio+sub. Los índices
// forman parte de la clave porque un cambio de audio o subtitle quemado
// requiere una sesión de transcode distinta.
func sessionKey(userID, itemID, profile string, audioStreamIndex, burnSubIndex int) string {
	return userID + ":" + itemID + ":" + profile +
		":" + strconv.Itoa(audioStreamIndex) +
		":" + strconv.Itoa(burnSubIndex)
}

// SessionKey es el constructor canónico exportado de clave de sesión.
func SessionKey(userID, itemID, profile string, audioStreamIndex, burnSubIndex int) string {
	return sessionKey(userID, itemID, profile, audioStreamIndex, burnSubIndex)
}

// SetForceDirectPlayLookup conecta el hook de runtime settings para
// playback.force_direct_play. Nil-safe e idempotente.
func (m *Manager) SetForceDirectPlayLookup(fn func(context.Context) bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.forceDirectPlayLookup = fn
}

func (m *Manager) shouldForceDirectPlay(ctx context.Context) bool {
	m.mu.Lock()
	fn := m.forceDirectPlayLookup
	m.mu.Unlock()
	if fn == nil {
		return false
	}
	return fn(ctx)
}

// StartSessionRequest agrupa los parámetros de StartSession.
// BurnSubIndex >=0 fuerza Transcode con burn-in; <0 = sin burn-in.
type StartSessionRequest struct {
	UserID           string
	ItemID           string
	ProfileName      string
	Caps             *Capabilities
	StartTime        float64
	AudioStreamIndex int
	BurnSubIndex     int
}

func (r StartSessionRequest) sessionKey() string {
	return sessionKey(r.UserID, r.ItemID, r.ProfileName, r.AudioStreamIndex, r.BurnSubIndex)
}

// StartSession crea o reutiliza una sesión existente. Llamadas
// concurrentes para la misma clave colapsan vía singleflight.
func (m *Manager) StartSession(ctx context.Context, req StartSessionRequest) (*ManagedSession, error) {
	key := req.sessionKey()

	// Fast path: sesión ya activa.
	m.mu.Lock()
	if ms, ok := m.sessions[key]; ok {
		ms.LastAccessed = time.Now()
		m.mu.Unlock()
		return ms, nil
	}
	m.mu.Unlock()

	v, err, _ := m.startGroup.Do(key, func() (any, error) {
		return m.startSessionSlow(ctx, key, req)
	})
	if err != nil {
		return nil, err
	}
	return v.(*ManagedSession), nil
}

func (m *Manager) startSessionSlow(ctx context.Context, key string, req StartSessionRequest) (*ManagedSession, error) {
	userID := req.UserID
	itemID := req.ItemID
	profileName := req.ProfileName
	caps := req.Caps
	startTime := req.StartTime
	audioStreamIndex := req.AudioStreamIndex
	burnSubIndex := req.BurnSubIndex
	// Re-check: un Do previo pudo completar entre el fast-path miss y aquí.
	m.mu.Lock()
	if ms, ok := m.sessions[key]; ok {
		ms.LastAccessed = time.Now()
		m.mu.Unlock()
		return ms, nil
	}

	if m.cfg.MaxTranscodeSessions > 0 && len(m.sessions) >= m.cfg.MaxTranscodeSessions {
		active := len(m.sessions)
		m.mu.Unlock()
		m.metrics.TranscodeBusy()
		return nil, domain.NewTranscodeBusy(active, m.cfg.MaxTranscodeSessions)
	}

	if m.cfg.MaxTranscodeSessionsPerUser > 0 {
		var userActive int
		for _, ms := range m.sessions {
			if ms.UserID == userID {
				userActive++
			}
		}
		if userActive >= m.cfg.MaxTranscodeSessionsPerUser {
			m.mu.Unlock()
			m.metrics.TranscodeBusy()
			return nil, domain.NewTranscodeBusy(userActive, m.cfg.MaxTranscodeSessionsPerUser)
		}
	}
	m.mu.Unlock()

	item, err := m.items.GetByID(ctx, itemID)
	if err != nil {
		m.metrics.TranscodeFailed()
		return nil, fmt.Errorf("get item: %w", err)
	}

	mediaStreams, err := m.streams.ListByItem(ctx, itemID)
	if err != nil {
		m.metrics.TranscodeFailed()
		return nil, fmt.Errorf("get streams: %w", err)
	}

	// Resolver spec de burn-in. Index fuera de rango = nil (sin burn-in).
	var burnSub *BurnSubtitleSpec
	if burnSubIndex >= 0 {
		var subOrd int
		for _, s := range mediaStreams {
			if s.StreamType != "subtitle" {
				continue
			}
			if subOrd == burnSubIndex {
				if IsBurnableSubtitleCodec(s.Codec) {
					burnSub = &BurnSubtitleSpec{
						Index:     burnSubIndex,
						Codec:     s.Codec,
						InputPath: item.Path,
					}
				}
				break
			}
			subOrd++
		}
	}

	var decision PlaybackDecision
	if m.shouldForceDirectPlay(ctx) {
		decision = DecideForceDirectPlay(item, mediaStreams)
	} else {
		decision = Decide(item, mediaStreams, caps, profileName)
	}

	// Burn-in necesita decoded frames → forzar Transcode si la waterfall
	// eligió DirectPlay/DirectStream.
	if burnSub != nil {
		if decision.Method == MethodDirectPlay {
			decision = Decide(item, mediaStreams, caps, profileName)
		}
		decision.Method = MethodTranscode
		decision.CopyVideo = false
	}

	if decision.Method == MethodDirectPlay {
		ms := &ManagedSession{
			Session: &Session{
				ID:        key,
				ItemID:    itemID,
				StartedAt: time.Now(),
			},
			UserID:       userID,
			Decision:     decision,
			LastAccessed: time.Now(),
		}
		return ms, nil
	}

	// Start transcode/remux session. Initial run starts at segment
	// index 0 to match a startTime of 0 (or the seek-from-resume
	// startTime — at which point segment numbering still begins at
	// segmentIndex(startTime), but the canonical first-play call
	// just passes 0 here and lets the synthesized manifest do the
	// rest).
	startSegment := 0
	if startTime > 0 {
		startSegment = int(startTime / 6) // matches -hls_time 6
	}
	session, err := m.transcoder.Start(key, itemID, TranscodeRequest{
		Input:              item.Path,
		Profile:            decision.Profile,
		StartTime:          startTime,
		CopyVideo:          decision.CopyVideo,
		CopyAudio:          decision.CopyAudio,
		ToneMap:            decision.ToneMap,
		StartSegmentNumber: startSegment,
		AudioStreamIndex:   audioStreamIndex,
		BurnSub:            burnSub,
	})
	if err != nil {
		m.metrics.TranscodeFailed()
		return nil, fmt.Errorf("start transcode: %w", err)
	}

	ms := &ManagedSession{
		Session:            session,
		UserID:             userID,
		InputPath:          item.Path,
		AudioStreamIndex:   audioStreamIndex,
		BurnSubtitle:       burnSub,
		Decision:           decision,
		LastAccessed:       time.Now(),
		LastRestartSegment: startSegment,
		LastRestartTime:    time.Now(),
	}

	m.mu.Lock()
	m.sessions[key] = ms
	active := len(m.sessions)
	m.mu.Unlock()

	m.metrics.TranscodeStarted()
	m.metrics.SetActiveSessions(active)

	m.publish(event.Event{
		Type: event.TranscodeStarted,
		Data: map[string]any{
			"session_id": key,
			"user_id":    userID,
			"item_id":    itemID,
			"profile":    decision.Profile.Name,
			"method":     string(decision.Method),
		},
	})

	m.logger.Info("session started",
		"key", key,
		"method", decision.Method,
		"profile", decision.Profile.Name,
	)

	return ms, nil
}

// restartCoalesceWindow is the ±N segment range within which a
// pending restart MAY be considered to "cover" a new request. Pair
// with restartCoalesceTimeWindow: BOTH conditions must hold for a
// new call to be coalesced.
//
// We size the gates against ONE specific pattern — hls.js's
// parallel-fanout burst on a single seek, which arrives within
// ~100 ms across the segment containing the seek + 1-2 prefill
// segments after it. Anything wider, and a second human click on
// a nearby spot 1-2 s later gets silently coalesced into the
// in-flight restart — the user sees ffmpeg keep producing from
// the FIRST seek's offset and the player never reaches their
// second target. Reported 2026-05-10: "click another minute, the
// player doesn't go there; click again, sometimes works".
//
// 2 segments AND 300 ms cover hls.js's actual fanout (~100 ms,
// 3 adjacent segments — the segment containing the seek + 2
// prefill) with a comfortable jitter margin, while staying
// well below human re-click reaction time (~500 ms minimum for
// "click, see no movement, click again").
const restartCoalesceWindow = 2
const restartCoalesceTimeWindow = 300 * time.Millisecond

// restartRateLimit caps RestartSessionAt invocations per session per
// minute. Healthy use lands well under this — a user dragging the
// seek bar with the SeekBar pointerup-commit pattern fires one seek
// per click; even a power user keyboard-scrubbing rarely tops 6/min.
// 20 leaves headroom while still detecting a runaway client.
const (
	restartRateLimitWindow = 60 * time.Second
	restartRateLimitMax    = 20
)

// RestartSessionAt stops the existing transcoder for `key` and
// re-starts it at the given segment index. This is the seek-restart
// path: the synthesized VOD manifest lists every segment up-front,
// so when the client asks for a far-future segment that ffmpeg
// hasn't produced yet, we restart ffmpeg at the corresponding offset
// (segIndex * segmentDuration) so the next segment file lands within
// a couple of seconds instead of after waiting for sequential
// encoding to catch up.
//
// Concurrent calls for the same session collapse onto a single
// restart via `ms.restartMu` plus a near-segment coverage check.
// hls.js fans out ~3 parallel segment requests on every seek; before
// this coalescing all three would tear down and respawn ffmpeg in
// turn, leaving two of the three processes orphaned and writing
// segments to the same cache dir — observable as "seek does
// nothing, htop shows N copies of ffmpeg with the same -ss".
//
// Existing segment files from the previous ffmpeg run remain on disk
// — useful for backwards seeks within an already-encoded range. The
// new ffmpeg run uses `-start_number = segIndex` so its produced
// files don't collide with the older ones.
//
// Returns ErrSessionNotFound if no session exists for the key
// (caller should fall back to a fresh StartSession).
func (m *Manager) RestartSessionAt(key string, segmentIndex int, segmentDuration float64) error {
	m.mu.Lock()
	ms, ok := m.sessions[key]
	m.mu.Unlock()
	if !ok {
		return ErrSessionNotFound
	}

	// Per-session lock: serialises restart work for THIS session
	// without holding the manager-wide m.mu (which would block
	// every other StreamManager operation for ~2 s while ffmpeg
	// shuts down).
	ms.restartMu.Lock()
	defer ms.restartMu.Unlock()

	// Coverage check, after the lock. Coalesce only when the previous
	// restart was BOTH (a) very recent in time AND (b) at a nearby
	// segment — that's the signature of hls.js's parallel-fanout
	// after a seek (3 adjacent segment fetches within ~100 ms) and
	// nothing else. A request that fails either gate is treated as
	// a fresh seek that deserves its own restart, even if it happens
	// to land near the previous one — humans clicking the bar twice
	// in adjacent regions used to fall into the trap and feel
	// "blocked" while ffmpeg encoded through the gap.
	delta := segmentIndex - ms.LastRestartSegment
	timeSinceRestart := time.Since(ms.LastRestartTime)
	timeRecent := !ms.LastRestartTime.IsZero() && timeSinceRestart < restartCoalesceTimeWindow
	segmentNear := delta >= 0 && delta <= restartCoalesceWindow
	if timeRecent && segmentNear {
		m.logger.Debug("seek restart coalesced into in-flight fanout",
			"key", key,
			"requested_segment", segmentIndex,
			"current_start_segment", ms.LastRestartSegment,
			"since_last_restart", timeSinceRestart,
		)
		return nil
	}

	// Sliding-window rate limit. The coalesce check above absorbs
	// the parallel-fan-out case (hls.js firing 2-3 adjacent segment
	// requests on every seek); this cap absorbs the SEQUENTIAL case
	// — a buggy client that keeps issuing far-apart seeks the user
	// never asked for. The 2026-05-07 incident registered 4 restarts
	// in 42 s for one human click; 20 in 60 s gives room for real
	// power-user scrubbing while still detecting that pattern.
	now := time.Now()
	if now.Sub(ms.restartWindowStart) > restartRateLimitWindow {
		ms.restartWindowStart = now
		ms.restartWindowCount = 0
	}
	ms.restartWindowCount++
	if ms.restartWindowCount > restartRateLimitMax {
		m.logger.Warn("restart rate limit hit — likely client-side seek loop",
			"key", key,
			"requested_segment", segmentIndex,
			"window_count", ms.restartWindowCount,
		)
		return ErrRestartRateLimited
	}

	// Stop the existing ffmpeg run. We keep the ManagedSession
	// (and its UserID, Decision, LastAccessed) so the cap/observer
	// state stays correct — only the underlying ffmpeg process
	// is torn down and replaced. We deliberately do NOT use
	// Session.Stop here because that would `os.RemoveAll` the
	// output dir; we want the previous run's segments to stay so
	// backwards seeks within the encoded prefix can still serve
	// from cache. (Session is embedded as *Session in
	// ManagedSession, so cancel / done resolve through field
	// promotion.)
	if ms.Session != nil && ms.cancel != nil {
		ms.cancel()
		select {
		case <-ms.done:
		case <-time.After(2 * time.Second):
		}
	}

	startTime := float64(segmentIndex) * segmentDuration
	newSession, err := m.transcoder.RestartAt(key, ms.ItemID, TranscodeRequest{
		Input:              ms.InputPath,
		Profile:            ms.Decision.Profile,
		StartTime:          startTime,
		CopyVideo:          ms.Decision.CopyVideo,
		CopyAudio:          ms.Decision.CopyAudio,
		ToneMap:            ms.Decision.ToneMap,
		StartSegmentNumber: segmentIndex,
		AudioStreamIndex:   ms.AudioStreamIndex,
		BurnSub:            ms.BurnSubtitle,
	})
	if err != nil {
		return fmt.Errorf("restart transcode at segment %d: %w", segmentIndex, err)
	}

	m.mu.Lock()
	ms.Session = newSession
	ms.LastAccessed = time.Now()
	ms.LastRestartSegment = segmentIndex
	ms.LastRestartTime = time.Now()
	m.mu.Unlock()

	m.logger.Info("session restarted at segment",
		"key", key,
		"segment", segmentIndex,
		"start_time", startTime,
	)
	return nil
}

// TouchSession updates the last accessed time for a session.
func (m *Manager) TouchSession(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ms, ok := m.sessions[key]; ok {
		ms.LastAccessed = time.Now()
	}
}

// StopSessionsByItem stops every session belonging to (userID, itemID),
// regardless of profile or audio stream index. Used by the player
// teardown DELETE so the client doesn't have to enumerate which
// (quality, audio) tuples ended up cached — a single request frees
// every active variant. Returns the count of sessions stopped.
//
// Without this, the per-user cap (MaxTranscodeSessionsPerUser) kept
// gathering zombie sessions across audio-language switches and
// quality flips, eventually returning 503 TranscodeBusy on every
// new playback. Also: hls.js routinely fans out to multiple variants
// during ABR probing, so a single playback can leave 4 sessions
// behind if only one (the active variant) is explicitly stopped.
func (m *Manager) StopSessionsByItem(userID, itemID string) int {
	m.mu.Lock()
	prefix := userID + ":" + itemID + ":"
	var keys []string
	for k := range m.sessions {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	m.mu.Unlock()
	for _, k := range keys {
		m.StopSession(k)
	}
	return len(keys)
}

// StopSession stops a specific session.
func (m *Manager) StopSession(key string) {
	m.mu.Lock()
	ms, ok := m.sessions[key]
	if ok {
		delete(m.sessions, key)
	}
	active := len(m.sessions)
	m.mu.Unlock()

	if ok {
		ms.Stop()
		m.metrics.SetActiveSessions(active)
		m.publish(event.Event{
			Type: event.TranscodeCompleted,
			Data: map[string]any{
				"session_id": key,
				"user_id":    ms.UserID,
				"item_id":    ms.ItemID,
			},
		})
		m.logger.Info("session stopped", "key", key)
	}
}

// GetSession returns a managed session by key.
func (m *Manager) GetSession(key string) (*ManagedSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ms, ok := m.sessions[key]
	if ok {
		ms.LastAccessed = time.Now()
	}
	return ms, ok
}

// ActiveSessions returns the count of active transcode sessions.
func (m *Manager) ActiveSessions() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// SessionSnapshot is the read-only view of an active session that the
// admin "Now Playing" panel consumes. Returned by value from
// ListAllSessions so the caller can serialise / iterate without
// re-acquiring the manager mutex and without aliasing back into live
// state. Field semantics mirror ManagedSession + its embedded
// Session, but all fields are plain values.
//
// ID is the manager's session map key (the same string StopSession
// accepts), not the embedded Session.ID — the two happen to match
// today, but pinning the API to the map key keeps the kill endpoint
// honest if the Session struct ever grows a separate identifier.
type SessionSnapshot struct {
	ID           string
	UserID       string
	ItemID       string
	Profile      string         // empty for non-transcode sessions
	Method       PlaybackMethod // DirectPlay / DirectStream / Transcode
	StartedAt    time.Time
	LastAccessed time.Time
}

// ListAllSessions returns a snapshot of every active session, taken
// under m.mu so the slice is internally consistent even while the
// manager is mutating elsewhere. Intended for the admin panel —
// callers that hold a single key (the player handler, the segment
// route) keep using GetSession.
//
// Iteration order is unspecified (Go map iteration); the admin
// frontend sorts by StartedAt descending for display.
func (m *Manager) ListAllSessions() []SessionSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]SessionSnapshot, 0, len(m.sessions))
	for key, ms := range m.sessions {
		snap := SessionSnapshot{
			ID:           key,
			UserID:       ms.UserID,
			Method:       ms.Decision.Method,
			LastAccessed: ms.LastAccessed,
		}
		if ms.Session != nil {
			// Field promotion via the embedded *Session — gofmt-safe and
			// matches the QF1008 staticcheck guidance.
			snap.ItemID = ms.ItemID
			snap.Profile = ms.Profile.Name
			snap.StartedAt = ms.StartedAt
		}
		out = append(out, snap)
	}
	return out
}

// MaxTranscodeSessions returns the concurrent transcode cap in
// effect after auto-tune + YAML + app_settings overrides. 0 means
// unlimited (an unusual operator choice but supported). Read by
// admin endpoints to render "X of Y in use".
func (m *Manager) MaxTranscodeSessions() int {
	return m.cfg.MaxTranscodeSessions
}

// MaxTranscodeSessionsPerUser returns the per-user transcode cap in
// effect after auto-tune + overrides. 0 means "no per-user cap".
// Surfaced to the admin panel so a saturation warning can explain
// whether the limit hit was the global pool or a single user
// soaking their slice.
func (m *Manager) MaxTranscodeSessionsPerUser() int {
	return m.cfg.MaxTranscodeSessionsPerUser
}

// TranscodePreset returns the libx264 -preset value in effect after
// auto-tune + overrides. Always non-empty post-construction (defaults
// to "veryfast" when nothing else applies). Used by the admin panel
// to display "Software preset: veryfast" alongside the HW status row.
func (m *Manager) TranscodePreset() string {
	return m.cfg.TranscodePreset
}

// HWAccelInfo returns the accelerator snapshot computed at construction.
// Zero-value when HW acceleration is disabled in config.
func (m *Manager) HWAccelInfo() HWAccelResult {
	return m.hwAccel
}

// HWAccelEnabled reports whether the operator turned HW acceleration on
// in config. Distinct from HWAccelInfo() because "enabled but no
// accelerators detected" is a different (and actionable) state from
// "disabled in config" — the admin panel renders different copy for each.
func (m *Manager) HWAccelEnabled() bool {
	return m.cfg.HWAccel.Enabled
}

// CacheDir returns the resolved transcode cache directory. Useful for the
// admin storage panel — same value the manager passes to the transcoder so
// the operator sees what's actually in use, not a stale config value.
func (m *Manager) CacheDir() string {
	return m.cfg.EffectiveCacheDir()
}

// Shutdown stops all sessions and the cleanup loop.
func (m *Manager) Shutdown() {
	close(m.stopClean)

	m.mu.Lock()
	sessions := make([]*ManagedSession, 0, len(m.sessions))
	for _, ms := range m.sessions {
		sessions = append(sessions, ms)
	}
	m.sessions = make(map[string]*ManagedSession)
	m.mu.Unlock()

	for _, ms := range sessions {
		ms.Stop()
	}
	m.metrics.SetActiveSessions(0)
	m.logger.Info("stream manager shut down", "stopped_sessions", len(sessions))
}

// cleanupLoop periodically removes idle sessions.
func (m *Manager) cleanupLoop() {
	idleTimeout := m.cfg.IdleTimeout
	if idleTimeout <= 0 {
		idleTimeout = 5 * time.Minute
	}

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopClean:
			return
		case <-ticker.C:
			m.cleanupIdle(idleTimeout)
		}
	}
}

func (m *Manager) cleanupIdle(maxIdle time.Duration) {
	now := time.Now()
	var toRemove []string
	var toStop []*ManagedSession

	m.mu.Lock()
	for key, ms := range m.sessions {
		if now.Sub(ms.LastAccessed) > maxIdle {
			toRemove = append(toRemove, key)
			toStop = append(toStop, ms)
			delete(m.sessions, key)
		}
	}
	active := len(m.sessions)
	m.mu.Unlock()

	for i, ms := range toStop {
		ms.Stop()
		m.transcoder.Stop(toRemove[i])
		m.logger.Info("cleaned up idle session", "key", toRemove[i])
	}

	if len(toStop) > 0 {
		m.metrics.SetActiveSessions(active)
	}
}
