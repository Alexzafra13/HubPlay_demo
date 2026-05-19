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
	"hubplay/internal/scanner"
	"hubplay/internal/setup"
	"hubplay/internal/stream"
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

	// Image storage shared with the HTTP image handler/refresher: the
	// scanner downloads every poster/backdrop here so re-scans and
	// admin refreshes write to the same root. Path-mapping is plain
	// filesystem state, so it's safe to instantiate independent
	// `pathmap.Store`s pointing at the same directory.
	imageDir := filepath.Join(filepath.Dir(cfg.Database.Path), "images")
	scannerPathmap := pathmap.New(imageDir)

	scnr := scanner.New(repos.Items, repos.MediaStreams, repos.Metadata, repos.ExternalIDs, repos.Images, repos.Chapters, repos.People, repos.ItemValues, repos.Studios, repos.Collections, repos.ItemMetadataLocks, providerManager, prober, eventBus, imageDir, scannerPathmap, logger)
	libraryService := library.NewService(repos.Libraries, repos.Items, repos.MediaStreams, repos.Images, repos.Channels, repos.ItemValues, scnr, logger)

	// Periodic SQLite query-planner refresh + FTS5 merge. Fires every
	// 6h once started; first tick is on the interval, not immediately,
	// so boot doesn't pay the cost on top of the cold-start overhead.
	// No-op on Postgres (autovacuum handles ANALYZE on its own schedule).
	stopOptimize := db.StartPeriodicOptimize(ctx, cfg.Database.Driver, database, logger)
	defer stopOptimize()

	// ═══ Phase 4a: Library Scan Scheduler ═══
	scanScheduler := library.NewScheduler(libraryService, logger)
	scanScheduler.Start(ctx)

	// ═══ Phase 4a-bis: Image Refresh Scheduler ═══
	//
	// The scan scheduler reacts to filesystem changes (new files appear in
	// a library); image freshness is a different signal — TMDb periodically
	// publishes better artwork for shows that already exist on disk, and
	// without a periodic sweep nothing ever updates them. Weekly is
	// sufficient: provider art doesn't change daily, and locked images
	// (admin curation) are skipped per-kind by the refresher itself
	// (ADR-003) so curation work is never overwritten.
	imageRefresher := library.NewImageRefresher(
		repos.Items, repos.ExternalIDs, repos.Images, providerManager,
		scannerPathmap, imageDir, logger,
	)
	imageRefreshScheduler := library.NewImageRefreshScheduler(repos.Libraries, imageRefresher, logger)
	imageRefreshScheduler.Start(ctx)

	// ═══ Phase 4a-ter: Episode Segment Detector (skip-intro) ═══
	//
	// Subscribes to library.scan.completed and writes intro / outro /
	// recap markers for each episode by reading its chapter titles.
	// Secondary detector — does NOT block scans, runs in its own
	// goroutine off the bus. The unsubscribe handle is held for the
	// process lifetime; bus.Subscribe leaks the handler if it's
	// never released.
	segmentDetector := library.NewSegmentDetector(
		repos.Items, repos.Chapters, repos.EpisodeSegments, eventBus, logger,
	)
	segmentDetectorUnsub := segmentDetector.Start(ctx)
	defer segmentDetectorUnsub()

	// Phase 4a-quinquies: Audio fingerprint segment detector.
	//
	// Phase 2 of the skip-intro feature — kicks in when the file
	// has no chapter markers (most cases). Listens to the same
	// library.scan.completed event as the chapter detector, runs
	// chromaprint over each episode's first / last few minutes,
	// and writes any common runs as fingerprint-source segments.
	// Disabled at runtime when fpcalc isn't on PATH.
	fingerprinter := library.NewFingerprinter(cfg.Streaming.EffectiveCacheDir())
	segmentFingerprinter := library.NewSegmentFingerprinter(
		repos.Items, repos.EpisodeSegments, fingerprinter, eventBus, logger,
	)
	segmentFingerprinterUnsub := segmentFingerprinter.Start(ctx)
	defer segmentFingerprinterUnsub()

	// ═══ Phase 4a-quater: Filesystem Watcher ═══
	//
	// Reactive complement to scanScheduler — when a file is copied
	// into a library path, the watcher fires a scan within ~2 s
	// instead of waiting for the next scheduled tick (15 min).
	// Fail-soft on platforms without inotify/equivalent (Docker on
	// Windows with bind mounts is the realistic case): the start
	// error is logged and the scheduler keeps doing its job.
	fsWatcher := library.NewFSWatcher(libraryService, logger)
	if err := fsWatcher.Start(ctx); err != nil {
		logger.Warn("filesystem watcher unavailable, scheduler-only mode",
			"error", err)
	} else {
		defer fsWatcher.Stop()
	}

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
	streamManager := stream.NewManager(repos.Items, repos.MediaStreams, streamingCfg, logger)
	streamManager.SetMetrics(observability.NewStreamSink(metrics))
	streamManager.SetEventBus(eventBus)
	// Runtime hook for the playback.force_direct_play admin toggle.
	// Read on every StartSession so the admin can flip it without a
	// restart. Boolean parse mirrors the validator in
	// settings.validateSettingValue (canonical "true"/"false" strings).
	streamManager.SetForceDirectPlayLookup(func(ctx context.Context) bool {
		v, err := repos.Settings.GetOr(ctx, "playback.force_direct_play", "false")
		if err != nil {
			return false
		}
		return v == "true"
	})

	// ═══ Phase 4c: IPTV ═══
	iptvService := iptv.NewService(repos.Channels, repos.EPGPrograms, repos.Libraries, repos.ChannelFavorites, repos.ChannelOrder, repos.LibraryChannelOrder, repos.LibraryEPGSources, repos.ChannelOverrides, repos.ChannelLogoOverrides, repos.ChannelWatchHistory, logger)
	iptvService.SetEventBus(eventBus)
	iptvProxy := iptv.NewStreamProxy(logger)
	// Wire health reporting now that both pieces exist. The proxy
	// records probe outcomes against the channel repo through the
	// service so dead upstreams drop out of the user view.
	iptvProxy.SetHealthReporter(iptvService)

	// Live MPEG-TS → HLS transmux. Required for Xtream Codes /
	// raw-TS providers (most non-public IPTV today). Optional: if
	// disabled in config, the channel-stream handler falls back to
	// the raw passthrough proxy and only HLS providers play in the
	// browser. The work dir is rooted under the streaming cache dir
	// so a single volume mount covers both VOD transcoding and live
	// transmux output.
	var iptvTransmux *iptv.TransmuxManager
	if cfg.IPTV.Transmux.Enabled {
		transmuxCacheDir := filepath.Join(cfg.Streaming.EffectiveCacheDir(), "iptv-hls")
		// Share the proxy's circuit breaker with the transmux manager
		// so failures on either plane (HLS proxy or MPEG-TS transmux)
		// trip the same per-channel cooldown. Without this, a dead
		// Xtream upstream produced a fork-bomb of failed ffmpeg spawns
		// every time the player retried the manifest.
		//
		// Hwaccel reuse: same encoder + decode flags the VOD
		// transcoder picked at boot. If the host has VAAPI / NVENC
		// available, the reencode fallback runs there too — for HEVC
		// → H.264 transcode that's often a 5-10× CPU win, which is
		// what makes the fallback affordable on low-spec hosts.
		hwInfo := streamManager.HWAccelInfo()
		iptvTransmux = iptv.NewTransmuxManager(iptv.TransmuxManagerConfig{
			CacheDir:                 transmuxCacheDir,
			MaxSessions:              cfg.IPTV.Transmux.MaxSessions,
			MaxReencodeSessions:      cfg.IPTV.Transmux.MaxReencodeSessions,
			IdleTimeout:              cfg.IPTV.Transmux.IdleTimeout,
			ReadyTimeout:             cfg.IPTV.Transmux.ReadyTimeout,
			Gate:                     iptvProxy.Breaker(),
			Reporter:                 iptvService,
			Metrics:                  observability.NewIPTVTransmuxSink(metrics),
			ReencodeEncoder:          hwInfo.Encoder,
			ReencodeHWAccelInputArgs: stream.HWAccelInputArgs(hwInfo.Selected),
		}, logger)
		if err := observability.RegisterIPTVTransmuxGauges(metrics, iptvTransmux); err != nil {
			return fmt.Errorf("register iptv transmux gauges: %w", err)
		}
		logger.Info("iptv transmux enabled",
			"cache_dir", transmuxCacheDir,
			"max_sessions", cfg.IPTV.Transmux.MaxSessions,
			"max_reencode_sessions", iptvTransmux.MaxReencodeSessions(),
			"reencode_encoder", hwInfo.Encoder,
			"hwaccel", hwInfo.Selected)
	}

	// Channel logo cache. Mirrors upstream `tvg-logo` URLs to disk
	// so the frontend can load them from a same-origin URL without
	// loosening the img-src CSP, and external hosts don't get to
	// track the user. Construction failure is non-fatal: the
	// handler treats nil as "logo cache disabled" and the React UI
	// falls back to the existing initials/colour avatar.
	var iptvLogoCache *iptv.LogoCache
	logoCacheDir := filepath.Join(cfg.Streaming.EffectiveCacheDir(), "iptv-logos")
	if lc, err := iptv.NewLogoCache(logoCacheDir, logger); err != nil {
		logger.Warn("iptv logo cache disabled", "error", err)
	} else {
		iptvLogoCache = lc
		logger.Info("iptv logo cache enabled", "cache_dir", logoCacheDir)
	}

	// iptv-org logo auto-discovery — fetcher + cache compartido con
	// el resto de assets de imagen. La descarga se hace lazy (en la
	// primera llamada del admin) para no pegar a iptv-org.github.io
	// durante el arranque.
	iptvOrgLogosCachePath := filepath.Join(filepath.Dir(cfg.Database.Path), "images", "iptv-org-channels.json")
	iptvService.SetIPTVOrgLogos(iptv.NewIPTVOrgLogoLookup(iptvOrgLogosCachePath))

	// ═══ Phase 4d: IPTV Scheduler ═══
	// Runs periodic M3U + EPG refreshes per configured schedule so the
	// product no longer requires an admin to click "Refrescar" every
	// morning. Sequential — see scheduler.go for why.
	iptvScheduler := iptv.NewScheduler(repos.IPTVSchedules, iptvService, logger)
	iptvScheduler.Start(ctx)

	// Active stream prober: walks every livetv library every few hours
	// and records a probe outcome against each channel via the same
	// ChannelHealthReporter the proxy uses. Catches dead upstreams on
	// channels nobody happens to be watching, so the user-facing list
	// auto-hides them before a viewer clicks a dead tile.
	iptvProber := iptv.NewProber(nil, iptvService)
	iptvProberWorker := iptv.NewProberWorker(iptvProber, repos.Libraries, repos.Channels, logger)
	iptvProberWorker.Start(ctx)
	iptvService.SetProberWorker(iptvProberWorker)

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
	retentionRunner := retention.New(cfg.Retention, iptvService, federationRepo, logger)
	retentionRunner.Start(ctx)

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
		Libraries:     libraryService,
		StreamManager: streamManager,
		IPTV:          iptvService,
		IPTVProxy:     iptvProxy,
		IPTVTransmux:  iptvTransmux,
		IPTVLogoCache: iptvLogoCache,
		IPTVScheduler: iptvScheduler,
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
		Scanner:       scnr,
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
	// runtime bundles every long-lived component the shutdown path
	// needs to stop or close. Adding a new background service is now
	// a one-line struct-field append plus a Stop call inside
	// waitForShutdown — instead of editing the positional argument
	// list and risking a "forgot to wire it through" leak. The class
	// of bug that fix is intended to prevent is "I added IPTV prober
	// last quarter and only spotted that we never closed it during
	// SIGTERM when CI flaked on a leaked goroutine."
	return waitForShutdown(ctx, cancel, &runtime{
		server:                server,
		streamManager:         streamManager,
		iptvService:           iptvService,
		iptvProxy:             iptvProxy,
		iptvTransmux:          iptvTransmux,
		iptvScheduler:         iptvScheduler,
		iptvProber:            iptvProberWorker,
		scanScheduler:         scanScheduler,
		imageRefreshScheduler: imageRefreshScheduler,
		libraryService:        libraryService,
		authService:           authService,
		retention:             retentionRunner,
		database:              database,
		dbDriver:              cfg.Database.Driver,
		logger:                logger,
	})
}

// runtime is the bag of long-lived components the shutdown path drives.
// Every field here must be safe to call its Stop / Shutdown / Close
// method even when the corresponding feature is disabled (see nil-checks
// in waitForShutdown).
type runtime struct {
	server                *http.Server
	streamManager         *stream.Manager
	iptvService           *iptv.Service
	iptvProxy             *iptv.StreamProxy
	iptvTransmux          *iptv.TransmuxManager
	iptvScheduler         *iptv.Scheduler
	iptvProber            *iptv.ProberWorker
	scanScheduler         *library.Scheduler
	imageRefreshScheduler *library.ImageRefreshScheduler
	libraryService        *library.Service
	authService           *auth.Service
	retention             *retention.Runner
	database              *sql.DB
	dbDriver              string
	logger                *slog.Logger
}

func waitForShutdown(ctx context.Context, cancel context.CancelFunc, rt *runtime) error {
	server := rt.server
	sm := rt.streamManager
	iptvSvc := rt.iptvService
	iptvProxy := rt.iptvProxy
	iptvTransmux := rt.iptvTransmux
	iptvSched := rt.iptvScheduler
	iptvProber := rt.iptvProber
	scheduler := rt.scanScheduler
	imageRefreshSched := rt.imageRefreshScheduler
	librarySvc := rt.libraryService
	authSvc := rt.authService
	retentionRunner := rt.retention
	database := rt.database
	logger := rt.logger
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

	// Stop background services. Stop the IPTV scheduler before the
	// IPTV service itself so an in-flight refresh has a chance to
	// finish recording its outcome against an open DB handle. The
	// shutdownCtx bounds the wait — if a run is stuck past the
	// supervisor deadline the scheduler cancels its in-flight ctx
	// rather than block the whole shutdown.
	iptvSched.Stop(shutdownCtx)
	logger.Info("iptv scheduler stopped")
	if err := iptvProber.Stop(shutdownCtx); err != nil {
		logger.Warn("iptv prober stop", "error", err)
	}
	logger.Info("iptv prober stopped")
	scheduler.Stop()
	logger.Info("scan scheduler stopped")
	imageRefreshSched.Stop()
	logger.Info("image refresh scheduler stopped")
	authSvc.StopSessionCleaner()
	logger.Info("session cleaner stopped")
	retentionRunner.Stop()
	logger.Info("retention runner stopped")

	// Stop HTTP server
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server shutdown error", "error", err)
	}
	logger.Info("HTTP server stopped")

	// Stop all streaming sessions
	sm.Shutdown()
	logger.Info("stream manager stopped")

	// Stop IPTV. El proxy NO drena goroutines aquí: el drain real
	// viene del http.Server.Shutdown previo, que cancela los ctx
	// de los requests en vuelo. ClearRelays solo vacía la
	// contabilidad (audit olor EE).
	iptvProxy.ClearRelays()
	if iptvTransmux != nil {
		iptvTransmux.Shutdown()
	}
	iptvSvc.Shutdown()
	logger.Info("IPTV services stopped")

	// Drain in-flight auto-scan goroutines BEFORE closing the DB so they
	// don't race on "sql: database is closed".
	librarySvc.Shutdown()
	logger.Info("library service stopped")

	// Cancel root context
	cancel()

	// Refresh sqlite query-planner stats before closing so the next
	// process starts with up-to-date analysis. PRAGMA optimize is a
	// no-op for tables that haven't changed; this is best-effort and
	// never blocks shutdown. No-op on Postgres.
	optimizeCtx, optimizeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	db.Optimize(optimizeCtx, rt.dbDriver, database, logger)
	optimizeCancel()

	// Close database
	if err := database.Close(); err != nil {
		logger.Error("database close error", "error", err)
	}
	logger.Info("database closed")

	logger.Info("shutdown complete")
	return nil
}
