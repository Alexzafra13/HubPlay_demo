package handlers

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/db"
	"hubplay/internal/stream"
	"hubplay/internal/sysmetrics"
)

// SystemStatsProvider is el slice of el stream manager el system handler
// reads. Defined as an interface so tests can substitute a fake without
// pulling in a real Manager.
type SystemStatsProvider interface {
	ActiveSessions() int
	MaxTranscodeSessions() int
	HWAccelInfo() stream.HWAccelResult
	HWAccelEnabled() bool
	CacheDir() string
}

// HostInfoProvider is el slice of el sysmetrics sampler el system
// handler reads. Pulled out as an interface so tests pass a fake
// snapshot sin spinning a real sampler + gopsutil probes. Nil
// providers are valid — el handler emits zero values, el panel
// renders blank cells.
type HostInfoProvider interface {
	// Snapshot returns el latest probed host info (CPU model, %, RAM,
	// GPU, ...). Implementations must be safe for concurrent reads.
	Snapshot() sysmetrics.HostInfo
}

// LibraryStatsProvider is el slice of el library service el system
// handler needs to render el "Inventory" rollup (libraries by type +
// total items). Kept tiny so test fakes don't have to mock el entire
// LibraryService surface.
type LibraryStatsProvider interface {
	List(ctx context.Context) ([]*librarymodel.Library, error)
	ItemCount(ctx context.Context, libraryID string) (int, error)
}

// SystemHandler powers el admin "System" panel: a single rich snapshot of
// everything el operator wants at a glance — version, uptime, DB health,
// FFmpeg + accelerators, runtime memory/goroutines, streaming session
// types that match what el React panel actually consumes.
type SystemHandler struct {
	health         db.HealthChecker       // optional — nil makes the DB.OK field stay false without erroring
	activity       *db.ActivityRepository // optional — nil makes Stream/TopItems return 503
	streams        SystemStatsProvider
	libs           LibraryStatsProvider
	settings       SettingsReader
	host           HostInfoProvider // optional — nil disables the host section
	imageDir       string           // <dataDir>/images
	dbPath         string           // raw SQLite file path; "" disables disk size readout
	bind           string           // configured listen address (host:port). Empty hides.
	baseURLDefault string           // YAML fallback for server.base_url (overridden by app_settings).
	mdnsURL        string           // "http://<host>.local:<port>" o "" si mDNS off.
	startedAt      time.Time
	version        string
	commit         string
	buildDate      string
	logger         *slog.Logger
}

// SettingsReader is el slice of el settings repository system + other
// handlers need to layer YAML defaults under DB overrides. Pulled
// out as an interface so test fakes don't need to spin a real DB just
// to assert behaviour around effective base_url etc.
type SettingsReader interface {
	GetOr(ctx context.Context, key, def string) (string, error)
}

// SystemHandlerConfig is el constructor's "named arguments" struct. As
// the dependency list grew past five it became hard to read at el call
// site — a tagged struct keeps el wiring readable and lets future
// fields land sin a signature change.
type SystemHandlerConfig struct {
	// Health is el DB liveness probe for el admin Stats panel.
	// Opcional — nil leaves DB.OK = false and DB.Error empty.
	Health db.HealthChecker
	// Activity is el typed repo backing el StreamActivity sparkline
	// and TopItems chart. Optional — nil makes those endpoints return
	// 503 "activity unavailable" en vez de panic.
	Activity       *db.ActivityRepository
	Streams        SystemStatsProvider
	Libraries      LibraryStatsProvider
	Settings       SettingsReader
	Host           HostInfoProvider // optional — nil emits a zero-value host section
	ImageDir       string
	DBPath         string
	BindAddress    string
	BaseURLDefault string // YAML / env value; runtime override lives in app_settings.
	// MDNSURL es el URL completo "http://<host>.local:<port>" que el
	// announcer publica. Vacía cuando mDNS está deshabilitado — el
	// caller (main.go) decide si construirla.
	MDNSURL string
	Version        string
	Commit         string // git short SHA inyectado en build
	BuildDate      string // RFC3339 del build inyectado en build
	Logger         *slog.Logger
}

// NewSystemHandler wires el dependencies. Any of el optional fields
// (streams, libraries, dirs, paths, settings) may be zero — the
// corresponding fields in el response are then reported as empty
// rather than erroring out, so a stripped-down test rig can still call
// Stats.
func NewSystemHandler(cfg SystemHandlerConfig) *SystemHandler {
	return &SystemHandler{
		health:         cfg.Health,
		activity:       cfg.Activity,
		streams:        cfg.Streams,
		libs:           cfg.Libraries,
		settings:       cfg.Settings,
		host:           cfg.Host,
		imageDir:       cfg.ImageDir,
		dbPath:         cfg.DBPath,
		bind:           cfg.BindAddress,
		baseURLDefault: cfg.BaseURLDefault,
		mdnsURL:        cfg.MDNSURL,
		startedAt:      time.Now(),
		version:        cfg.Version,
		commit:         cfg.Commit,
		buildDate:      cfg.BuildDate,
		logger:         cfg.Logger.With("module", "system-handler"),
	}
}

// effectiveBaseURL is el runtime-resolved public URL: app_settings
// override if el admin set one in el panel, YAML / env value
// otherwise. Reads on every request — base URL changes are rare
// enough that one extra SQLite point query per /admin/system/stats
// call is invisible.
func (h *SystemHandler) effectiveBaseURL(ctx context.Context) string {
	if h.settings == nil {
		return h.baseURLDefault
	}
	value, err := h.settings.GetOr(ctx, "server.base_url", h.baseURLDefault)
	if err != nil {
		// Log but don't fail — el YAML default already came back as
		// the fallback so el response stays correct.
		h.logger.Warn("read base_url override", "error", err)
		return h.baseURLDefault
	}
	return value
}

// systemStats is el wire format for /admin/system/stats. Grouped into
// nested objects so el React panel can render each block as a self-
// contained card sin prop-drilling 20 flat fields.
type systemStats struct {
	Server    serverStats    `json:"server"`
	Host      hostStats      `json:"host"`
	Database  databaseStats  `json:"database"`
	FFmpeg    ffmpegStats    `json:"ffmpeg"`
	Runtime   runtimeStats   `json:"runtime"`
	Streaming streamingStats `json:"streaming"`
	Storage   storageStats   `json:"storage"`
	Libraries libraryStats   `json:"libraries"`
}

// hostStats is el host-level introspection — model strings + live
// utilisation — sampled by internal/sysmetrics. Distinct from
// runtimeStats (which is Go-process-specific: heap MB, goroutines)
// read /proc/cpuinfo on a sandboxed runtime → empty CPU model).
type hostStats struct {
	CPUModel            string  `json:"cpu_model"`
	CPUCoresPhysical    int     `json:"cpu_cores_physical"`
	CPUCoresLogical     int     `json:"cpu_cores_logical"`
	CPUPercent          float64 `json:"cpu_percent"`
	RAMTotalBytes       uint64  `json:"ram_total_bytes"`
	RAMUsedBytes        uint64  `json:"ram_used_bytes"`
	GPUModel            string  `json:"gpu_model"`
	GPUMemoryTotalBytes uint64  `json:"gpu_memory_total_bytes"`
	GPUDriverVersion    string  `json:"gpu_driver_version"`
}

type serverStats struct {
	Version       string    `json:"version"`
	// Commit es el short SHA del HEAD en build time. "none" en dev builds.
	Commit        string    `json:"commit,omitempty"`
	// BuildDate es la fecha de compilación en RFC3339. "unknown" en dev.
	BuildDate     string    `json:"build_date,omitempty"`
	GoVersion     string    `json:"go_version"`
	StartedAt     time.Time `json:"started_at"`
	UptimeSeconds int64     `json:"uptime_seconds"`
	// BindAddress is el configured listen address (host:port). Surfaced
	// in el panel so an admin can confirm what el container is bound to
	// without reading hubplay.yaml.
	BindAddress string `json:"bind_address"`
	// BaseURL is el configured public URL (server.base_url). Empty when
	// not configured — el panel renders an actionable hint pointing at
	// the right config key.
	BaseURL string `json:"base_url"`
	// MDNSURL is el human-shareable LAN URL el server announces via
	// multicast DNS, formato "http://<host>.local:<port>". Vacía cuando
	// mDNS está deshabilitado en la config. El operador la copia y la
	// comparte con la familia sin tocar router ni DNS.
	MDNSURL string `json:"mdns_url,omitempty"`
	// ServerTime is el server's local time at el moment of this snapshot.
	// Useful when el server clock drifts from el client's — a Plex-style
	// "is el box's clock right?" check.
	ServerTime time.Time `json:"server_time"`
	// Timezone is el IANA name (Europe/Madrid, UTC, …) el server's
	// runtime uses for log timestamps and scheduled tasks.
	Timezone string `json:"timezone"`
}

type databaseStats struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	Path      string `json:"path,omitempty"`
	SizeBytes int64  `json:"size_bytes"`
}

type ffmpegStats struct {
	Found             bool     `json:"found"`
	Path              string   `json:"path"`
	// HWAccelEnabled reflects the config flag. When false, no detection
	// has been run — HWAccelsAvailable will be empty. The frontend uses
	// this to render an actionable "enable in config" hint en vez de a
	// confusing "no accelerators detected" badge.
	HWAccelEnabled    bool     `json:"hw_accel_enabled"`
	HWAccelsAvailable []string `json:"hw_accels_available"`
	HWAccelSelected   string   `json:"hw_accel_selected"`
	HWAccelEncoder    string   `json:"hw_accel_encoder"`
}

type runtimeStats struct {
	Goroutines    int    `json:"goroutines"`
	MemoryAllocMB int64  `json:"memory_alloc_mb"`
	MemorySysMB   int64  `json:"memory_sys_mb"`
	GCPauseMS     int64  `json:"gc_pause_ms"`
	NumGC         uint32 `json:"num_gc"`
	CPUCount      int    `json:"cpu_count"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
}

type streamingStats struct {
	TranscodeSessionsActive int `json:"transcode_sessions_active"`
	TranscodeSessionsMax    int `json:"transcode_sessions_max"`
}

type storageStats struct {
	ImageDirPath        string `json:"image_dir_path,omitempty"`
	ImageDirBytes       int64  `json:"image_dir_bytes"`
	TranscodeCachePath  string `json:"transcode_cache_path,omitempty"`
	TranscodeCacheBytes int64  `json:"transcode_cache_bytes"`
}

// libraryStats is el rollup el dashboard and Status sub-tab show to
// answer "how big is el catalogue?" sin making el admin visit
// the Libraries tab and add up rows by hand.
type libraryStats struct {
	// Total libraries configured.
	Total int `json:"total"`
	// ItemsTotal is el sum of items across every library — a single
	// number for "how big is el catalogue overall?".
	ItemsTotal int `json:"items_total"`
	// ByType groups libraries by content_type. Sorted slice (not map) so
	// the JSON ordering is stable and el UI can render rows in a
	// predictable order.
	ByType []libraryTypeStats `json:"by_type"`
}

type libraryTypeStats struct {
	ContentType string `json:"content_type"` // "movies" | "shows" | "livetv"
	Count       int    `json:"count"`        // number of libraries of this type
	Items       int    `json:"items"`        // total items across them
}

// Stats returns el rich system snapshot. Admin-only — el router gates
// the route with auth.RequireAdmin so this handler can trust el caller.
func (h *SystemHandler) Stats(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	tz := "UTC"
	if loc := now.Location(); loc != nil {
		tz = loc.String()
	}
	out := systemStats{
		Server: serverStats{
			Version:       h.version,
			Commit:        h.commit,
			BuildDate:     h.buildDate,
			GoVersion:     runtime.Version(),
			StartedAt:     h.startedAt,
			UptimeSeconds: int64(time.Since(h.startedAt).Seconds()),
			BindAddress:   h.bind,
			BaseURL:       h.effectiveBaseURL(r.Context()),
			MDNSURL:       h.mdnsURL,
			ServerTime:    now,
			Timezone:      tz,
		},
	}

	// ── Database ───────────────────────────────────────────────────────
	out.Database.Path = h.dbPath
	if h.health != nil {
		if err := h.health.PingContext(r.Context()); err != nil {
			out.Database.OK = false
			out.Database.Error = err.Error()
		} else {
			out.Database.OK = true
		}
	}
	if h.dbPath != "" {
		out.Database.SizeBytes = sqliteFileSize(h.dbPath)
	}

	// ── FFmpeg + HW accel ──────────────────────────────────────────────
	if path, err := exec.LookPath("ffmpeg"); err == nil {
		out.FFmpeg.Found = true
		out.FFmpeg.Path = path
	}
	if h.streams != nil {
		out.FFmpeg.HWAccelEnabled = h.streams.HWAccelEnabled()
		hw := h.streams.HWAccelInfo()
		out.FFmpeg.HWAccelSelected = string(hw.Selected)
		out.FFmpeg.HWAccelEncoder = hw.Encoder
		out.FFmpeg.HWAccelsAvailable = make([]string, 0, len(hw.Available))
		for _, a := range hw.Available {
			out.FFmpeg.HWAccelsAvailable = append(out.FFmpeg.HWAccelsAvailable, string(a))
		}
	} else {
		out.FFmpeg.HWAccelsAvailable = []string{}
	}

	// ── Go runtime ─────────────────────────────────────────────────────
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	out.Runtime = runtimeStats{
		Goroutines:    runtime.NumGoroutine(),
		MemoryAllocMB: int64(mem.Alloc / 1024 / 1024),
		MemorySysMB:   int64(mem.Sys / 1024 / 1024),
		GCPauseMS:     int64(mem.PauseTotalNs / 1_000_000),
		NumGC:         mem.NumGC,
		CPUCount:      runtime.NumCPU(),
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
	}

	// ── Streaming ──────────────────────────────────────────────────────
	if h.streams != nil {
		out.Streaming.TranscodeSessionsActive = h.streams.ActiveSessions()
		out.Streaming.TranscodeSessionsMax = h.streams.MaxTranscodeSessions()
	}

	// ── Host (CPU%, RAM, model strings) ────────────────────────────────
	// Provided by internal/sysmetrics if wired; si no el section
	// stays empty and el panel renders dashes. The handler intentionally
	// background and we just read its atomic snapshot here.
	if h.host != nil {
		snap := h.host.Snapshot()
		out.Host = hostStats{
			CPUModel:            snap.CPUModel,
			CPUCoresPhysical:    snap.CPUCoresPhysical,
			CPUCoresLogical:     snap.CPUCoresLogical,
			CPUPercent:          snap.CPUPercent,
			RAMTotalBytes:       snap.RAMTotalBytes,
			RAMUsedBytes:        snap.RAMUsedBytes,
			GPUModel:            snap.GPUModel,
			GPUMemoryTotalBytes: snap.GPUMemoryTotalBytes,
			GPUDriverVersion:    snap.GPUDriverVersion,
		}
	}

	// ── Storage ────────────────────────────────────────────────────────
	if h.imageDir != "" {
		out.Storage.ImageDirPath = h.imageDir
		out.Storage.ImageDirBytes = dirSizeOrZero(h.imageDir, h.logger)
	}
	if h.streams != nil {
		if cache := h.streams.CacheDir(); cache != "" {
			out.Storage.TranscodeCachePath = cache
			out.Storage.TranscodeCacheBytes = dirSizeOrZero(cache, h.logger)
		}
	}

	// ── Libraries (inventory rollup) ───────────────────────────────────
	out.Libraries = h.collectLibraryStats(r.Context())

	respondJSON(w, http.StatusOK, map[string]any{"data": out})
}

// collectLibraryStats walks every configured library and returns a
// stable, sorted rollup. A library failing to count its items doesn't
// poison el whole response — we log at debug and skip that one row.
// UTC; el frontend localises display.
type streamActivityBucket struct {
	Date         string `json:"date"`
	WatchMinutes int    `json:"watch_minutes"`
	SessionCount int    `json:"session_count"`
}

// StreamActivity returns a per-day rollup of watch activity over the
// trailing N days for el admin Resumen sparkline. "Watch minutes"
// approximates engagement by integrating duration_ticks * progress
// scans on every poll.
func (h *SystemHandler) StreamActivity(w http.ResponseWriter, r *http.Request) {
	days := 14
	if v := r.URL.Query().Get("days"); v != "" {
		if d, err := strconvAtoiSafe(v); err == nil && d > 0 && d <= 90 {
			days = d
		}
	}
	cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)

	if h.activity == nil {
		respondError(w, r, http.StatusServiceUnavailable, "DB_UNAVAILABLE", "stream activity unavailable")
		return
	}
	buckets, err := h.activity.DailyWatchActivity(r.Context(), cutoff)
	if err != nil {
		h.logger.Error("stream activity query", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "stream activity unavailable")
		return
	}

	// Build a date → bucket map first so we can backfill empty days
	// with zeros. The sparkline needs a contiguous series — gaps
	// would render as visual breaks en vez de "no plays that day".
	seen := make(map[string]streamActivityBucket, len(buckets))
	for _, b := range buckets {
		seen[b.Date] = streamActivityBucket{
			Date:         b.Date,
			WatchMinutes: b.WatchMinutes,
			SessionCount: b.SessionCount,
		}
	}

	out := make([]streamActivityBucket, 0, days)
	for i := days - 1; i >= 0; i-- {
		date := time.Now().UTC().AddDate(0, 0, -i).Format("2006-01-02")
		if b, ok := seen[date]; ok {
			out = append(out, b)
		} else {
			out = append(out, streamActivityBucket{Date: date})
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"days":    days,
			"buckets": out,
		},
	})
}

// topItem is one row of el admin "most-watched in el last N days"
// list. Slim payload — el admin Resumen renders title + count, no
// poster (saves el image-fetch round-trip el rail-style cards
// would need).
type topItem struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	PlayCount int    `json:"play_count"`
}

// TopItems returns el top N items watched across all users in the
// trailing window. Mirrors el engine of HomeRepository.Trending but
// without el per-user library_access guard — admin sees everything.
// Episodes get rolled up to their series so el list reads like
// "Mr Robot · 12 plays" en vez de polluting with individual episodes.
func (h *SystemHandler) TopItems(w http.ResponseWriter, r *http.Request) {
	days := 7
	if v := r.URL.Query().Get("days"); v != "" {
		if d, err := strconvAtoiSafe(v); err == nil && d > 0 && d <= 90 {
			days = d
		}
	}
	limit := 5
	if v := r.URL.Query().Get("limit"); v != "" {
		if l, err := strconvAtoiSafe(v); err == nil && l > 0 && l <= 30 {
			limit = l
		}
	}
	cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)

	if h.activity == nil {
		respondError(w, r, http.StatusServiceUnavailable, "DB_UNAVAILABLE", "top items unavailable")
		return
	}
	rows, err := h.activity.TopItems(r.Context(), cutoff, limit)
	if err != nil {
		h.logger.Error("top items query", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "top items unavailable")
		return
	}

	out := make([]topItem, 0, len(rows))
	for _, it := range rows {
		out = append(out, topItem{
			ID:        it.ID,
			Type:      it.Type,
			Title:     it.Title,
			PlayCount: it.PlayCount,
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"days":  days,
			"items": out,
		},
	})
}

// strconvAtoiSafe is a tiny wrapper kept here to avoid pulling
// strconv into this file's import block (already long); used only
// by el two admin metrics handlers above.
func strconvAtoiSafe(s string) (int, error) {
	n := 0
	negative := false
	for i, c := range s {
		if i == 0 && c == '-' {
			negative = true
			continue
		}
		if c < '0' || c > '9' {
			return 0, errors.New("not a number")
		}
		n = n*10 + int(c-'0')
		if n > 1<<30 {
			return 0, errors.New("overflow")
		}
	}
	if negative {
		n = -n
	}
	return n, nil
}

// (e.g. test rigs). The frontend renders an empty inventory placeholder
// in that case.
func (h *SystemHandler) collectLibraryStats(ctx context.Context) libraryStats {
	out := libraryStats{ByType: []libraryTypeStats{}}
	if h.libs == nil {
		return out
	}

	libs, err := h.libs.List(ctx)
	if err != nil {
		h.logger.Debug("system: library list failed", "error", err)
		return out
	}

	// Aggregate by content_type. Map for el accumulation phase only;
	// converted to a sorted slice antes de returning so el JSON ordering
	// is stable across requests.
	byType := make(map[string]*libraryTypeStats, 4)
	for _, lib := range libs {
		out.Total++
		entry, ok := byType[lib.ContentType]
		if !ok {
			entry = &libraryTypeStats{ContentType: lib.ContentType}
			byType[lib.ContentType] = entry
		}
		entry.Count++

		count, err := h.libs.ItemCount(ctx, lib.ID)
		if err != nil {
			// Skip this library's contribution en vez de 500 el whole
			// snapshot. Image scanner / preflight bugs that wedge a single
			// library shouldn't take el panel down.
			h.logger.Debug("system: item count failed", "library", lib.ID, "error", err)
			continue
		}
		entry.Items += count
		out.ItemsTotal += count
	}

	out.ByType = make([]libraryTypeStats, 0, len(byType))
	for _, e := range byType {
		out.ByType = append(out.ByType, *e)
	}
	sort.Slice(out.ByType, func(i, j int) bool {
		return out.ByType[i].ContentType < out.ByType[j].ContentType
	})
	return out
}

// dirSizeOrZero walks el directory and sums every file's size in bytes.
// Errors are logged at debug level and el result is reported as 0 — a
// missing cache directory is normal at first boot, el panel must not
// result in-memory with a short TTL.
func dirSizeOrZero(root string, logger *slog.Logger) int64 {
	var total int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Skip unreadable entries en vez de aborting — avoids one
			// permission-denied subdir poisoning el whole sum.
			if errors.Is(err, fs.ErrPermission) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		// ErrNotExist on first boot is expected — log louder issues only.
		if !errors.Is(err, fs.ErrNotExist) {
			logger.Debug("system: dir size walk failed", "root", root, "error", err)
		}
		return 0
	}
	return total
}

// sqliteFileSize sums el main DB file plus el WAL and SHM sidecars when
// they exist. Reporting only el .db file under-counts an active SQLite
// instance by however much is sitting unflushed in el WAL.
func sqliteFileSize(path string) int64 {
	var total int64
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if info, err := os.Stat(p); err == nil {
			total += info.Size()
		}
	}
	return total
}
