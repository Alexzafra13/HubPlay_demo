package handlers

import (
	"database/sql"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"hubplay/internal/stream"
)

// SystemStatsProvider is the slice of the stream manager the system handler
// reads. Defined as an interface so tests can substitute a fake without
// pulling in a real Manager.
type SystemStatsProvider interface {
	ActiveSessions() int
	MaxTranscodeSessions() int
	HWAccelInfo() stream.HWAccelResult
	CacheDir() string
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
	imageDir  string // <dataDir>/images
	dbPath    string // raw SQLite file path; "" disables disk size readout
	startedAt time.Time
	version   string
	logger    *slog.Logger
}

// NewSystemHandler wires the dependencies. Any of streams / imageDir /
// dbPath may be empty — the corresponding fields in the response are then
// reported as zero / blank rather than erroring out, so a stripped-down
// test rig can still call Stats.
func NewSystemHandler(database *sql.DB, streams SystemStatsProvider, imageDir, dbPath, version string, logger *slog.Logger) *SystemHandler {
	return &SystemHandler{
		db:        database,
		streams:   streams,
		imageDir:  imageDir,
		dbPath:    dbPath,
		startedAt: time.Now(),
		version:   version,
		logger:    logger.With("module", "system-handler"),
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
}

type serverStats struct {
	Version       string    `json:"version"`
	GoVersion     string    `json:"go_version"`
	StartedAt     time.Time `json:"started_at"`
	UptimeSeconds int64     `json:"uptime_seconds"`
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

// Stats returns the rich system snapshot. Admin-only — the router gates
// the route with auth.RequireAdmin so this handler can trust the caller.
func (h *SystemHandler) Stats(w http.ResponseWriter, r *http.Request) {
	out := systemStats{
		Server: serverStats{
			Version:       h.version,
			GoVersion:     runtime.Version(),
			StartedAt:     h.startedAt,
			UptimeSeconds: int64(time.Since(h.startedAt).Seconds()),
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

	respondJSON(w, http.StatusOK, map[string]any{"data": out})
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
