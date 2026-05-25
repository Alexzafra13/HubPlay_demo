package iptv

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/event"
)

// Module agrupa los componentes long-lived del feature IPTV (service +
// proxy + transmux opcional + logo cache opcional + scheduler + prober)
// y reemplaza el bloque de ~125 LoC que `cmd/hubplay/main.go` ejecutaba
// inline para cablearlos.
//
// Cierra la fase iptv del olor G del audit 2026-05-14. La extracción
// previa del `runtime` god-struct a `cmd/hubplay/lifecycle.go` cerró el
// olor a nivel shutdown — esta fase cierra la otra mitad (constructor
// composition) trasladando el wiring a su paquete dueño.
type Module struct {
	Service   *Service
	Proxy     *StreamProxy
	Transmux  *TransmuxManager // nil cuando deps.Transmux.Enabled == false
	LogoCache *LogoCache       // nil cuando la construcción falla (non-fatal)
	Scheduler *Scheduler
	Prober    *ProberWorker
}

// Deps es la entrada explícita a New. Mantiene el paquete iptv libre de
// dependencias hacia config / observability / stream — main resuelve los
// valores derivados (hwaccel encoder, cache dirs, sinks Prometheus) y
// los pasa pre-resueltos.
type Deps struct {
	Channels             *db.ChannelRepository
	EPGPrograms          *db.EPGProgramRepository
	Libraries            *db.LibraryRepository
	Favorites            *db.ChannelFavoritesRepository
	ChannelOrder         *db.UserChannelOrderRepository
	LibraryChannelOrder  *db.LibraryChannelOrderRepository
	EPGSources           *db.LibraryEPGSourceRepository
	ChannelOverrides     *db.ChannelOverrideRepository
	ChannelLogoOverrides *db.ChannelLogoOverrideRepository
	ChannelWatchHistory  *db.ChannelWatchHistoryRepository
	Schedules            *db.IPTVScheduleRepository

	EventBus *event.Bus

	// Transmux: zero-value (Enabled=false) deshabilita el TransmuxManager
	// y los demás campos del struct se ignoran.
	Transmux TransmuxOpts

	// LogoCacheDir: ruta absoluta del cache de logos descargados.
	LogoCacheDir string

	// IPTVOrgLogosCachePath: ruta del JSON cache para iptv-org logos.
	IPTVOrgLogosCachePath string

	Logger *slog.Logger
}

// TransmuxOpts congrega los parámetros del transmux manager. main los
// rellena con `cfg.IPTV.Transmux.*` + `stream.HWAccelInfo`. Mantener
// como struct propio (en lugar de `TransmuxManagerConfig` directamente)
// hace explícito qué opcionales el Module conoce y oculta los wires
// internos (Gate, Reporter) que el Module fija él mismo.
type TransmuxOpts struct {
	Enabled             bool
	CacheDir            string
	MaxSessions         int
	MaxReencodeSessions int
	IdleTimeout         time.Duration
	ReadyTimeout        time.Duration
	ReencodeEncoder     string
	ReencodeHWAccelArgs []string
	Metrics             TransmuxMetrics

	// RegisterGauges es un callback opcional para registrar gauges
	// Prometheus contra el manager recién construido. main inyecta
	// `observability.RegisterIPTVTransmuxGauges`. Un error aquí
	// (programmer error: gauge duplicada) rompe el boot — visible en
	// CI, no degradación silenciosa en runtime.
	RegisterGauges func(*TransmuxManager) error
}

// New construye el Module aplicando el cross-wiring interno
// (`proxy.SetHealthReporter`, `service.SetIPTVOrgLogos`,
// `service.SetProberWorker`) y arranca los workers (scheduler + prober)
// contra el ctx pasado. El ctx limita la vida de ambos workers — su
// drain se hace en `Stop`, llamada por el lifecycle del binario.
func New(ctx context.Context, deps Deps) (*Module, error) {
	service := NewService(
		deps.Channels,
		deps.EPGPrograms,
		deps.Libraries,
		deps.Favorites,
		deps.ChannelOrder,
		deps.LibraryChannelOrder,
		deps.EPGSources,
		deps.ChannelOverrides,
		deps.ChannelLogoOverrides,
		deps.ChannelWatchHistory,
		deps.Logger,
	)
	service.SetEventBus(deps.EventBus)

	proxy := NewStreamProxy(deps.Logger)
	// El proxy registra outcomes de probe contra el channel repo a
	// través del service (dead upstreams ⇒ user view filtrada).
	proxy.SetHealthReporter(service)

	var transmux *TransmuxManager
	if deps.Transmux.Enabled {
		transmux = NewTransmuxManager(TransmuxManagerConfig{
			CacheDir:                 deps.Transmux.CacheDir,
			MaxSessions:              deps.Transmux.MaxSessions,
			MaxReencodeSessions:      deps.Transmux.MaxReencodeSessions,
			IdleTimeout:              deps.Transmux.IdleTimeout,
			ReadyTimeout:             deps.Transmux.ReadyTimeout,
			Gate:                     proxy.Breaker(),
			Reporter:                 service,
			Metrics:                  deps.Transmux.Metrics,
			ReencodeEncoder:          deps.Transmux.ReencodeEncoder,
			ReencodeHWAccelInputArgs: deps.Transmux.ReencodeHWAccelArgs,
		}, deps.Logger)
		if deps.Transmux.RegisterGauges != nil {
			if err := deps.Transmux.RegisterGauges(transmux); err != nil {
				return nil, fmt.Errorf("register iptv transmux gauges: %w", err)
			}
		}
		deps.Logger.Info("iptv transmux enabled",
			"cache_dir", deps.Transmux.CacheDir,
			"max_sessions", deps.Transmux.MaxSessions,
			"max_reencode_sessions", transmux.MaxReencodeSessions(),
			"reencode_encoder", deps.Transmux.ReencodeEncoder)
	}

	// LogoCache es opcional + no-fatal. Si la construcción falla el
	// handler trata nil como "logo cache disabled" y el frontend cae al
	// fallback de iniciales/color.
	var logoCache *LogoCache
	if lc, err := NewLogoCache(deps.LogoCacheDir, deps.Logger); err != nil {
		deps.Logger.Warn("iptv logo cache disabled", "error", err)
	} else {
		logoCache = lc
		deps.Logger.Info("iptv logo cache enabled", "cache_dir", deps.LogoCacheDir)
	}

	// iptv-org logo auto-discovery: fetch lazy (en la primera llamada
	// admin) para no pegar a iptv-org.github.io durante boot.
	service.SetIPTVOrgLogos(NewIPTVOrgLogoLookup(deps.IPTVOrgLogosCachePath))

	scheduler := NewScheduler(deps.Schedules, service, deps.Logger)
	scheduler.Start(ctx)

	proberHTTP := NewProber(nil, service)
	proberWorker, err := NewProberWorker(proberHTTP, deps.Libraries, deps.Channels, deps.Logger)
	if err != nil {
		return nil, fmt.Errorf("iptv prober worker: %w", err)
	}
	proberWorker.Start(ctx)
	service.SetProberWorker(proberWorker)

	return &Module{
		Service:   service,
		Proxy:     proxy,
		Transmux:  transmux,
		LogoCache: logoCache,
		Scheduler: scheduler,
		Prober:    proberWorker,
	}, nil
}

// LifecycleRegistrar es la interface mínima que el Module necesita del
// `lifecycle` del binario para auto-registrar sus hooks de teardown.
// Definida aquí (no en main) para que el paquete iptv no importe nada
// de cmd/. El tipo `*lifecycle` del paquete main la satisface
// estructuralmente — el alias `stopFn = func(context.Context) error`
// lo deja compatible sin conversión.
type LifecycleRegistrar interface {
	AddWorker(name string, fn func(ctx context.Context) error)
	AddService(name string, fn func(ctx context.Context) error)
}

// RegisterWith registra los hooks de teardown del módulo contra el
// lifecycle del binario. El orden respeta la fase 3 del lifecycle
// (services en LIFO): el service se registra el primero ⇒ se para el
// último, después de que transmux y proxy (que dependen de él como
// reporter) hayan drenado.
//
// Llamar inmediatamente después de New, en el mismo run() que lo
// construyó. Los hooks capturan punteros del Module.
func (m *Module) RegisterWith(lc LifecycleRegistrar) {
	// Workers (fase 1, add-order): scheduler primero porque su refresh
	// en vuelo necesita un DB handle abierto para grabar su outcome;
	// si esperase a la fase de services, race con database.Close.
	lc.AddWorker("iptv scheduler", func(ctx context.Context) error {
		m.Scheduler.Stop(ctx)
		return nil
	})
	lc.AddWorker("iptv prober", func(ctx context.Context) error {
		return m.Prober.Stop(ctx)
	})

	// Services (fase 3, LIFO):
	//   registrar service → proxy → transmux
	//   shutdown LIFO ⇒ transmux → proxy → service
	// El service queda fuera el último porque proxy y transmux le
	// reportan health durante su shutdown.
	lc.AddService("iptv service", func(context.Context) error {
		m.Service.Shutdown()
		return nil
	})
	lc.AddService("iptv proxy", func(context.Context) error {
		// ClearRelays sólo vacía contabilidad — el drain real viene
		// del http.Server.Shutdown previo (audit olor EE).
		m.Proxy.ClearRelays()
		return nil
	})
	if m.Transmux != nil {
		tm := m.Transmux
		lc.AddService("iptv transmux", func(context.Context) error {
			tm.Shutdown()
			return nil
		})
	}
}
