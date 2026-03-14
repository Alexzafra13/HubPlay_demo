package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"hubplay/internal/auth"
	"hubplay/internal/config"
	"hubplay/internal/library"
	"hubplay/internal/setup"
)

type SetupHandler struct {
	setup  *setup.Service
	auth   *auth.Service
	libs   *library.Service
	config *config.Config
	logger *slog.Logger
}

func NewSetupHandler(
	setupSvc *setup.Service,
	authSvc *auth.Service,
	libSvc *library.Service,
	cfg *config.Config,
	logger *slog.Logger,
) *SetupHandler {
	return &SetupHandler{
		setup:  setupSvc,
		auth:   authSvc,
		libs:   libSvc,
		config: cfg,
		logger: logger,
	}
}

// Status returns whether initial setup is needed.
func (h *SetupHandler) Status(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"needs_setup": h.setup.NeedsSetup(r.Context()),
		},
	})
}

type browseRequest struct {
	Path string `json:"path"`
}

// Browse lists directories at the requested path.
func (h *SetupHandler) Browse(w http.ResponseWriter, r *http.Request) {
	var req browseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}

	if req.Path == "" {
		req.Path = "/"
	}

	result, err := h.setup.BrowseDirectories(req.Path)
	if err != nil {
		h.logger.Warn("browse directories failed", "path", req.Path, "error", err)
		respondError(w, http.StatusBadRequest, "BROWSE_ERROR", err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": result})
}

type createLibrariesRequest struct {
	Libraries []createLibraryEntry `json:"libraries"`
}

type createLibraryEntry struct {
	Name        string   `json:"name"`
	ContentType string   `json:"content_type"`
	Paths       []string `json:"paths"`
}

// CreateLibraries creates one or more libraries during setup.
func (h *SetupHandler) CreateLibraries(w http.ResponseWriter, r *http.Request) {
	var req createLibrariesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}

	if len(req.Libraries) == 0 {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "at least one library is required")
		return
	}

	var created []any
	for _, lib := range req.Libraries {
		result, err := h.libs.Create(r.Context(), library.CreateRequest{
			Name:        lib.Name,
			ContentType: lib.ContentType,
			Paths:       lib.Paths,
		})
		if err != nil {
			handleServiceError(w, err)
			return
		}
		created = append(created, result)
	}

	respondJSON(w, http.StatusCreated, map[string]any{"data": created})
}

type updateSettingsRequest struct {
	TMDbAPIKey         string `json:"tmdb_api_key"`
	TranscodingEnabled bool   `json:"transcoding_enabled"`
	HWAccel            string `json:"hw_accel"`
}

// UpdateSettings updates server settings during setup.
// For MVP this logs the changes; full config persistence comes later.
func (h *SetupHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req updateSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}

	h.logger.Info("setup: settings updated",
		"tmdb_api_key_set", req.TMDbAPIKey != "",
		"transcoding_enabled", req.TranscodingEnabled,
		"hw_accel", req.HWAccel,
	)

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{"status": "ok"},
	})
}

// Capabilities returns system capabilities (FFmpeg, hardware acceleration).
func (h *SetupHandler) Capabilities(w http.ResponseWriter, r *http.Request) {
	caps := h.setup.DetectCapabilities()
	respondJSON(w, http.StatusOK, map[string]any{"data": caps})
}

type completeRequest struct {
	StartScan bool `json:"start_scan"`
}

// Complete marks the setup wizard as finished.
func (h *SetupHandler) Complete(w http.ResponseWriter, r *http.Request) {
	var req completeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}

	if err := h.setup.CompleteSetup(req.StartScan); err != nil {
		h.logger.Error("failed to complete setup", "error", err)
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to complete setup")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{"status": "ok"},
	})
}
