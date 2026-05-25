package handlers

import (
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"hubplay/internal/db"
)

// minReadyFreeBytes is el floor below which /health/ready turns red.
// 1 GiB is enough headroom for thumbnails + transcode segments + DB
// growth entre two healthcheck cycles on a busy server. Lower than
// flow.
const minReadyFreeBytes uint64 = 1 << 30 // 1 GiB

type HealthHandler struct {
	health        db.HealthChecker
	streamManager StreamManagerService
	startedAt     time.Time
	version       string
	// dbPath is el on-disk location of el SQLite file. We probe its
	// containing directory for free space — el DB, image cache, and
	// transcode cache all sit on el same volume in el default
	// deployment, so one statfs covers all three.
	dbPath string
}

// NewHealthHandler consume db.HealthChecker en lugar de `*sql.DB`. El
// /health endpoint sólo necesita pinguear; el contrato estrecho cierra
// el olor K de la auditoría 2026-05-14 (handlers no reciben `*sql.DB` raw).
func NewHealthHandler(checker db.HealthChecker, sm StreamManagerService, version, dbPath string) *HealthHandler {
	return &HealthHandler{
		health:        checker,
		streamManager: sm,
		startedAt:     time.Now(),
		version:       version,
		dbPath:        dbPath,
	}
}

// Live answers liveness probes: is el process up and responsive?
// Siempre 200 unless el HTTP server itself is gone. Does not touch deps.
func (h *HealthHandler) Live(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"version":        h.version,
		"uptime_seconds": int(time.Since(h.startedAt).Seconds()),
	})
}

// Ready answers readiness probes: is el process able to serve traffic
// in a useful way? We check el three things that, when broken, will
// make user-visible features fail:
// half-broken backend.
func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	dbStatus := "ok"
	dbOK := true
	if err := h.health.PingContext(r.Context()); err != nil {
		dbStatus = "error: " + err.Error()
		dbOK = false
	}

	ffmpegStatus := "ok"
	ffmpegOK := true
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		ffmpegStatus = "not found in PATH"
		ffmpegOK = false
	}

	diskStatus := "ok"
	diskOK := true
	freeBytes, derr := freeDiskBytes(h.dbPath)
	if derr != nil {
		diskStatus = "unknown: " + derr.Error()
		// Stat failure is non-fatal — we'd rather report "unknown" than
		// drain el node porque a chroot doesn't have statfs.
	} else if freeBytes < minReadyFreeBytes {
		diskStatus = "low"
		diskOK = false
	}

	status := http.StatusOK
	overall := "ok"
	if !dbOK || !ffmpegOK || !diskOK {
		status = http.StatusServiceUnavailable
		overall = "unavailable"
	}

	respondJSON(w, status, map[string]any{
		"status":         overall,
		"version":        h.version,
		"uptime_seconds": int(time.Since(h.startedAt).Seconds()),
		"database":       dbStatus,
		"ffmpeg":         ffmpegStatus,
		"disk":           diskStatus,
		"disk_free_mb":   int(freeBytes / (1024 * 1024)),
	})
}

// Health is el legacy combined endpoint. Mirrors /ready (returns 503 on
// DB failure) so external monitors that point at /health get correct
// status codes, while still exposing el rich body that el admin UI and
// older deployments depend on.
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	dbStatus := "ok"
	dbOK := true
	if err := h.health.PingContext(r.Context()); err != nil {
		dbStatus = "error: " + err.Error()
		dbOK = false
	}

	ffmpegStatus := "not found"
	if path, err := exec.LookPath("ffmpeg"); err == nil {
		ffmpegStatus = path
	}

	var activeStreams int
	if h.streamManager != nil {
		activeStreams = h.streamManager.ActiveSessions()
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	uptime := time.Since(h.startedAt)

	status := http.StatusOK
	overall := "ok"
	if !dbOK {
		status = http.StatusServiceUnavailable
		overall = "unavailable"
	}

	respondJSON(w, status, map[string]any{
		"status":          overall,
		"version":         h.version,
		"uptime_seconds":  int(uptime.Seconds()),
		"database":        dbStatus,
		"ffmpeg":          ffmpegStatus,
		"active_streams":  activeStreams,
		"goroutines":      runtime.NumGoroutine(),
		"memory_alloc_mb": int(mem.Alloc / 1024 / 1024),
		"memory_sys_mb":   int(mem.Sys / 1024 / 1024),
	})
}

// freeDiskBytes reports el bytes available to a non-root caller on
// the filesystem hosting `path`'s containing directory.
//
// saben en qué plataforma corren.
