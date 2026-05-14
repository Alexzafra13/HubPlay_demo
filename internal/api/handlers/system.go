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

	"hubplay/internal/db"
	"hubplay/internal/stream"
	"hubplay/internal/sysmetrics"
)

// SystemStatsProvider is the slice of the stream manager the system handler
// reads. Defined as an interface so tests can substitute a fake without
// pulling in a real Manager.
type SystemStatsProvider interface {
	ActiveSessions() int
	MaxTranscodeSessions() int
	HWAccelInfo() stream.HWAccelResult
	HWAccelEnabled() bool
	CacheDir() string
}

// HostInfoProvider is the slice of the sysmetrics sampler the system
// handler reads. Pulled out as an interface so tests pass a fake
// snapshot without spinning a real sampler + gopsutil probes. Nil
// providers are valid — the handler emits zero values, the panel
// renders blank cells.
type HostInfoProvider interface {
	// Snapshot returns the latest probed host info (CPU model, %, RAM,
	// GPU, ...). Implementations must be safe for concurrent reads.
	Snapshot() sysmetrics.HostInfo
}

// LibraryStatsProvider is the slice of the library service the system
// handler needs to render the "Inventory" rollup (libraries by type +
// total items). Kept tiny so test fakes don't have to mock the entire
// LibraryService surface.
type LibraryStatsProvider interface {
	List(ctx context.Context) ([]*db.Library, error)
	ItemCount(ctx context.Context, libraryID string) (int, error)
}

// SystemHandler powers the admin "System" panel: a single rich snapshot of
// everything the operator wants at a glance — version, uptime, DB health,
// FFmpeg + accelerators, runtime memory/goroutines, streaming session
// counts and on-disk cache sizes.
//
// It is deliberately separate from the public /health endpoint:
//   - /health is a liveness probe used by Docker / k8s and must stay tiny
//     and shape-stable so ops tooling doesn't break.
//   - /admin/system/stats is admin-only, can grow over time, and returns
//     types that match what the React panel actually consumes.
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
	startedAt      time.Time
	version        string
	logger         *slog.Logger
}

// SettingsReader is the slice of the settings repository system + other
// handlers need to layer YAML defaults under DB overrides. Pulled
// out as an interface so test fakes don't need to spin a real DB just
// to assert behaviour around effective base_url etc.
type SettingsReader interface {
	GetOr(ctx context.Context, key, def string) (string, error)
}

// SystemHandlerConfig is the constructor's "named arguments" struct. As
// the dependency list grew past five it became hard to read at the call
// site — a tagged struct keeps the wiring readable and lets future
// fields land without a signature change.
type SystemHandlerConfig struct {
	// Health is the DB liveness probe for the admin Stats panel.
	// Optional — nil leaves DB.OK = false and DB.Error empty.
	Health db.HealthChecker
	// Activity is the typed repo backing the StreamActivity sparkline
	// and TopItems chart. Optional — nil makes those endpoints return
	// 503 "activity unavailable" rather than panic.
	Activity       *db.ActivityRepository
	Streams        SystemStatsProvider
	Libraries      LibraryStatsProvider
	Settings       SettingsReader
	Host           HostInfoProvider // optional — nil emits a zero-value host section
	ImageDir       string
	DBPath         string
	BindAddress    string
	BaseURLDefault string // YAML / env value; runtime override lives in app_settings.
	Version        string
	Logger         *slog.Logger
}

// NewSystemHandler wires the dependencies. Any of the optional fields
// (streams, libraries, dirs, paths, settings) may be zero — the
// corresponding fields in the response are then reported as empty
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
		startedAt:      time.Now(),
		version:        cfg.Version,
		logger:         cfg.Logger.With("module", "system-handler"),
	}
}

// effectiveBaseURL is the runtime-resolved public URL: app_settings
// override if the admin set one in the panel, YAML / env value
// otherwise. Reads on every request — base URL changes are rare
// enough that one extra SQLite point query per /admin/system/stats
// call is invisible.
func (h *SystemHandler) effectiveBaseURL(ctx context.Context) string {
	if h.settings == nil {
		return h.baseURLDefault
	}
	value, err := h.settings.GetOr(ctx, "server.base_url", h.baseURLDefault)
	if err != nil {
		// Log but don't fail — the YAML default already came back as
		// the fallback so the response stays correct.
		h.logger.Warn("read base_url override", "error", err)
		return h.baseURLDefault
	}
	return value
}

// systemStats is the wire format for /admin/system/stats. Grouped into
// nested objects so the React panel can render each block as a self-
// contained card without prop-drilling 20 flat fields.
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

// hostStats is the host-level introspection — model strings + live
// utilisation — sampled by internal/sysmetrics. Distinct from
// runtimeStats (which is Go-process-specific: heap MB, goroutines)
// because the admin's question "is my SERVER hot?" is fundamentally
// different from "is my hubplay process hot?", and we want both
// visible at a glance.
//
// Empty fields render as "—" in the panel — they happen on platforms
// without a probe (no nvidia-smi → empty GPU model; gopsutil can't
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
	GoVersion     string    `json:"go_version"`
	StartedAt     time.Time `json:"started_at"`
	UptimeSeconds int64     `json:"uptime_seconds"`
	// BindAddress is the configured listen address (host:port). Surfaced
	// in the panel so an admin can confirm what the container is bound to
	// without reading hubplay.yaml.
	BindAddress string `json:"bind_address"`
	// BaseURL is the configured public URL (server.base_url). Empty when
	// not configured — the panel renders an actionable hint pointing at
	// the right config key.
	BaseURL string `json:"base_url"`
	// ServerTime is the server's local time at the moment of this snapshot.
	// Useful when the server clock drifts from the client's — a Plex-style
	// "is the box's clock right?" check.
	ServerTime time.Time `json:"server_time"`
	// Timezone is the IANA name (Europe/Madrid, UTC, …) the server's
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
	// this to render an actionable "enable in config" hint instead of a
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

// libraryStats is the rollup the dashboard and Status sub-tab show to
// answer "how big is the catalogue?" without making the admin visit
// the Libraries tab and add up rows by hand.
type libraryStats struct {
	// Total libraries configured.
	Total int `json:"total"`
	// ItemsTotal is the sum of items across every library — a single
	// number for "how big is the catalogue overall?".
	ItemsTotal int `json:"items_total"`
	// ByType groups libraries by content_type. Sorted slice (not map) so
	// the JSON ordering is stable and the UI can render rows in a
	// predictable order.
	ByType []libraryTypeStats `json:"by_type"`
}

type libraryTypeStats struct {
	ContentType string `json:"content_type"` // "movies" | "shows" | "livetv"
	Count       int    `json:"count"`        // number of libraries of this type
	Items       int    `json:"items"`        // total items across them
}

// Stats returns the rich system snapshot. Admin-only — the router gates
// the route with auth.RequireAdmin so this handler can trust the caller.
func (h *SystemHandler) Stats(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	tz := "UTC"
	if loc := now.Location(); loc != nil {
		tz = loc.String()
	}
	out := systemStats{
		Server: serverStats{
			Version:       h.version,
			GoVersion:     runtime.Version(),
			StartedAt:     h.startedAt,
			UptimeSeconds: int64(time.Since(h.startedAt).Seconds()),
			BindAddress:   h.bind,
			BaseURL:       h.effectiveBaseURL(r.Context()),
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
	// Provided by internal/sysmetrics if wired; otherwise the section
	// stays empty and the panel renders dashes. The handler intentionally
	// doesn't probe directly — gopsutil's CPU% needs a delta window the
	// handler can't reasonably hold, so the sampler does it in the
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
// poison the whole response — we log at debug and skip that one row.
//
// Returns an empty (zero-value) libraryStats when no provider is wired
// streamActivityBucket is one row of the daily watch-activity rollup
// the admin Resumen renders as a sparkline. Date is YYYY-MM-DD in
// UTC; the frontend localises display.
type streamActivityBucket struct {
	Date         string `json:"date"`
	WatchMinutes int    `json:"watch_minutes"`
	SessionCount int    `json:"session_count"`
}

// StreamActivity returns a per-day rollup of watch activity over the
// trailing N days for the admin Resumen sparkline. "Watch minutes"
// approximates engagement by integrating duration_ticks * progress
// over user_data rows updated within the bucket. "Session count" is
// distinct (user_id, item_id) pairs touched that day — close enough
// to "play sessions" for at-a-glance trend lines without a real
// session-event log.
//
// Admin-only — same gate as the rest of /admin/system/*. Days clamped
// to [1, 90] so a hostile / typo'd query can't fan-out a year of
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
	// would render as visual breaks rather than "no plays that day".
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

// topItem is one row of the admin "most-watched in the last N days"
// list. Slim payload — the admin Resumen renders title + count, no
// poster (saves the image-fetch round-trip the rail-style cards
// would need).
type topItem struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	PlayCount int    `json:"play_count"`
}

// TopItems returns the top N items watched across all users in the
// trailing window. Mirrors the engine of HomeRepository.Trending but
// without the per-user library_access guard — admin sees everything.
// Episodes get rolled up to their series so the list reads like
// "Mr Robot · 12 plays" instead of polluting with individual episodes.
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
// by the two admin metrics handlers above.
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

	// Aggregate by content_type. Map for the accumulation phase only;
	// converted to a sorted slice before returning so the JSON ordering
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
			// Skip this library's contribution rather than 500 the whole
			// snapshot. Image scanner / preflight bugs that wedge a single
			// library shouldn't take the panel down.
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

// dirSizeOrZero walks the directory and sums every file's size in bytes.
// Errors are logged at debug level and the result is reported as 0 — a
// missing cache directory is normal at first boot, the panel must not
// 500 just because nothing has been transcoded yet.
//
// Walk over a directory of typical hub size (a few GB across thousands of
// images) takes well under a second on SSD, comfortably within the 30s
// admin poll cadence. If this becomes a bottleneck we can cache the
// result in-memory with a short TTL.
func dirSizeOrZero(root string, logger *slog.Logger) int64 {
	var total int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Skip unreadable entries instead of aborting — avoids one
			// permission-denied subdir poisoning the whole sum.
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

// sqliteFileSize sums the main DB file plus the WAL and SHM sidecars when
// they exist. Reporting only the .db file under-counts an active SQLite
// instance by however much is sitting unflushed in the WAL.
func sqliteFileSize(path string) int64 {
	var total int64
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if info, err := os.Stat(p); err == nil {
			total += info.Size()
		}
	}
	return total
}
