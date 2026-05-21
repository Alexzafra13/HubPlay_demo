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

// MetricsSink is the minimal observability surface the Manager uses. Keeping
// it local (instead of importing an observability package) avoids a package
// cycle and lets tests pass a nil-safe sink without Prometheus in the mix.
type MetricsSink interface {
	TranscodeStarted()
	TranscodeBusy()
	TranscodeFailed()
	SetActiveSessions(n int)
}

// noopSink is the default implementation used when no metrics are wired in.
// Using a value type with empty methods lets the Manager call metrics.* on
// every hot path without nil checks.
type noopSink struct{}

func (noopSink) TranscodeStarted()       {}
func (noopSink) TranscodeBusy()          {}
func (noopSink) TranscodeFailed()        {}
func (noopSink) SetActiveSessions(n int) {}

// Manager orchestrates streaming sessions (direct play, remux, and
// transcode).
//
// Responsabilidad respecto a `Transcoder` (cierra parcialmente el olor
// LL del audit 2026-05-14, "doble session tracking"):
//
//   - **Manager.sessions** mapa keyed por `sessionKey(userID, itemID,
//     profile, audioIdx, subIdx)` — clave compuesta de la sesión
//     LÓGICA del usuario. El valor es `*ManagedSession`, que envuelve
//     un `*Session` raw y añade contexto de usuario, decisión de
//     playback, mutex de restart por-sesión y trickle de
//     LastAccessed. Es la API pública: handlers cliente entran aquí.
//
//   - **Transcoder.sessions** mapa keyed por `sessionID` bare — clave
//     del proceso ffmpeg físico. El valor es `*Session` con cmd,
//     cancel, done — control del proceso. Es interno al paquete.
//
// Los dos mapas apuntan al MISMO `*Session` por debajo (ManagedSession
// embed un puntero a Session) — no hay duplicación de estado, sólo dos
// vistas con propósitos distintos. La auditoría sugirió hacer el
// Transcoder stateless (tracking solo en Manager) pero eso implica
// migrar cmd/cancel/done a ManagedSession + reescribir Start +
// RestartSessionAt + StopSession, deferred como sesión propia.
type Manager struct {
	mu         sync.Mutex
	sessions   map[string]*ManagedSession
	transcoder *Transcoder
	items      *db.ItemRepository
	streams    *db.MediaStreamRepository
	cfg        config.StreamingConfig
	logger     *slog.Logger
	stopClean  chan struct{}
	metrics    MetricsSink
	bus        *event.Bus // optional; nil-safe
	// startGroup serialises StartSession's slow path per session
	// key. Two parallel callers for the same userID:itemID:profile
	// (player init + an immediate auth-retry burst, a double-clicked
	// Play, hls.js requesting the manifest while the page is still
	// mounting, etc.) used to BOTH miss the m.sessions fast-path
	// lookup and BOTH reach transcoder.Start, leaving two ffmpegs
	// alive simultaneously and writing segments to the same cache
	// dir. singleflight collapses the racers onto a single execution;
	// late joiners receive the same ManagedSession the winner built.
	startGroup singleflight.Group
	// hwAccel is the snapshot of accelerator detection done at startup.
	// Cached here so the admin /system/stats endpoint can read it without
	// re-running ffmpeg on every poll. Zero value means "no detection
	// performed" (HWAccel.Enabled = false in config).
	hwAccel HWAccelResult
	// forceDirectPlayLookup is the optional hook into runtime settings
	// for `playback.force_direct_play`. When set and the lookup
	// returns true, the manager skips the capability waterfall and
	// returns a DirectPlay decision — the admin has vouched that
	// every client can decode every file. Nil-safe: when the hook
	// isn't wired (tests, deployments without runtime settings) the
	// normal Decide() path runs unchanged.
	forceDirectPlayLookup func(context.Context) bool
}

// ManagedSession wraps a transcoding session with access tracking.
type ManagedSession struct {
	*Session
	UserID string
	// InputPath is the absolute file path of the source media. We
	// cache it here so RestartSessionAt doesn't have to re-query the
	// items repository every time the user seeks to an unencoded
	// region — the path is immutable for the life of the session.
	InputPath string
	// AudioStreamIndex is the per-type audio track ffmpeg should
	// pick (0-based, mapped from the user's preferred-language /
	// in-player switcher). -1 = let ffmpeg auto-pick the file's
	// default audio. Cached on the session so RestartSessionAt
	// keeps the same dub across seeks.
	AudioStreamIndex int
	// BurnSubtitle is the subtitle stream burned into the encoded
	// video frames (PGS / DVDSUB / ASS). Nil = no burn-in, the
	// player either has no sub or relies on a native HLS sub track.
	// Cached on the session so RestartSessionAt keeps the chosen
	// subtitle across seeks — without this, a seek would restart
	// ffmpeg without the filter graph and the next segments would
	// drop the burned subtitle silently.
	BurnSubtitle *BurnSubtitleSpec
	Decision     PlaybackDecision
	LastAccessed time.Time

	// restartMu serialises RestartSessionAt for THIS session. The
	// outer m.mu only guards the sessions map; the actual ffmpeg
	// cancel + spawn is per-session work that can take ~2 s, so
	// holding m.mu through it would freeze every other StreamManager
	// caller (StartSession, segment handler, the cleanup loop) for
	// the duration. A per-session mutex isolates the cost while
	// still preventing the racing-restart bug where hls.js fires
	// several parallel segment requests after a seek and each one
	// triggers its own restart, orphaning ffmpegs.
	restartMu sync.Mutex
	// LastRestartSegment is the segment index of the most recent
	// successful Start / RestartAt for this session — i.e. the
	// `-start_number` value the currently-running ffmpeg was given.
	// Paired with LastRestartTime by the coalesce check.
	LastRestartSegment int
	// LastRestartTime is the wall-clock moment of the most recent
	// successful Start / RestartAt. The coalesce window in
	// RestartSessionAt requires BOTH (a) recent in time AND
	// (b) nearby in segment number — so a parallel-fanout burst
	// from hls.js (3 adjacent segment requests fired within 100 ms
	// of each other) collapses onto one ffmpeg, but a SECOND human
	// click 5 s later that happens to land near the first still
	// gets its own restart instead of waiting for ffmpeg to encode
	// linearly through the gap.
	LastRestartTime time.Time
	// restartWindowStart / restartWindowCount form a per-session
	// sliding-window rate limiter for RestartSessionAt. Defense in
	// depth against a frontend regression that fires seek events
	// the user did not request — observed 2026-05-07 with a
	// +366-segment cadence in the server logs for one user click.
	// The pointerup-commit fix in the SeekBar should keep this from
	// triggering under normal use; if it does, it's a signal of a
	// new client bug and we'd rather refuse the restart than melt
	// the transcoder.
	restartWindowStart time.Time
	restartWindowCount int
}

// ErrRestartRateLimited is returned by RestartSessionAt when a
// session exceeds the per-minute cap. The handler maps it to 429
// so the client backs off; under healthy use this never fires.
var ErrRestartRateLimited = errors.New("stream: restart rate limit exceeded")

// ErrSessionNotFound is returned by RestartSessionAt / TouchSession
// when the caller references a key that has no live session. The
// handler converts this into a 404 so the client falls back to a
// fresh StartSession instead of looping on a dead key.
var ErrSessionNotFound = errors.New("stream: session not found")

// Deps agrupa las dependencias de `NewManager`. Los 4 primeros campos
// son obligatorios (Items, Streams, Config, Logger); los 3 restantes
// (Metrics, EventBus, ForceDirectPlayLookup) son opcionales y reemplazan
// a los antiguos setters `SetMetrics` / `SetEventBus` /
// `SetForceDirectPlayLookup` que el composition root encadenaba. Los
// setters siguen existiendo para tests (swap de stub a runtime, ver
// `manager_test.go::TestManager_SetMetrics_*`) pero la wiring de
// producción usa Deps — un único call site, sin Builder Pattern
// accidental. Cierra olor JJ del audit 2026-05-14.
type Deps struct {
	Items   *db.ItemRepository
	Streams *db.MediaStreamRepository
	Config  config.StreamingConfig
	Logger  *slog.Logger

	// Metrics — sink de observability. Nil deja el noopSink por defecto.
	Metrics MetricsSink
	// EventBus — publish de eventos de ciclo de vida del transcoder
	// (TranscodeStarted / TranscodeCompleted). Nil deshabilita.
	EventBus *event.Bus
	// ForceDirectPlayLookup — hook a app_settings para el toggle
	// admin `playback.force_direct_play`. El closure se llama en
	// cada `StartSession`, así que el flag es mutable runtime sin
	// reiniciar. Nil deja la decisión 100% en el algoritmo estándar.
	ForceDirectPlayLookup func(context.Context) bool
}

// NewManager creates a streaming manager.
func NewManager(deps Deps) *Manager {
	items := deps.Items
	streams := deps.Streams
	cfg := deps.Config
	logger := deps.Logger
	// Single source of truth for the cache directory — preflight checks
	// use the same helper so "the cache dir" means the same thing in both
	// places.
	cacheDir := cfg.EffectiveCacheDir()

	// Detect hardware acceleration once at construction. Detection is
	// fast (< 50 ms on a warm system) and the result is read on every
	// transcode session, so doing it inline here keeps the wiring at
	// a single point. When `Enabled = false` we skip detection
	// entirely and fall back to libx264 — matches a deliberately-
	// configured "force software" deployment.
	hwAccel := HWAccelNone
	encoder := "libx264"
	hwResult := HWAccelResult{Selected: HWAccelNone, Encoder: "libx264"}
	if cfg.HWAccel.Enabled {
		hwResult = DetectHWAccel(cfg.HWAccel.Preferred, logger)
		hwAccel = hwResult.Selected
		encoder = hwResult.Encoder
	}

	// Auto-tune zero/empty knobs in cfg with hardware-aware defaults.
	// Runs AFTER detection so the recommendation is matched to the
	// accelerator that's actually going to do the work; runs AFTER
	// the caller has applied YAML + app_settings overrides so any
	// explicit operator value (in cfg already) survives unchanged.
	//
	// runtime.NumCPU() reads the cgroup-aware CPU limit on Linux
	// (Go 1.18+), so a container with --cpus=2 sees 2 here even on
	// a 32-core host — auto-tune scales to the budget the operator
	// gave the process, not the underlying hardware.
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
		sessions:   make(map[string]*ManagedSession),
		transcoder: NewTranscoder(cacheDir, "", cfg.TranscodeTimeout, hwAccel, encoder, cfg.TranscodePreset, logger),
		items:      items,
		streams:    streams,
		cfg:        cfg,
		logger:     logger.With("module", "stream-manager"),
		stopClean:  make(chan struct{}),
		metrics:    noopSink{},
		hwAccel:    hwResult,
	}

	// Wiring atómico de los hooks opcionales — sustituye a los 3
	// setters post-construcción que main.go encadenaba (olor JJ del
	// audit). Los setters siguen existiendo para tests; producción
	// pasa todo en Deps y NewManager devuelve un Manager listo para
	// servir sin call sites adicionales.
	if deps.Metrics != nil {
		m.metrics = deps.Metrics
		// Mirror del contrato documentado en SetMetrics: el sink ve
		// el gauge inicial (0 al arrancar) en vez de tener que esperar
		// al primer StartSession para emitir un valor.
		m.metrics.SetActiveSessions(0)
	}
	m.bus = deps.EventBus
	m.forceDirectPlayLookup = deps.ForceDirectPlayLookup

	go m.cleanupLoop()
	return m
}

// SetMetrics wires an observability sink into the manager. Passing nil is a
// no-op (the default noopSink stays in place) so callers never have to
// short-circuit in production code.
func (m *Manager) SetMetrics(sink MetricsSink) {
	if sink == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metrics = sink
	m.metrics.SetActiveSessions(len(m.sessions))
}

// SetEventBus wires an event bus so the manager can publish lifecycle events
// (TranscodeStarted / TranscodeCompleted). Passing nil disables publishing.
// Follows the SetMetrics pattern so the constructor signature stays stable.
func (m *Manager) SetEventBus(bus *event.Bus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bus = bus
}

// publish sends an event if a bus is wired. Reads m.bus under the mutex to
// stay race-free with SetEventBus.
func (m *Manager) publish(e event.Event) {
	m.mu.Lock()
	bus := m.bus
	m.mu.Unlock()
	if bus != nil {
		bus.Publish(e)
	}
}

// sessionKey builds a unique key for a user+item+profile+audio+sub
// combo. Index fields are part of the key so a mid-playback switch
// (audio dub or burned-in subtitle) creates a fresh session instead
// of the manager handing back the previous transcode unchanged.
// -1 on either index collapses to "default" so cache hits for
// callers that don't care about that dimension stay intact.
//
// Why subtitle index belongs in the key: burned-in subtitles are
// baked into the encoded segments, so toggling the subtitle later
// MUST produce a different transcode. Without this, the manager
// would return the old session and the player would see frames
// with the previous subtitle still rendered on them.
func sessionKey(userID, itemID, profile string, audioStreamIndex, burnSubIndex int) string {
	return userID + ":" + itemID + ":" + profile +
		":" + strconv.Itoa(audioStreamIndex) +
		":" + strconv.Itoa(burnSubIndex)
}

// SessionKey is the exported canonical key builder. Handlers must use
// this rather than concatenating fields by hand — the format includes
// the audio stream index AND the burn-in subtitle index, and a
// hand-rolled "user:item:profile" key silently misses every session
// the manager actually registered (404 on every segment fetch).
func SessionKey(userID, itemID, profile string, audioStreamIndex, burnSubIndex int) string {
	return sessionKey(userID, itemID, profile, audioStreamIndex, burnSubIndex)
}

// SetForceDirectPlayLookup wires the runtime-settings hook for the
// `playback.force_direct_play` admin toggle. main.go calls this
// after both the Manager and the Settings repo are constructed.
// Nil-safe and idempotent — call again to swap implementations
// (e.g. swapping a test stub for the real settings repo).
func (m *Manager) SetForceDirectPlayLookup(fn func(context.Context) bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.forceDirectPlayLookup = fn
}

// shouldForceDirectPlay returns the runtime value of the
// `playback.force_direct_play` admin toggle. Falls through to false
// when no lookup is wired.
func (m *Manager) shouldForceDirectPlay(ctx context.Context) bool {
	m.mu.Lock()
	fn := m.forceDirectPlayLookup
	m.mu.Unlock()
	if fn == nil {
		return false
	}
	return fn(ctx)
}

// StartSession creates or returns an existing session for the given item.
//
// `caps` is the client's declared codec/container capabilities (parsed
// from the X-Hubplay-Client-Capabilities header). Pass nil for unknown
// clients — the playback Decide() falls back to web-browser defaults.
// The capabilities affect the DirectPlay/DirectStream/Transcode
// waterfall: a Kotlin TV app declaring HEVC + EAC3 + MKV gets
// DirectPlay where today's hard-coded defaults forced a Transcode.
//
// Concurrent calls for the same key collapse onto a single ffmpeg
// spawn via `m.startGroup` (singleflight). Without this, the gap
// between releasing m.mu (after the cap checks) and writing the
// completed session back into m.sessions was large enough — items
// fetch + streams fetch + transcoder.Start, hundreds of milliseconds
// in production — that two HTTP requests landing within the same
// player init burst could both miss the fast-path lookup and both
// drive the slow path to completion. transcoder.Start serialises on
// its own mutex but its existing-session check uses the same map,
// so the second caller's existing.Stop() race-killed the first
// caller's just-spawned ffmpeg. The visible symptom was two ffmpegs
// at 99% CPU writing segments to the same cache dir, fighting over
// segmentNNNNN.ts. singleflight makes the slow path single-flight
// per key; late joiners receive the same ManagedSession the winner
// produced.
// StartSession's burnSubtitleIndex argument:
//   - < 0 → no burn-in. The player either has no subtitle picked or
//     is consuming an HLS-native sub track (text format, sidecar).
//   - >= 0 → burn the subtitle at this per-type index. The manager
//     resolves the codec from the item's MediaStream rows and decides
//     between the overlay (bitmap) and the subtitles= (ASS/SSA)
//     filter strategy at ffmpeg-arg time.
//
// Burn-in forces a Transcode decision regardless of what the
// capability waterfall would have picked — there's no decoded frame
// to composite onto in DirectPlay / DirectStream paths.

// StartSessionRequest agrupa los parámetros de `Manager.StartSession`
// y `Manager.startSessionSlow`. Cierra el olor F14-2-b del audit
// 2026-05-14 (firmas de 8 y 9 params posicionales). Permite añadir/
// renombrar params sin tocar callers ni la `StreamManagerService`
// interface que consumen los handlers.
//
// `BurnSubIndex`:
//   - < 0 → no burn-in. El player o no tiene sub seleccionado o
//     está consumiendo una HLS-native sub track (text format,
//     sidecar).
//   - >= 0 → burn-in del subtítulo en este per-type index. El
//     manager resuelve el codec desde las MediaStream rows del item
//     y decide entre overlay (bitmap) o subtitles= (ASS/SSA) en
//     ffmpeg-arg time.
//
// Burn-in fuerza una decision Transcode independientemente de lo
// que el capability waterfall haya elegido — no hay decoded frame
// que componer en los paths DirectPlay/DirectStream.
type StartSessionRequest struct {
	UserID           string
	ItemID           string
	ProfileName      string
	Caps             *Capabilities
	StartTime        float64
	AudioStreamIndex int
	BurnSubIndex     int
}

// sessionKey deriva la clave canónica de la sesión desde los campos
// identitarios del request (sin StartTime ni Caps, que no son
// identidad). Centralizar la derivación garantiza que el fast-path
// lookup y el singleflight admission usen el mismo string.
func (r StartSessionRequest) sessionKey() string {
	return sessionKey(r.UserID, r.ItemID, r.ProfileName, r.AudioStreamIndex, r.BurnSubIndex)
}

func (m *Manager) StartSession(ctx context.Context, req StartSessionRequest) (*ManagedSession, error) {
	key := req.sessionKey()

	// Fast path: already-running session bypasses the singleflight
	// and the slow-path setup entirely. This is the >99% case once
	// the player is past its init burst — every subsequent segment
	// request for the session.
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

// startSessionSlow runs the actual fetch + decide + ffmpeg spawn.
// Wrapped by `m.startGroup.Do` so concurrent callers for the same
// `key` collapse onto one execution. The first caller's `ctx` drives
// the work — if it cancels mid-fetch, late joiners get the same
// error, which is the right trade: callers who arrived 50 ms apart
// for the same key were going to share the result anyway.
func (m *Manager) startSessionSlow(ctx context.Context, key string, req StartSessionRequest) (*ManagedSession, error) {
	// Unpack a local view del req así que el resto del body (90+
	// LoC) no tiene que llamar req.X cada vez. Las variables son
	// puro alias — el behaviour del cuerpo no cambia con el
	// refactor F14-2-b.
	userID := req.UserID
	itemID := req.ItemID
	profileName := req.ProfileName
	caps := req.Caps
	startTime := req.StartTime
	audioStreamIndex := req.AudioStreamIndex
	burnSubIndex := req.BurnSubIndex
	// Re-check after singleflight admission: a previous Do for this
	// key may have just finished and populated m.sessions in the
	// brief window between this caller's fast-path miss and its
	// arrival here. singleflight collapses *concurrent* calls; it
	// doesn't deduplicate a fresh call against a finished one.
	m.mu.Lock()
	if ms, ok := m.sessions[key]; ok {
		ms.LastAccessed = time.Now()
		m.mu.Unlock()
		return ms, nil
	}

	// Global concurrent limit.
	if m.cfg.MaxTranscodeSessions > 0 && len(m.sessions) >= m.cfg.MaxTranscodeSessions {
		active := len(m.sessions)
		m.mu.Unlock()
		m.metrics.TranscodeBusy()
		return nil, domain.NewTranscodeBusy(active, m.cfg.MaxTranscodeSessions)
	}

	// Per-user cap. Without this a single user fanning out to many
	// items / qualities (or repeatedly re-clicking Play during a
	// flaky network) can soak up the whole global pool. The check
	// runs while holding the manager mutex so concurrent StartSession
	// calls from the same user can't both squeeze past it.
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

	// Fetch item and its streams
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

	// Resolve the subtitle-burn-in spec from the requested index.
	// Find the Nth subtitle stream (per-type ordering, matching the
	// audio convention) and capture its codec so BuildFFmpegArgs can
	// choose between filter_complex overlay (bitmap) and the
	// subtitles= filter (ASS / SSA). Unknown / out-of-range index =
	// no-op (nil) so a stale client URL doesn't fail the start —
	// the player just doesn't get its subtitle this time.
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

	// Burn-in needs decoded frames to overlay onto. If the waterfall
	// picked DirectPlay or DirectStream (CopyVideo=true), upgrade to
	// a full re-encode — same trade-off Plex / Jellyfin make: video
	// re-encode cost in exchange for the user actually seeing the
	// subtitle. Audio decision stays untouched.
	if burnSub != nil {
		if decision.Method == MethodDirectPlay {
			// Forcing DirectStream here would still copy video; jump
			// straight to Transcode. We keep the streams metadata so
			// the rest of the path is identical to a regular transcode.
			decision = Decide(item, mediaStreams, caps, profileName)
		}
		decision.Method = MethodTranscode
		decision.CopyVideo = false
	}

	// Direct play doesn't need a transcode session
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
		// No lock needed — direct play sessions are not tracked
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
