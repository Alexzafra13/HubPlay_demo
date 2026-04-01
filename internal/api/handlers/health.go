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

func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	// Database check
	dbStatus := "ok"
	if err := h.db.Ping(); err != nil {
		dbStatus = "error: " + err.Error()
	}

	// FFmpeg check
	ffmpegStatus := "not found"
	if path, err := exec.LookPath("ffmpeg"); err == nil {
		ffmpegStatus = path
	}

	// Streaming stats
	var activeStreams int
	if h.streamManager != nil {
		activeStreams = h.streamManager.ActiveSessions()
	}

	// Memory stats
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	uptime := time.Since(h.startedAt)

	respondJSON(w, http.StatusOK, map[string]any{
		"status":            "ok",
		"version":           h.version,
		"uptime_seconds":    int(uptime.Seconds()),
		"database":          dbStatus,
		"ffmpeg":            ffmpegStatus,
		"active_streams":    activeStreams,
		"goroutines":        runtime.NumGoroutine(),
		"memory_alloc_mb":   int(mem.Alloc / 1024 / 1024),
		"memory_sys_mb":     int(mem.Sys / 1024 / 1024),
	})
}
