package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"hubplay/internal/config"
	"hubplay/internal/db"
	"hubplay/internal/library"
)

type SetupHandler struct {
	setup     SetupService
	auth      AuthService
	libs      LibraryService
	users     UserService
	providers ProviderRepository
	config    *config.Config
	logger    *slog.Logger
}

func NewSetupHandler(
	setupSvc SetupService,
	authSvc AuthService,
	libSvc LibraryService,
	userSvc UserService,
	providerRepo ProviderRepository,
	cfg *config.Config,
	logger *slog.Logger,
) *SetupHandler {
	return &SetupHandler{
		setup:     setupSvc,
		auth:      authSvc,
		libs:      libSvc,
		users:     userSvc,
		providers: providerRepo,
		config:    cfg,
		logger:    logger,
	}
}

// Status returns setup state including the current step so the wizard
// can resume from where it was interrupted (similar to Jellyfin's approach).
// Steps: "account" → "libraries" → "settings" → "complete" → "" (done).
func (h *SetupHandler) Status(w http.ResponseWriter, r *http.Request) {
	needsSetup := h.setup.NeedsSetup(r.Context())

	if !needsSetup {
		respondJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{
				"needs_setup":  false,
				"current_step": "",
			},
		})
		return
	}

	// Determine which step the user is on based on actual state.
	step := "account"

	userCount, err := h.users.Count(r.Context())
	if err != nil {
		h.logger.Warn("setup status: failed to count users", "error", err)
	}

	if userCount > 0 {
		step = "libraries"

		libs, err := h.libs.List(r.Context())
		if err != nil {
			h.logger.Warn("setup status: failed to list libraries", "error", err)
		}

		if len(libs) > 0 {
			step = "settings"
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"needs_setup":  true,
			"current_step": step,
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
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}

	if req.Path == "" {
		req.Path = "/"
	}

	result, err := h.setup.BrowseDirectories(req.Path)
	if err != nil {
		// Details (raw error, requested path) stay in logs only; the client
		// gets a stable code it can map to a UI-friendly message.
		h.logger.Warn("browse directories failed", "path", req.Path, "error", err)
		respondError(w, r, http.StatusBadRequest, "BROWSE_ERROR", "cannot browse this directory")
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
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}

	if len(req.Libraries) == 0 {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "at least one library is required")
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
			handleServiceError(w, r, err)
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
func (h *SetupHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req updateSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}

	// Persist TMDB API key if provided
	if req.TMDbAPIKey != "" && h.providers != nil {
		cfg, err := h.providers.GetByName(r.Context(), "tmdb")
		if err != nil {
			h.logger.Warn("setup: failed to get tmdb provider", "error", err)
		}
		if cfg == nil {
			cfg = &db.ProviderConfig{
				Name:     "tmdb",
				Type:     "metadata",
				Version:  "1.0",
				Status:   "active",
				Priority: 100,
			}
		}
		cfg.APIKey = req.TMDbAPIKey
		if err := h.providers.Upsert(r.Context(), cfg); err != nil {
			h.logger.Error("setup: failed to save tmdb api key", "error", err)
			respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to save TMDB API key")
			return
		}
		h.logger.Info("setup: TMDB API key saved")
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
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}

	if err := h.setup.CompleteSetup(req.StartScan); err != nil {
		h.logger.Error("failed to complete setup", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to complete setup")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{"status": "ok"},
	})
}
