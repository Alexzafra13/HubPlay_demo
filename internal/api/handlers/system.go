package handlers

import (
	"context"
	"database/sql"
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
	db        *sql.DB
	streams   SystemStatsProvider
	libs      LibraryStatsProvider
	imageDir  string // <dataDir>/images
	dbPath    string // raw SQLite file path; "" disables disk size readout
	bind      string // configured listen address (host:port). Empty hides.
	baseURL   string // configured public URL (server.base_url). Empty hides.
	startedAt time.Time
	version   string
	logger    *slog.Logger
}

// SystemHandlerConfig is the constructor's "named arguments" struct. As
// the dependency list grew past five it became hard to read at the call
// site — a tagged struct keeps the wiring readable and lets future
// fields land without a signature change.
type SystemHandlerConfig struct {
	DB           *sql.DB
	Streams      SystemStatsProvider
	Libraries    LibraryStatsProvider
	ImageDir     string
	DBPath       string
	BindAddress  string
	BaseURL      string
	Version      string
	Logger       *slog.Logger
}

// NewSystemHandler wires the dependencies. Any of the optional fields
// (streams, libraries, dirs, paths) may be zero — the corresponding
// fields in the response are then reported as empty rather than
// erroring out, so a stripped-down test rig can still call Stats.
func NewSystemHandler(cfg SystemHandlerConfig) *SystemHandler {
	return &SystemHandler{
		db:        cfg.DB,
		streams:   cfg.Streams,
		libs:      cfg.Libraries,
		imageDir:  cfg.ImageDir,
		dbPath:    cfg.DBPath,
		bind:      cfg.BindAddress,
		baseURL:   cfg.BaseURL,
		startedAt: time.Now(),
		version:   cfg.Version,
		logger:    cfg.Logger.With("module", "system-handler"),
	}
}

// systemStats is the wire format for /admin/system/stats. Grouped into
// nested objects so the React panel can render each block as a self-
// contained card without prop-drilling 20 flat fields.
type systemStats struct {
	Server    serverStats    `json:"server"`
	Database  databaseStats  `json:"database"`
	FFmpeg    ffmpegStats    `json:"ffmpeg"`
	Runtime   runtimeStats   `json:"runtime"`
	Streaming streamingStats `json:"streaming"`
	Storage   storageStats   `json:"storage"`
	Libraries libraryStats   `json:"libraries"`
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
			BaseURL:       h.baseURL,
			ServerTime:    now,
			Timezone:      tz,
		},
	}

	// ── Database ───────────────────────────────────────────────────────
	out.Database.Path = h.dbPath
	if h.db != nil {
		if err := h.db.PingContext(r.Context()); err != nil {
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
