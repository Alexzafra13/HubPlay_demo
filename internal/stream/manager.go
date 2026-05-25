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
// Manager.sessions es la vista logica (clave compuesta user+item+profile+audio+sub);
// Transcoder.sessions es la vista del proceso ffmpeg. Ambos mapas apuntan
// al mismo *Session — no hay duplicacion, solo dos perspectivas.
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
	bus        *event.Bus // nil-safe
	// startGroup: singleflight por session key para colapsar inicios concurrentes.
	startGroup singleflight.Group
	// hwAccel: snapshot de deteccion al arranque; leido por admin stats.
	hwAccel HWAccelResult
	// forceDirectPlayLookup: hook a runtime settings para el toggle admin
	// playback.force_direct_play. Nil-safe.
	forceDirectPlayLookup func(context.Context) bool
}

// ManagedSession envuelve una Session con tracking de acceso y contexto de usuario.
type ManagedSession struct {
	*Session
	UserID           string
	InputPath        string            // ruta fuente; cacheada para evitar re-query en seeks
	AudioStreamIndex int               // per-type index de audio; -1 = auto-pick
	BurnSubtitle     *BurnSubtitleSpec // nil = sin burn-in
	Decision         PlaybackDecision
	LastAccessed     time.Time

	// restartMu: mutex por-sesion para serializar restarts sin bloquear m.mu.
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

	Metrics               MetricsSink                // nil = noopSink
	EventBus              *event.Bus                 // nil = sin eventos
	ForceDirectPlayLookup func(context.Context) bool // nil = algoritmo estandar
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
		items:     items,
		streams:   streams,
		cfg:       cfg,
		logger:    logger.With("module", "stream-manager"),
		stopClean: make(chan struct{}),
		metrics:   noopSink{},
		hwAccel:   hwResult,
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

// SetMetrics conecta un sink de metricas. Nil = no-op.
func (m *Manager) SetMetrics(sink MetricsSink) {
	if sink == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metrics = sink
	m.metrics.SetActiveSessions(len(m.sessions))
}

// SetEventBus conecta un bus de eventos. Nil deshabilita publicacion.
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

// sessionKey: clave compuesta user+item+profile+audio+sub. Los indices
// forman parte de la clave porque un cambio de audio o subtitle quemado
// requiere una sesion de transcode distinta.
func sessionKey(userID, itemID, profile string, audioStreamIndex, burnSubIndex int) string {
	return userID + ":" + itemID + ":" + profile +
		":" + strconv.Itoa(audioStreamIndex) +
		":" + strconv.Itoa(burnSubIndex)
}

// SessionKey es el constructor canonico exportado de clave de sesion.
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

// StartSessionRequest agrupa los parametros de StartSession.
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

// StartSession crea o reutiliza una sesion existente. Llamadas
// concurrentes para la misma clave colapsan via singleflight.
func (m *Manager) StartSession(ctx context.Context, req StartSessionRequest) (*ManagedSession, error) {
	key := req.sessionKey()

	// Fast path: sesion ya activa.
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

	// Re-check: un Do previo pudo completar entre el fast-path miss y aqui.
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

	// Burn-in necesita decoded frames; forzar Transcode si la waterfall
	// eligio DirectPlay/DirectStream.
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

// Ventana de coalescencia: colapsa el fanout paralelo de hls.js (3
// segmentos adyacentes en ~100ms) sin tragarse un segundo seek humano.
const restartCoalesceWindow = 2
const restartCoalesceTimeWindow = 300 * time.Millisecond

const (
	restartRateLimitWindow = 60 * time.Second
	restartRateLimitMax    = 20 // suficiente para power-user; detecta runaway client
)

// RestartSessionAt reinicia ffmpeg en el segmento dado (seek-restart).
// Llamadas concurrentes para la misma sesion se coalescen via restartMu.
// Los segmentos previos se conservan en disco. Devuelve ErrSessionNotFound
// si la sesion no existe.
func (m *Manager) RestartSessionAt(key string, segmentIndex int, segmentDuration float64) error {
	m.mu.Lock()
	ms, ok := m.sessions[key]
	m.mu.Unlock()
	if !ok {
		return ErrSessionNotFound
	}

	ms.restartMu.Lock()
	defer ms.restartMu.Unlock()

	// Coalescencia: solo colapsar si el restart previo fue MUY reciente
	// Y en un segmento cercano (firma del fanout de hls.js).
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

	// Rate limit sliding-window por sesion.
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

	// Cancelar ffmpeg actual sin borrar segmentos (NO usar Session.Stop
	// que haria RemoveAll del outputDir).
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

func (m *Manager) TouchSession(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ms, ok := m.sessions[key]; ok {
		ms.LastAccessed = time.Now()
	}
}

// StopSessionsByItem detiene todas las sesiones de (userID, itemID)
// independientemente del profile o audio index. Evita zombies en el
// cap per-user tras cambios de calidad/idioma.
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

func (m *Manager) GetSession(key string) (*ManagedSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ms, ok := m.sessions[key]
	if ok {
		ms.LastAccessed = time.Now()
	}
	return ms, ok
}

func (m *Manager) ActiveSessions() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// SessionSnapshot vista read-only de una sesion activa para el panel admin.
type SessionSnapshot struct {
	ID           string
	UserID       string
	ItemID       string
	Profile      string
	Method       PlaybackMethod
	StartedAt    time.Time
	LastAccessed time.Time
}

// ListAllSessions devuelve un snapshot de todas las sesiones activas.
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
			snap.ItemID = ms.ItemID
			snap.Profile = ms.Profile.Name
			snap.StartedAt = ms.StartedAt
		}
		out = append(out, snap)
	}
	return out
}

func (m *Manager) MaxTranscodeSessions() int {
	return m.cfg.MaxTranscodeSessions
}

func (m *Manager) MaxTranscodeSessionsPerUser() int {
	return m.cfg.MaxTranscodeSessionsPerUser
}

func (m *Manager) TranscodePreset() string {
	return m.cfg.TranscodePreset
}

func (m *Manager) HWAccelInfo() HWAccelResult {
	return m.hwAccel
}

// HWAccelEnabled indica si el operador activo HW accel en config.
// Distinto de HWAccelInfo(): "enabled pero sin aceleradores" es un
// estado diferente de "disabled en config".
func (m *Manager) HWAccelEnabled() bool {
	return m.cfg.HWAccel.Enabled
}

func (m *Manager) CacheDir() string {
	return m.cfg.EffectiveCacheDir()
}

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
