package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	hubplay "hubplay"
	"hubplay/internal/api"
	"hubplay/internal/api/handlers"
	"hubplay/internal/audit"
	"hubplay/internal/auth"
	"hubplay/internal/clock"
	"hubplay/internal/config"
	"hubplay/internal/db"
	"hubplay/internal/event"
	"hubplay/internal/federation"
	federationstorage "hubplay/internal/federation/storage"
	"hubplay/internal/imaging/pathmap"
	"hubplay/internal/library"
	"hubplay/internal/logging"
	"hubplay/internal/iptv"
	"hubplay/internal/notification"
	"hubplay/internal/observability"
	"hubplay/internal/probe"
	"hubplay/internal/provider"
	"hubplay/internal/retention"
	"hubplay/internal/setup"
	"hubplay/internal/stream"
	"hubplay/internal/mdns"
	"hubplay/internal/sysmetrics"
	"hubplay/internal/updates"
	"hubplay/internal/upload"
	"hubplay/internal/user"
)

// Variables de release. Inyectadas por el linker en CI:
//
//	go build -ldflags="
//	    -X main.version=$(git describe --tags --always --dirty)
//	    -X main.commit=$(git rev-parse --short HEAD)
//	    -X main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
//
// En desarrollo (go run, make dev) quedan en sus defaults — útil para
// distinguir builds locales en logs y en el endpoint /api/v1/version.
var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

func main() {
	configPath := flag.String("config", "hubplay.yaml", "path to config file")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		// Output one-line — scripts (install.sh, monitoring, etc.) parsean
		// stdout. Formato estable: "hubplay <version> (commit <sha>, built <date>)".
		// dev builds quedan "hubplay dev (commit none, built unknown)" — parseable.
		fmt.Printf("hubplay %s (commit %s, built %s)\n", version, commit, buildDate)
		return
	}

	if err := run(*configPath); err != nil {
		fmt.Fprintf(os.Stderr, "hubplay: %v\n", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// lifecycle dirige el shutdown ordenado por dominio (workers
	// independientes → HTTP drain → services HTTP-coupled). Reemplaza
	// el `runtime` god-struct del flujo pre-refactor (olor G del audit
	// 2026-05-14). Cada componente long-lived se registra con
	// `lc.AddWorker(name, stopFn)` o `lc.AddService(name, stopFn)`
	// junto al wiring — sin god-struct intermedio, sin desempaquetado
	// posicional en waitForShutdown.
	lc := &lifecycle{}

	// ═══ Phase 1: Foundation ═══
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	logger, logBuffer := logging.NewWithBuffer(cfg.Logging)
	slog.SetDefault(logger)
	clk := clock.New()

	logger.Info("starting HubPlay", "version", version, "commit", commit, "addr", cfg.Server.Addr())

	// Bundled binaries lookup. Cuando el release ship ffmpeg+ffprobe
	// junto a hubplay.exe, prepender el directorio del ejecutable al
	// PATH hace que cualquier exec.LookPath/exec.Command los encuentre
	// sin tocar call-sites individuales. En instalaciones donde el
	// operador ya tiene ffmpeg en PATH del sistema, prevalece el
	// bundled (intencional — distribuimos una build conocida estable).
	// Errores aquí son fatales sólo si interrumpen el boot — failure
	// silencioso del prepend cae al preflight habitual.
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		curPath := os.Getenv("PATH")
		if curPath == "" {
			_ = os.Setenv("PATH", exeDir)
		} else if !strings.Contains(string(os.PathListSeparator)+curPath+string(os.PathListSeparator),
			string(os.PathListSeparator)+exeDir+string(os.PathListSeparator)) {
			_ = os.Setenv("PATH", exeDir+string(os.PathListSeparator)+curPath)
		}
	}

	// Preflight: validate external binaries and filesystem permissions
	// before any service is built. Catching these here means "ffmpeg not
	// installed" shows up as a clear boot error instead of an opaque 500
	// during the first user's stream attempt.
	if err := cfg.Preflight(logger); err != nil {
		return fmt.Errorf("preflight checks failed:\n%w", err)
	}

	// ═══ Phase 2: Database ═══
	// Swap in any pending admin-uploaded restore file before opening
	// the live connection. SQLite-only — restore is a file swap on the
	// live DB path; Postgres has its own server-side restore tooling
	// (pg_restore + base backups) that operates out-of-process.
	if cfg.Database.Driver == db.DriverSQLite {
		if err := db.ApplyPendingRestoreIfAny(cfg.Database.Path, logger); err != nil {
			return fmt.Errorf("applying pending DB restore: %w", err)
		}
	}
	dbDsnOrPath := cfg.Database.Path
	if cfg.Database.Driver == db.DriverPostgres {
		dbDsnOrPath = cfg.Database.DSN
	}
	database, err := db.Open(cfg.Database.Driver, dbDsnOrPath, logger)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close() //nolint:errcheck

	if err := db.Migrate(cfg.Database.Driver, database, hubplay.Migrations(cfg.Database.Driver), logger); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	// Driver string drives dual-dialect repos (Sesión E in progress).
	// Repos not yet migrated ignore the param and stay on SQLite.
	repos := db.NewRepositories(cfg.Database.Driver, database)

	// ═══ Phase 3: Infrastructure ═══
	eventBus := event.NewBus(logger)

	// Observability: one registry shared by every collector. A construction
	// error here is a programmer error (duplicate metric name) — fail fast
	// so it shows up in CI, not in a production scrape.
	metrics, err := observability.NewMetrics(version)
	if err != nil {
		return fmt.Errorf("initialising metrics: %w", err)
	}

	// ═══ Phase 4: Core Services ═══
	//
	// JWT signing keys live in the DB so they survive restarts and can be
	// rotated without editing config. On first boot (empty table) we seed
	// the keystore with the config secret so any tokens that were issued
	// before this upgrade keep validating; subsequent boots pick up existing
	// keys verbatim.
	if _, err := auth.Bootstrap(ctx, repos.SigningKeys, clk, cfg.Auth.JWTSecret); err != nil {
		return fmt.Errorf("bootstrapping signing keys: %w", err)
	}
	keyStore, err := auth.NewKeyStore(ctx, repos.SigningKeys, clk)
	if err != nil {
		return fmt.Errorf("loading signing keys: %w", err)
	}
	logger.Info("signing keys loaded", "active", keyStore.ActiveCount(), "retired", keyStore.RetiredCount())

	// Expose live keystore counts to Prometheus via GaugeFunc so the metric
	// never drifts from the DB (see observability/auth.go for rationale).
	if err := observability.RegisterKeyStoreGauges(metrics, keyStore); err != nil {
		return fmt.Errorf("registering keystore gauges: %w", err)
	}

	authService := auth.NewService(repos.Users, repos.Sessions, keyStore, cfg.Auth, clk, logger, cfg.RateLimit)
	authService.SetEventBus(eventBus)
	authService.StartSessionCleaner(ctx)
	lc.AddWorker("session cleaner", func(context.Context) error {
		authService.StopSessionCleaner()
		return nil
	})
	deviceCodeService := auth.NewDeviceCodeService(authService, repos.DeviceCodes, repos.Users, logger)
	// avatarsDir vive junto a la DB para compartir volumen docker.
	// Si el operador no tiene la DB en disco (modo :memory: en
	// tests), pasamos "" y el service deshabilita uploads.
	avatarsDir := ""
	if cfg.Database.Path != "" && cfg.Database.Path != ":memory:" {
		avatarsDir = filepath.Join(filepath.Dir(cfg.Database.Path), "avatars")
	}
	userService := user.NewService(repos.Users, logger, avatarsDir)

	// Inbox de notificaciones generico (migration 049). El service
	// vive como singleton para que cualquier feature (federation,
	// stream, library, ...) emita con un solo handle. El bus opcional
	// hace que Create publique un evento que /me/events empuja al
	// frontend del destinatario - badge en vivo sin polling.
	notificationRepo := notification.NewRepository(cfg.Database.Driver, database)
	notificationService := notification.NewService(notificationRepo, repos.Users, eventBus, clk, logger)

	prober := probe.New()

	// ═══ Phase 4d: Providers (TMDb, Fanart.tv, OpenSubtitles) ═══
	providerManager := provider.NewManager(repos.Providers, logger)
	_ = providerManager.Register(ctx, provider.NewTMDbProvider())
	_ = providerManager.Register(ctx, provider.NewFanartProvider())
	_ = providerManager.Register(ctx, provider.NewOpenSubtitlesProvider())

	// Image storage shared with the HTTP image handler/refresher: el
	// scanner descarga aquí cada poster/backdrop, los re-scans y los
	// refreshes admin escriben al mismo root. Pathmap es plain
	// filesystem state — instances independientes apuntando al mismo
	// directorio son seguras.
	imageDir := filepath.Join(filepath.Dir(cfg.Database.Path), "images")
	scannerPathmap := pathmap.New(imageDir)

	// ═══ Phase 4a: Library ═══
	//
	// `library.New` agrupa los 9 componentes long-lived del feature
	// library (scanner + service + 2 schedulers + 2 detectores
	// skip-intro + fingerprinter + fs watcher), aplica el
	// cross-wiring (scanner inyectado en service; segment-detector +
	// fingerprinter suscritos al bus) y arranca los workers contra
	// `ctx`. `libMod.RegisterWith(lc)` añade los 6 hooks de shutdown
	// en orden (workers add-order; services LIFO empezando por
	// library service que drena scans). Cierra la fase library del
	// olor G del audit 2026-05-14 — con esto y la fase iptv (#417)
	// + lifecycle.go (#396) el olor queda al 100 %.
	libMod, err := library.New(ctx, library.Deps{
		Libraries:           repos.Libraries,
		Items:               repos.Items,
		MediaStreams:        repos.MediaStreams,
		Metadata:            repos.Metadata,
		ExternalIDs:         repos.ExternalIDs,
		Images:              repos.Images,
		Chapters:            repos.Chapters,
		EpisodeSegments:     repos.EpisodeSegments,
		People:              repos.People,
		ItemValues:          repos.ItemValues,
		Studios:             repos.Studios,
		Collections:         repos.Collections,
		ItemMetadataLocks:   repos.ItemMetadataLocks,
		Channels:            repos.Channels,
		Providers:           providerManager,
		Prober:              prober,
		EventBus:            eventBus,
		Pathmap:             scannerPathmap,
		ImageDir:            imageDir,
		FingerprintCacheDir: cfg.Streaming.EffectiveCacheDir(),
		Logger:              logger,
	})
	if err != nil {
		return err
	}
	libMod.RegisterWith(lc)

	// Periodic SQLite query-planner refresh + FTS5 merge. Fires every
	// 6h once started; first tick is on the interval, not immediately,
	// so boot doesn't pay the cost on top of the cold-start overhead.
	// No-op on Postgres (autovacuum handles ANALYZE on its own schedule).
	stopOptimize := db.StartPeriodicOptimize(ctx, cfg.Database.Driver, database, logger)
	defer stopOptimize()

	// ═══ Phase 4b: Streaming ═══
	//
	// Apply runtime overrides from app_settings BEFORE constructing the
	// stream manager. The detector runs once at NewManager() time and
	// the choice is captured for the lifetime of the process — that's
	// why hardware_acceleration toggles in the admin UI carry a
	// "restart to apply" hint. Reading the DB here keeps a single
	// authority chain: YAML default → DB override → effective config
	// the rest of the code sees, with no second source of truth.
	streamingCfg := cfg.Streaming
	if v, err := repos.Settings.Get(ctx, "hardware_acceleration.enabled"); err == nil {
		streamingCfg.HWAccel.Enabled = v == "true"
	}
	if v, err := repos.Settings.Get(ctx, "hardware_acceleration.preferred"); err == nil && v != "" {
		streamingCfg.HWAccel.Preferred = v
	}
	// Runtime overrides for the auto-tuned knobs. Empty / zero values
	// in streamingCfg trigger stream.AutoTuneStreaming inside
	// NewManager; values populated here (whether from YAML defaults
	// the operator set, or from admin app_settings rows) bypass the
	// auto-tuner. Parse failures fall through silently — a bad row in
	// app_settings is a UI bug, not a reason to refuse to boot.
	if v, err := repos.Settings.Get(ctx, "streaming.max_transcode_sessions"); err == nil {
		if n, perr := strconv.Atoi(v); perr == nil && n >= 0 {
			streamingCfg.MaxTranscodeSessions = n
		}
	}
	if v, err := repos.Settings.Get(ctx, "streaming.max_transcode_sessions_per_user"); err == nil {
		if n, perr := strconv.Atoi(v); perr == nil && n >= 0 {
			streamingCfg.MaxTranscodeSessionsPerUser = n
		}
	}
	if v, err := repos.Settings.Get(ctx, "streaming.transcode_preset"); err == nil && v != "" {
		streamingCfg.TranscodePreset = v
	}
	// Wiring atómico (Deps) sustituye a los 3 setters post-construcción
	// del flujo pre-refactor (olor JJ del audit 2026-05-14).
	streamManager := stream.NewManager(stream.Deps{
		Items:    repos.Items,
		Streams:  repos.MediaStreams,
		Config:   streamingCfg,
		Logger:   logger,
		Metrics:  observability.NewStreamSink(metrics),
		EventBus: eventBus,
		// Runtime hook for the playback.force_direct_play admin
		// toggle. Read on every StartSession so el admin puede
		// flipearlo sin restart. Boolean parse mirrors el validator
		// en settings.validateSettingValue (canónico "true"/"false").
		ForceDirectPlayLookup: func(ctx context.Context) bool {
			v, err := repos.Settings.GetOr(ctx, "playback.force_direct_play", "false")
			if err != nil {
				return false
			}
			return v == "true"
		},
	})
	lc.AddService("stream manager", func(context.Context) error {
		streamManager.Shutdown()
		return nil
	})

	// ═══ Phase 4c: IPTV ═══
	//
	// `iptv.New` agrupa los 6 componentes long-lived del feature
	// (service + proxy + transmux opcional + logo cache opcional +
	// scheduler + prober), aplica el cross-wiring interno
	// (proxy.SetHealthReporter, service.SetIPTVOrgLogos,
	// service.SetProberWorker, transmux.Gate=proxy.Breaker()) y
	// arranca los workers contra `ctx`. `iptvMod.RegisterWith(lc)`
	// añade los 5 hooks de shutdown en el orden correcto. Cierra la
	// fase iptv del olor G del audit 2026-05-14.
	var iptvTransmuxOpts iptv.TransmuxOpts
	if cfg.IPTV.Transmux.Enabled {
		// Hwaccel reuse: mismo encoder + decode flags que el VOD
		// transcoder eligió al boot. En hosts con VAAPI / NVENC el
		// reencode fallback corre ahí también (5-10× CPU win en
		// HEVC → H.264 — lo que hace al fallback affordable en
		// low-spec).
		hwInfo := streamManager.HWAccelInfo()
		iptvTransmuxOpts = iptv.TransmuxOpts{
			Enabled:             true,
			CacheDir:            filepath.Join(cfg.Streaming.EffectiveCacheDir(), "iptv-hls"),
			MaxSessions:         cfg.IPTV.Transmux.MaxSessions,
			MaxReencodeSessions: cfg.IPTV.Transmux.MaxReencodeSessions,
			IdleTimeout:         cfg.IPTV.Transmux.IdleTimeout,
			ReadyTimeout:        cfg.IPTV.Transmux.ReadyTimeout,
			ReencodeEncoder:     hwInfo.Encoder,
			ReencodeHWAccelArgs: stream.HWAccelInputArgs(hwInfo.Selected),
			Metrics:             observability.NewIPTVTransmuxSink(metrics),
			RegisterGauges: func(t *iptv.TransmuxManager) error {
				return observability.RegisterIPTVTransmuxGauges(metrics, t)
			},
		}
	}
	iptvMod, err := iptv.New(ctx, iptv.Deps{
		Channels:              repos.Channels,
		EPGPrograms:           repos.EPGPrograms,
		Libraries:             repos.Libraries,
		Favorites:             repos.ChannelFavorites,
		ChannelOrder:          repos.ChannelOrder,
		LibraryChannelOrder:   repos.LibraryChannelOrder,
		EPGSources:            repos.LibraryEPGSources,
		ChannelOverrides:      repos.ChannelOverrides,
		ChannelLogoOverrides:  repos.ChannelLogoOverrides,
		ChannelWatchHistory:   repos.ChannelWatchHistory,
		Schedules:             repos.IPTVSchedules,
		EventBus:              eventBus,
		Transmux:              iptvTransmuxOpts,
		LogoCacheDir:          filepath.Join(cfg.Streaming.EffectiveCacheDir(), "iptv-logos"),
		IPTVOrgLogosCachePath: filepath.Join(filepath.Dir(cfg.Database.Path), "images", "iptv-org-channels.json"),
		Logger:                logger,
	})
	if err != nil {
		return err
	}
	iptvMod.RegisterWith(lc)

	// ═══ Phase 4e: Setup Service ═══
	setupService := setup.NewService(cfg, configPath, logger)

	// Restart requester: handlers (admin DB panel, setup wizard's
	// database step) trigger it after saving a new YAML to roll the
	// process so the next boot reads the updated config. Under the
	// project's default `restart: unless-stopped` docker-compose
	// policy this means a 2-3 second outage and the container is
	// back; on bare metal the operator's supervisor (systemd /
	// docker swarm / k8s) is expected to do the same.
	restartRequester := config.NewRestartRequester(cancel, logger)

	// ═══ Phase 5: HTTP Server ═══
	webFS, _ := fs.Sub(hubplay.WebAssets, "web/dist")

	// Federation: load-or-create this server's Ed25519 identity and wire
	// the manager. Failures here are non-fatal — federation is opt-in;
	// if init fails we run with Federation=nil and the routes are skipped
	// (the router checks deps.Federation != nil). The admin sees the
	// federation surface unavailable; everything else keeps working.
	federationRepo := federationstorage.NewRepository(cfg.Database.Driver, database)
	federationCfg := federation.DefaultConfig()
	federationCfg.AdvertisedURL = cfg.Server.BaseURL
	federationCfg.Version = version
	// Comparte el avatarsDir con users: namespace disjunto (los
	// nombres del servidor llevan prefijo "server-", los de usuario
	// son UUIDs), pero el mismo volumen docker. Vacío = uploads
	// del servidor deshabilitados (handler 503).
	federationCfg.AvatarsDir = avatarsDir
	if _, err := federation.LoadOrCreate(ctx, federationRepo, clk, "HubPlay Server"); err != nil {
		logger.Error("federation: identity load/create failed; federation disabled", "err", err)
	}
	federationManager, err := federation.NewManager(ctx, federationCfg, federationRepo, clk, logger, eventBus)
	if err != nil {
		logger.Error("federation: manager init failed; federation disabled", "err", err)
		federationManager = nil
	} else {
		// Inyectar el reader de settings persistentes para que el
		// manager pueda leer/escribir el toggle
		// federation.accept_pairing_requests sin tocar SQL crudo.
		federationManager.SetSettings(repos.Settings)
		logger.Info("federation: manager initialised",
			"server_uuid", federationManager.PublicServerInfo().ServerUUID,
			"fingerprint", federationManager.PublicServerInfo().PubkeyFingerprint)
		// Wire Prometheus observability: counter+histogram via sink,
		// live gauges via GaugeFunc reading the manager's in-memory
		// state at scrape time.
		federationManager.SetMetricsSink(observability.NewFederationSink(metrics))
		if err := observability.RegisterFederationGauges(metrics, federationManager); err != nil {
			logger.Error("federation: register gauges failed", "err", err)
		}
		// Flush the audit log queue on graceful shutdown so the last
		// few peer requests aren't lost.
		defer federationManager.Close()

		// Hook event-bus -> notifications: cuando llega una pairing
		// request o se resuelve una outbound, creamos las entradas
		// correspondientes en el inbox del admin (badge en TopBar).
		// El federation manager publica los eventos; el wire vive
		// aqui en main.go para mantener federation desacoplado del
		// paquete notification (acoplamiento solo va de notification
		// hacia event, no al reves).
		registerFederationNotifications(ctx, eventBus, notificationService, logger)

		// Job periodico: cada 1h expira las pairing requests cuyo
		// TTL ha pasado. Sin esto las filas se acumulan eternamente
		// y el cap defensivo nunca recicla espacio.
		stopPendingSweeper := federation.StartPendingRequestSweeper(ctx, federationManager, logger, time.Hour)
		defer stopPendingSweeper()
	}

	// Job periodico: cada 24h purga notificaciones leidas con > 30d.
	// Vive fuera del bloque federation porque el inbox es independiente
	// (otras features podran emitir en el futuro). Las no-leidas se
	// conservan siempre.
	stopNotifSweeper := notification.StartReadCleanupSweeper(ctx, notificationRepo, logger, 24*time.Hour, notification.DefaultReadRetention)
	defer stopNotifSweeper()

	// Retention sweep: prune EPG programmes and federation audit log on
	// a fixed cadence so append-only tables don't grow forever. Both
	// dependencies are nil-safe inside the runner — operators without
	// IPTV or federation still get a no-op runner that costs nothing.
	retentionRunner := retention.New(cfg.Retention, iptvMod.Service, federationRepo, logger)
	retentionRunner.Start(ctx)
	lc.AddWorker("retention runner", func(context.Context) error {
		retentionRunner.Stop()
		return nil
	})

	// Host metrics sampler: CPU%, RAM, CPU/GPU model strings.
	// Background goroutine ticks every 5 s and stores the latest
	// snapshot in an atomic.Value; the admin /system/stats handler
	// reads non-blocking on every poll. Start() runs the slow probes
	// (gopsutil cpu.Info, nvidia-smi when present) inline so the
	// first /system/stats response after boot already has populated
	// values. Lifetime bound to ctx (cancelled on shutdown signal).
	hostMetrics := sysmetrics.New(5*time.Second, logger)
	hostMetrics.Start(ctx)

	// Uploads (PR2 feature). El handler se cablea sólo si está
	// activado en config — si no, deps.Uploads queda nil y el router
	// no monta /api/v1/uploads*.
	var uploadsHandler http.Handler
	if cfg.Upload.Enabled {
		stagingDir, err := upload.NewStagingDir(cfg.Upload.StagingDir)
		if err != nil {
			logger.Error("upload staging dir setup failed", "error", err)
			os.Exit(1)
		}
		upSvc := upload.NewService(
			upload.Config{
				MaxUploadBytes: cfg.Upload.MaxBytesPerUpload,
				MinDurationMs:  cfg.Upload.MinDurationMs,
			},
			stagingDir,
			repos.Users,
			repos.UploadAudit,
			eventBus,
			upload.NewLibraryPicker(repos.Libraries),
			prober,
			logger,
		)
		// basePath debe casar EXACTAMENTE con el path bajo el que se
		// monta en chi (/api/v1/uploads/). tusd usa este string para
		// generar el Location: <basePath><id> tras el POST de creación.
		tusd, err := upload.NewTusdHandler(upSvc, "/api/v1/uploads/")
		if err != nil {
			logger.Error("upload tusd handler setup failed", "error", err)
			os.Exit(1)
		}
		// http.StripPrefix es REQUERIDO entre chi y tusd. Razón:
		// chi.Mount NO modifica r.URL.Path — sólo actualiza un
		// RouteContext interno que tusd no consulta. tusd, dentro
		// de su Handler.ServeHTTP, hace `strings.Trim(r.URL.Path, "/")`
		// y compara contra "" para decidir si es POST de creación.
		// Sin strip, tusd ve "/api/v1/uploads/" → no coincide con ""
		// → cae al default que devuelve 405 method not allowed.  Con
		// strip, r.URL.Path queda "/", tusd lo trimea a "" → POST
		// creación OK; en los chunks queda "/<id>", tusd extrae el
		// id correctamente y rutea a PatchFile.
		//
		// El BasePath de la config tusd (/api/v1/uploads/) se preserva
		// porque tusd lo usa SÓLO para componer el Location: header
		// que devuelve al cliente — no para route matching.
		uploadsHandler = http.StripPrefix("/api/v1/uploads", tusd)
		logger.Info("uploads enabled",
			"staging_dir", stagingDir.Root(),
			"max_bytes", cfg.Upload.MaxBytesPerUpload)

		// GC de uploads huérfanos. Si el binario cae mientras un
		// upload está en vuelo, los chunks + .info quedan en
		// <staging>/<user>/<id>/ sin que Service.Finish/Aborted
		// recupere espacio. El GC barre cada hora dirs con TODOS
		// sus ficheros más antiguos que 24h — tiempo suficiente
		// para que un "upload pausado" legítimo sobreviva un par
		// de timeouts de red sin perderse, pero corto para que un
		// blob abandonado no acumule días.
		upload.NewGC(stagingDir, time.Hour, 24*time.Hour, logger).Start(ctx)
	} else {
		logger.Info("uploads disabled (config.upload.enabled=false)")
	}

	// Audit service (PR5). Cableado antes que los handlers para que
	// el router lo reciba. Sink fire-and-forget — un INSERT lento o
	// fallido NO bloquea el flujo principal.
	auditService := audit.NewService(repos.AuditLog, logger)

	// Update checker (PR2 update-notifier): goroutine en background que
	// sondea GitHub Releases cada 24h con ETag para detectar versiones
	// nuevas. Si version=="dev" o repo=="" el servicio queda no-op.
	// El context del run() cancela la goroutine al shutdown.
	updateService := updates.New(version, "Alexzafra13/HubPlay_demo", logger)
	updateService.Start(ctx)

	// mDNS: anuncia el server en la LAN como "<hostname>.local". Errores
	// no son fatales — firewall bloqueando UDP/5353 o falta de soporte
	// multicast no debe impedir el arranque del server.
	if _, err := mdns.Start(ctx, mdns.Config{
		Enabled:  cfg.MDNS.Enabled,
		Hostname: cfg.MDNS.Hostname,
		Port:     cfg.Server.Port,
		Version:  version,
	}, logger); err != nil {
		logger.Warn("mdns disabled", "error", err)
	}

	// CORS registry (PR4 feature CORS-dynamic): combina statics del
	// YAML + dynamics del DB en un atomic.Pointer.  Lo construimos
	// AQUÍ (antes del router) para que NewRouter lo reciba listo.
	// Pre-carga inicial de dynamics — los siguientes Add/Delete del
	// panel admin recargan vía el handler.
	corsRegistry := api.NewCorsRegistry(api.AllowedOrigins(cfg))
	if initialDynamics, err := repos.CorsOrigins.ListOrigins(ctx); err == nil {
		corsRegistry.SetDynamics(initialDynamics)
		logger.Info("cors registry loaded",
			"statics", len(api.AllowedOrigins(cfg)),
			"dynamics", len(initialDynamics))
	} else {
		// Si el pre-load falla, arrancamos con dynamics vacíos. El
		// operador verá la lista vacía en el panel y al reiniciar se
		// llenará. No abortamos boot — CORS estático del YAML basta.
		logger.Warn("cors registry: failed initial dynamics load", "error", err)
	}

	router := api.NewRouter(api.Dependencies{
		Auth:          authService,
		DeviceCode:    deviceCodeService,
		Users:         userService,
		Libraries:     libMod.Service,
		StreamManager: streamManager,
		IPTV:          iptvMod.Service,
		IPTVProxy:     iptvMod.Proxy,
		IPTVTransmux:  iptvMod.Transmux,
		IPTVLogoCache: iptvMod.LogoCache,
		IPTVScheduler: iptvMod.Scheduler,
		IPTVSchedules: repos.IPTVSchedules,
		Items:         repos.Items,
		MediaStreams:   repos.MediaStreams,
		Images:        repos.Images,
		Metadata:      repos.Metadata,
		UserData:        repos.UserData,
		Chapters:        repos.Chapters,
		EpisodeSegments: repos.EpisodeSegments,
		People:          repos.People,
		Studios:         repos.Studios,
		Collections:     repos.Collections,
		CollectionImageOverrides: repos.CollectionImageOverrides,
		UserPreferences: repos.UserPreferences,
		Home:            repos.Home,
		Providers:     providerManager,
		Scanner:       libMod.Scanner,
		ExternalIDs:   repos.ExternalIDs,
		LibraryRepo:   repos.Libraries,
		ProviderRepo:  repos.Providers,
		Settings:      repos.Settings,
		SetupService:  setupService,
		EventBus:      eventBus,
		Federation:    federationManager,
		Notifications: notificationService,
		DB:            db.NewMaintenance(cfg.Database.Driver, database),
		Activity:      db.NewActivityRepository(cfg.Database.Driver, database),
		Version:       version,
		Commit:        commit,
		BuildDate:     buildDate,
		WebAssets:     webFS,
		Config:        cfg,
		// Primitivos derivados de cfg pasados explícitos al router.
		// Cierra olor V del audit 2026-05-14 — router.go ya no lee
		// `deps.Config.X.Y` (salvo los dos handlers que mutan el
		// fichero: setup wizard + admin DB panel).
		MetricsEnabled: cfg.Observability.MetricsEnabled,
		MetricsPath:    cfg.Observability.MetricsPath,
		AuthConfig:     cfg.Auth,
		DataDir:        filepath.Dir(cfg.Database.Path),
		DatabasePath:   cfg.Database.Path,
		DatabaseDriver: cfg.Database.Driver,
		ServerAddr:     cfg.Server.Addr(),
		ServerBaseURL:  cfg.Server.BaseURL,
		ServerPort:     cfg.Server.Port,
		MDNSEnabled:    cfg.MDNS.Enabled,
		MDNSHostname:   cfg.MDNS.Hostname,
		HWAccelDefault: cfg.Streaming.HWAccel,
		AllowedOrigins: api.AllowedOrigins(cfg),
		Logger:        logger,
		Metrics:       metrics,
		LogBuffer:     logBuffer,
		// One shared limiter across every SSE surface. Defaults
		// (100 global, 5 per-user) are sized for a household-scale
		// self-hosted server; if a deployment grows, lift these to
		// config rather than tweaking the constants.
		SSELimiter:       handlers.NewSSELimiter(handlers.DefaultSSEGlobalMax, handlers.DefaultSSEPerUserMax),
		HostMetrics:      hostMetrics,
		ConfigPath:       configPath,
		RestartRequester: restartRequester,
		Uploads:          uploadsHandler,
		UploadsAudit:     repos.UploadAudit,
		Permissions:      auth.NewPermissionChecker(repos.Users),
		UserRepo:         repos.Users,
		CorsRegistry:     corsRegistry,
		CorsOriginsRepo:  repos.CorsOrigins,
		AuditLog:         repos.AuditLog,
		Audit:            auditService,
		Updates:          updateService,
	})

	server := &http.Server{
		Addr:        cfg.Server.Addr(),
		Handler:     router,
		ReadTimeout: 15 * time.Second,
		// WriteTimeout default 30s. Cubre el 95 % de los handlers
		// (JSON CRUD bajo /api/v1/*) y evita que un cliente lento
		// consumiendo a 1 byte/segundo deje una goroutine de servidor
		// viva indefinidamente. Los ~15 handlers streaming (HLS,
		// SSE, file download, peer stream proxy) llaman
		// `handlers.DisableWriteDeadline(w)` al inicio para opt-out
		// explícito. Cierra el olor Q de la auditoría 2026-05-14.
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// ═══ Phase 6: Start ═══
	go func() {
		logger.Info("server started", "addr", cfg.Server.Addr())
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			cancel()
		}
	}()

	// ═══ Phase 7: Wait for shutdown ═══
	return waitForShutdown(ctx, cancel, server, lc, database, cfg.Database.Driver, logger)
}

// waitForShutdown bloquea hasta SIGINT/SIGTERM o ctx-cancel, luego
// dirige el teardown en tres fases (ver docstring de `lifecycle`).
// shutdownCtx bounds el budget total a 30s — tareas que excedan se
// cancelan en lugar de bloquear el binario indefinidamente.
func waitForShutdown(
	ctx context.Context,
	cancel context.CancelFunc,
	server *http.Server,
	lc *lifecycle,
	database *sql.DB,
	dbDriver string,
	logger *slog.Logger,
) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info("shutdown signal received", "signal", sig)
	case <-ctx.Done():
		logger.Info("context cancelled, shutting down")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	logger.Info("starting graceful shutdown...")

	// Fase 1 — workers en add-order. Independientes de HTTP; los
	// paramos primero para que no generen actividad nueva mientras
	// los services se van bajando.
	lc.stopWorkers(shutdownCtx, logger)

	// Fase 2 — HTTP drain. Espera a que los requests in-flight
	// terminen; bounded por shutdownCtx (los handlers streaming
	// llaman a DisableWriteDeadline pero el ctx del request se
	// cancela igual al expirar el shutdown budget).
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server shutdown error", "error", err)
	}
	logger.Info("HTTP server stopped")

	// Fase 3 — services HTTP-coupled en LIFO. El último registrado es
	// el primero parado; los que dependen de otros suelen registrarse
	// más tarde, así que LIFO respeta el grafo natural.
	lc.stopServices(shutdownCtx, logger)

	// Cancel root context AFTER services drained — algunos services
	// usan el ctx interno para señalar shutdown de sub-goroutines.
	cancel()

	// Refresh sqlite query-planner stats before closing so the next
	// process starts with up-to-date analysis. PRAGMA optimize is a
	// no-op for tables que no han cambiado; best-effort y nunca
	// bloquea shutdown. No-op en Postgres.
	optimizeCtx, optimizeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	db.Optimize(optimizeCtx, dbDriver, database, logger)
	optimizeCancel()

	if err := database.Close(); err != nil {
		logger.Error("database close error", "error", err)
	}
	logger.Info("database closed")

	logger.Info("shutdown complete")
	return nil
}
