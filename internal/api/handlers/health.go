package handlers

import (
	"database/sql"
	"net/http"
	"os/exec"
	"runtime"
	"time"
)

type HealthHandler struct {
	db            *sql.DB
	streamManager StreamManagerService
	startedAt     time.Time
	version       string
}

func NewHealthHandler(database *sql.DB, sm StreamManagerService, version string) *HealthHandler {
	return &HealthHandler{
		db:            database,
		streamManager: sm,
		startedAt:     time.Now(),
		version:       version,
	}
}

// Live answers liveness probes: is the process up and responsive?
// Always 200 unless the HTTP server itself is gone. Does not touch deps.
func (h *HealthHandler) Live(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"version":        h.version,
		"uptime_seconds": int(time.Since(h.startedAt).Seconds()),
	})
}

// Ready answers readiness probes: is the process able to serve traffic?
// 503 when a critical dependency (DB) is unreachable so load balancers
// drain the node instead of routing requests into a broken backend.
func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	dbStatus := "ok"
	dbOK := true
	if err := h.db.Ping(); err != nil {
		dbStatus = "error: " + err.Error()
		dbOK = false
	}

	status := http.StatusOK
	overall := "ok"
	if !dbOK {
		status = http.StatusServiceUnavailable
		overall = "unavailable"
	}

	respondJSON(w, status, map[string]any{
		"status":         overall,
		"version":        h.version,
		"uptime_seconds": int(time.Since(h.startedAt).Seconds()),
		"database":       dbStatus,
	})
}

// Health is the legacy combined endpoint. Mirrors /ready (returns 503 on
// DB failure) so external monitors that point at /health get correct
// status codes, while still exposing the rich body that the admin UI and
// older deployments depend on.
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	dbStatus := "ok"
	dbOK := true
	if err := h.db.Ping(); err != nil {
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
