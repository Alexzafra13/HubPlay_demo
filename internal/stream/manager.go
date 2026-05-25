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
	librarymodel "hubplay/internal/library/model"
)

// MetricsSink es la superficie mínima de observability del Manager.
// Interfaz local para evitar ciclo de paquetes con observability.
type MetricsSink interface {
	TranscodeStarted()
	TranscodeBusy()
	TranscodeFailed()
	SetActiveSessions(n int)
}

// noopSink es la implementación por defecto cuando no hay métricas.
type noopSink struct{}

func (noopSink) TranscodeStarted()       {}
func (noopSink) TranscodeBusy()          {}
func (noopSink) TranscodeFailed()        {}
func (noopSink) SetActiveSessions(n int) {}

// Manager orquesta sesiones de streaming (direct play, remux, transcode).
//
// Manager.sessions usa clave compuesta (sessionKey) — vista lógica por
// usuario. Transcoder.sessions usa sessionID bare — control del proceso
// ffmpeg. Ambos mapas apuntan al mismo *Session subyacente.
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
	bus        *event.Bus // opcional; nil-safe
	// startGroup serializa el slow path de StartSession por session key.
	// Colapsa racers paralelos (init burst, double-click, hls.js mount)
	// en una sola ejecución vía singleflight.
	startGroup singleflight.Group
	// hwAccel es el snapshot de detección de acelerador al arranque.
	hwAccel HWAccelResult
	// forceDirectPlayLookup es el hook opcional a runtime settings para
	// `playback.force_direct_play`. Nil-safe.
	forceDirectPlayLookup func(context.Context) bool
}

// ManagedSession envuelve una sesión de transcoding con tracking de acceso.
type ManagedSession struct {
	*Session
	UserID string
	// InputPath es la ruta absoluta del fichero fuente. Cacheada para
	// que RestartSessionAt no re-consulte el repo de items.
	InputPath string
	// AudioStreamIndex es la pista de audio (0-based, per-type) que
	// ffmpeg debe usar. -1 = auto-pick del fichero.
	AudioStreamIndex int
	// BurnSubtitle es el subtítulo quemado en el video (PGS/DVDSUB/ASS).
	// Nil = sin burn-in.
	BurnSubtitle *BurnSubtitleSpec
	Decision     PlaybackDecision
	LastAccessed time.Time

	// restartMu serializa RestartSessionAt POR sesión. El outer m.mu
	// solo protege el map; el trabajo ffmpeg cancel+spawn es per-sesión
	// (~2s) y no debe bloquear otros callers del Manager.
	restartMu sync.Mutex
	// LastRestartSegment/Time controlan la ventana de coalesce para
	// evitar restarts duplicados del fanout de hls.js.
	LastRestartSegment int
	LastRestartTime    time.Time
	// Rate limiter sliding-window per-sesión para RestartSessionAt.
	// Defensa contra regresiones de frontend que disparen seeks que
	// el usuario no pidió.
	restartWindowStart time.Time
	restartWindowCount int
}

// ErrRestartRateLimited se devuelve cuando una sesión excede el cap
// por minuto. El handler lo mapea a 429.
var ErrRestartRateLimited = errors.New("stream: restart rate limit exceeded")

// ErrSessionNotFound se devuelve cuando el caller referencia una key
// sin sesión viva. El handler lo convierte a 404.
var ErrSessionNotFound = errors.New("stream: session not found")

// Deps agrupa las dependencias de NewManager. Items, Streams, Config
// y Logger son obligatorios; Metrics, EventBus y ForceDirectPlayLookup
// son opcionales.
type Deps struct {
	Items   *db.ItemRepository
	Streams *db.MediaStreamRepository
	Config  config.StreamingConfig
	Logger  *slog.Logger

	Metrics               MetricsSink
	EventBus              *event.Bus
	ForceDirectPlayLookup func(context.Context) bool
}

// NewManager crea un streaming manager.
func NewManager(deps Deps) *Manager {
	items := deps.Items
	streams := deps.Streams
	cfg := deps.Config
	logger := deps.Logger
	cacheDir := cfg.EffectiveCacheDir()

	// Detección de HW una vez en construcción. Rápida (<50ms) y el
	// resultado se lee en cada sesión de transcode.
	hwAccel := HWAccelNone
	encoder := "libx264"
	hwResult := HWAccelResult{Selected: HWAccelNone, Encoder: "libx264"}
	if cfg.HWAccel.Enabled {
		hwResult = DetectHWAccel(cfg.HWAccel.Preferred, logger)
		hwAccel = hwResult.Selected
		encoder = hwResult.Encoder
	}

	// Auto-tune después de detección para que la recomendación se
	// ajuste al acelerador real. runtime.NumCPU() lee el límite cgroup.
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

// SetMetrics conecta un sink de observability. Nil es no-op.
func (m *Manager) SetMetrics(sink MetricsSink) {
	if sink == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metrics = sink
	m.metrics.SetActiveSessions(len(m.sessions))
}

// SetEventBus conecta un bus de eventos para publicar ciclo de vida
// del transcoder. Nil deshabilita publicación.
func (m *Manager) SetEventBus(bus *event.Bus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bus = bus
}

// publish envía un evento si hay bus conectado.
func (m *Manager) publish(e event.Event) {
	m.mu.Lock()
	bus := m.bus
	m.mu.Unlock()
	if bus != nil {
		bus.Publish(e)
	}
}

// sessionKey construye una clave única para la combinación
// user+item+profile+audio+sub. Los índices forman parte de la clave
// para que un switch mid-playback cree una sesión nueva.
func sessionKey(userID, itemID, profile string, audioStreamIndex, burnSubIndex int) string {
	return userID + ":" + itemID + ":" + profile +
		":" + strconv.Itoa(audioStreamIndex) +
		":" + strconv.Itoa(burnSubIndex)
}

// SessionKey es el constructor canónico exportado de clave de sesión.
func SessionKey(userID, itemID, profile string, audioStreamIndex, burnSubIndex int) string {
	return sessionKey(userID, itemID, profile, audioStreamIndex, burnSubIndex)
}

// SetForceDirectPlayLookup conecta el hook de runtime-settings para
// el toggle admin `playback.force_direct_play`. Nil-safe e idempotente.
func (m *Manager) SetForceDirectPlayLookup(fn func(context.Context) bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.forceDirectPlayLookup = fn
}

// shouldForceDirectPlay devuelve el valor runtime del toggle admin.
func (m *Manager) shouldForceDirectPlay(ctx context.Context) bool {
	m.mu.Lock()
	fn := m.forceDirectPlayLookup
	m.mu.Unlock()
	if fn == nil {
		return false
	}
	return fn(ctx)
}

// StartSessionRequest agrupa los parámetros de StartSession y
// startSessionSlow. BurnSubIndex < 0 = sin burn-in; >= 0 = burn-in
// del subtítulo en ese per-type index.
type StartSessionRequest struct {
	UserID           string
	ItemID           string
	ProfileName      string
	Caps             *Capabilities
	StartTime        float64
	AudioStreamIndex int
	BurnSubIndex     int
}

// sessionKey deriva la clave canónica desde los campos identitarios.
func (r StartSessionRequest) sessionKey() string {
	return sessionKey(r.UserID, r.ItemID, r.ProfileName, r.AudioStreamIndex, r.BurnSubIndex)
}

// StartSession crea o devuelve una sesión existente para el item dado.
// Llamadas concurrentes para la misma key colapsan en un solo spawn
// de ffmpeg vía singleflight.
func (m *Manager) StartSession(ctx context.Context, req StartSessionRequest) (*ManagedSession, error) {
	key := req.sessionKey()

	// Fast path: sesión ya activa (>99% de las veces post-init).
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

// startSessionSlow ejecuta el fetch + decide + spawn de ffmpeg.
// Envuelto por singleflight para colapsar callers concurrentes.
func (m *Manager) startSessionSlow(ctx context.Context, key string, req StartSessionRequest) (*ManagedSession, error) {
	userID := req.UserID
	itemID := req.ItemID
	profileName := req.ProfileName
	caps := req.Caps
	startTime := req.StartTime
	audioStreamIndex := req.AudioStreamIndex
	burnSubIndex := req.BurnSubIndex

	// Re-check tras admisión singleflight.
	if ms := m.tryGetExistingSession(key); ms != nil {
		return ms, nil
	}

	// Checks de capacidad global y per-user.
	if err := m.checkSessionCaps(userID); err != nil {
		return nil, err
	}

	// Fetch item y streams.
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

	// Resolver burn-in spec desde el índice pedido.
	burnSub := m.resolveBurnSubtitle(mediaStreams, burnSubIndex, item.Path)

	// Decidir método de playback.
	decision := m.decidePlayback(ctx, item, mediaStreams, caps, profileName, burnSub)

	// DirectPlay no necesita sesión de transcode.
	if decision.Method == MethodDirectPlay {
		return m.buildDirectPlaySession(key, userID, itemID, decision), nil
	}

	// Arrancar transcode/remux.
	return m.spawnTranscodeSession(key, userID, itemID, item.Path, audioStreamIndex, burnSub, decision, startTime)
}

// tryGetExistingSession intenta devolver una sesión ya registrada bajo
// m.mu. Devuelve nil si no existe.
func (m *Manager) tryGetExistingSession(key string) *ManagedSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ms, ok := m.sessions[key]; ok {
		ms.LastAccessed = time.Now()
		return ms
	}
	return nil
}

// checkSessionCaps verifica los límites global y per-user de sesiones
// concurrentes. Devuelve error TranscodeBusy si se excede alguno.
func (m *Manager) checkSessionCaps(userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cfg.MaxTranscodeSessions > 0 && len(m.sessions) >= m.cfg.MaxTranscodeSessions {
		active := len(m.sessions)
		m.metrics.TranscodeBusy()
		return domain.NewTranscodeBusy(active, m.cfg.MaxTranscodeSessions)
	}
	if m.cfg.MaxTranscodeSessionsPerUser > 0 {
		var userActive int
		for _, ms := range m.sessions {
			if ms.UserID == userID {
				userActive++
			}
		}
		if userActive >= m.cfg.MaxTranscodeSessionsPerUser {
			m.metrics.TranscodeBusy()
			return domain.NewTranscodeBusy(userActive, m.cfg.MaxTranscodeSessionsPerUser)
		}
	}
	return nil
}

// resolveBurnSubtitle localiza el stream de subtítulos en el índice
// pedido y devuelve un BurnSubtitleSpec si es un codec que requiere
// burn-in. Devuelve nil si el índice es < 0 o fuera de rango.
func (m *Manager) resolveBurnSubtitle(mediaStreams []*librarymodel.MediaStream, burnSubIndex int, inputPath string) *BurnSubtitleSpec {
	if burnSubIndex < 0 {
		return nil
	}
	var subOrd int
	for _, s := range mediaStreams {
		if s.StreamType != "subtitle" {
			continue
		}
		if subOrd == burnSubIndex {
			if IsBurnableSubtitleCodec(s.Codec) {
				return &BurnSubtitleSpec{
					Index:     burnSubIndex,
					Codec:     s.Codec,
					InputPath: inputPath,
				}
			}
			break
		}
		subOrd++
	}
	return nil
}

// decidePlayback ejecuta el waterfall DirectPlay → DirectStream → Transcode
// y aplica el override force_direct_play y el upgrade por burn-in.
func (m *Manager) decidePlayback(ctx context.Context, item *librarymodel.Item, mediaStreams []*librarymodel.MediaStream, caps *Capabilities, profileName string, burnSub *BurnSubtitleSpec) PlaybackDecision {
	var decision PlaybackDecision
	if m.shouldForceDirectPlay(ctx) {
		decision = DecideForceDirectPlay(item, mediaStreams)
	} else {
		decision = Decide(item, mediaStreams, caps, profileName)
	}

	// Burn-in necesita decoded frames. Si el waterfall eligió
	// DirectPlay/DirectStream, upgrade a Transcode completo.
	if burnSub != nil {
		if decision.Method == MethodDirectPlay {
			decision = Decide(item, mediaStreams, caps, profileName)
		}
		decision.Method = MethodTranscode
		decision.CopyVideo = false
	}
	return decision
}

// buildDirectPlaySession construye una ManagedSession sin sesión de
// transcode (DirectPlay no toca ffmpeg).
func (m *Manager) buildDirectPlaySession(key, userID, itemID string, decision PlaybackDecision) *ManagedSession {
	return &ManagedSession{
		Session: &Session{
			ID:        key,
			ItemID:    itemID,
			StartedAt: time.Now(),
		},
		UserID:       userID,
		Decision:     decision,
		LastAccessed: time.Now(),
	}
}

// spawnTranscodeSession arranca el proceso ffmpeg y registra la sesión
// en el map del manager.
func (m *Manager) spawnTranscodeSession(key, userID, itemID, inputPath string, audioStreamIndex int, burnSub *BurnSubtitleSpec, decision PlaybackDecision, startTime float64) (*ManagedSession, error) {
	startSegment := 0
	if startTime > 0 {
		startSegment = int(startTime / 6) // matches -hls_time 6
	}
	session, err := m.transcoder.Start(key, itemID, TranscodeRequest{
		Input:              inputPath,
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
		InputPath:          inputPath,
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

// Constantes de coalesce para RestartSessionAt. 2 segmentos Y 300ms
// cubren el fanout real de hls.js (~100ms, 3 segmentos adyacentes)
// sin bloquear re-clicks humanos (>=500ms).
const restartCoalesceWindow = 2
const restartCoalesceTimeWindow = 300 * time.Millisecond

// Rate limit per-sesión de restarts por minuto. 20 deja margen para
// power-user scrubbing y detecta runaway clients.
const (
	restartRateLimitWindow = 60 * time.Second
	restartRateLimitMax    = 20
)

// RestartSessionAt detiene el transcoder existente y lo re-arranca en
// el segmento dado. Path de seek-restart: el manifest VOD sintético
// lista todos los segmentos, y cuando el cliente pide uno que ffmpeg
// aún no produjo, reiniciamos en el offset correcto.
//
// Llamadas concurrentes se serializan por sesión vía ms.restartMu con
// coalesce de fanout hls.js. Rate-limited per-sesión para proteger
// contra runaway clients.
func (m *Manager) RestartSessionAt(key string, segmentIndex int, segmentDuration float64) error {
	m.mu.Lock()
	ms, ok := m.sessions[key]
	m.mu.Unlock()
	if !ok {
		return ErrSessionNotFound
	}

	ms.restartMu.Lock()
	defer ms.restartMu.Unlock()

	// Coalesce: si el restart anterior fue reciente en tiempo Y cercano
	// en segmento, es fanout de hls.js — no reiniciar.
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

	// Sliding-window rate limit.
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

	// Cancelar ffmpeg actual sin borrar segmentos previos (útil para
	// seeks hacia atrás dentro del rango ya codificado).
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

// TouchSession actualiza el tiempo de último acceso de una sesión.
func (m *Manager) TouchSession(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ms, ok := m.sessions[key]; ok {
		ms.LastAccessed = time.Now()
	}
}

// StopSessionsByItem detiene todas las sesiones de (userID, itemID)
// independientemente de profile o audio index. Devuelve el count de
// sesiones detenidas.
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

// StopSession detiene una sesión específica.
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

// GetSession devuelve una managed session por key.
func (m *Manager) GetSession(key string) (*ManagedSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ms, ok := m.sessions[key]
	if ok {
		ms.LastAccessed = time.Now()
	}
	return ms, ok
}

// ActiveSessions devuelve el count de sesiones activas de transcode.
func (m *Manager) ActiveSessions() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// SessionSnapshot es la vista read-only de una sesión activa para el
// panel admin "Now Playing".
type SessionSnapshot struct {
	ID           string
	UserID       string
	ItemID       string
	Profile      string
	Method       PlaybackMethod
	StartedAt    time.Time
	LastAccessed time.Time
}

// ListAllSessions devuelve un snapshot de cada sesión activa bajo m.mu.
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

// MaxTranscodeSessions devuelve el cap concurrente efectivo post auto-tune.
func (m *Manager) MaxTranscodeSessions() int {
	return m.cfg.MaxTranscodeSessions
}

// MaxTranscodeSessionsPerUser devuelve el cap per-user efectivo.
func (m *Manager) MaxTranscodeSessionsPerUser() int {
	return m.cfg.MaxTranscodeSessionsPerUser
}

// TranscodePreset devuelve el -preset libx264 efectivo post auto-tune.
func (m *Manager) TranscodePreset() string {
	return m.cfg.TranscodePreset
}

// HWAccelInfo devuelve el snapshot de acelerador detectado al arranque.
func (m *Manager) HWAccelInfo() HWAccelResult {
	return m.hwAccel
}

// HWAccelEnabled reporta si HW acceleration está habilitada en config.
func (m *Manager) HWAccelEnabled() bool {
	return m.cfg.HWAccel.Enabled
}

// CacheDir devuelve el directorio de cache de transcode resuelto.
func (m *Manager) CacheDir() string {
	return m.cfg.EffectiveCacheDir()
}

// Shutdown detiene todas las sesiones y el loop de limpieza.
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

// cleanupLoop elimina periódicamente sesiones idle.
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
