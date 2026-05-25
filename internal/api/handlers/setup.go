package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"hubplay/internal/config"
	"hubplay/internal/db"
	"hubplay/internal/library"
)

// SetupDatabaseSaver is the slice of setup.Service the wizard needs to
// persist a candidate database driver+DSN before the operator
// finishes the rest of the wizard. Kept as a tiny interface so tests
// drop in a fake without instantiating the whole setup service.
type SetupDatabaseSaver interface {
	SaveDatabaseConfig(driver, path, dsn string) error
}

type SetupHandler struct {
	setup     SetupService
	dbSaver   SetupDatabaseSaver
	auth      AuthService
	libs      LibraryService
	users     UserService
	providers ProviderRepository
	config    *config.Config
	restart   *config.RestartRequester
	logger    *slog.Logger
}

// SetupHandlerConfig collects the dependencies of the wizard's
// HTTP-facing handler. A tagged struct keeps the wiring readable as
// the dep list has grown to include the database saver and restart
// requester for the wizard's "Database" step.
type SetupHandlerConfig struct {
	Setup     SetupService
	DBSaver   SetupDatabaseSaver
	Auth      AuthService
	Libraries LibraryService
	Users     UserService
	Providers ProviderRepository
	Config    *config.Config
	Restart   *config.RestartRequester
	Logger    *slog.Logger
}

func NewSetupHandler(cfg SetupHandlerConfig) *SetupHandler {
	return &SetupHandler{
		setup:     cfg.Setup,
		dbSaver:   cfg.DBSaver,
		auth:      cfg.Auth,
		libs:      cfg.Libraries,
		users:     cfg.Users,
		providers: cfg.Providers,
		config:    cfg.Config,
		restart:   cfg.Restart,
		logger:    cfg.Logger,
	}
}

// requireSetupActive short-circuits a request when the initial setup
// wizard has already been completed. Setup endpoints are intentionally
// unauthenticated so the very first user can reach them on a fresh
// install — but once setup is done, leaving them open turned the
// filesystem browser, library creation, and settings updates into an
// unauthenticated attack surface (filesystem disclosure via
// /setup/browse, library/path takeover via /setup/libraries, etc.).
//
// Returns true when the handler should continue, false when it has
// already written a 403 response.
func (h *SetupHandler) requireSetupActive(w http.ResponseWriter, r *http.Request) bool {
	if h.setup.NeedsSetup(r.Context()) {
		return true
	}
	respondError(w, r, http.StatusForbidden, "SETUP_COMPLETE", "setup wizard is no longer accepting requests")
	return false
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

// Browse lists directories at the requested path. GET (not POST) so it
// bypasses CSRF and the browser can short-cache the response — the
// admin folder-picker re-opens instantly without a full round-trip.
func (h *SetupHandler) Browse(w http.ResponseWriter, r *http.Request) {
	if !h.requireSetupActive(w, r) {
		return
	}
	reqPath := r.URL.Query().Get("path")
	if reqPath == "" {
		reqPath = "/"
	}

	result, err := h.setup.BrowseDirectories(reqPath)
	if err != nil {
		// Details (raw error, requested path) stay in logs only; the client
		// gets a stable code it can map to a UI-friendly message.
		h.logger.Warn("browse directories failed", "path", reqPath, "error", err)
		respondError(w, r, http.StatusBadRequest, "BROWSE_ERROR", "cannot browse this directory")
		return
	}

	w.Header().Set("Cache-Control", CacheControlListing)
	respondData(w, http.StatusOK, result)
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
	if !h.requireSetupActive(w, r) {
		return
	}
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

	respondData(w, http.StatusCreated, created)
}

type updateSettingsRequest struct {
	TMDbAPIKey         string `json:"tmdb_api_key"`
	TranscodingEnabled bool   `json:"transcoding_enabled"`
	HWAccel            string `json:"hw_accel"`
}

// UpdateSettings updates server settings during setup.
func (h *SetupHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	if !h.requireSetupActive(w, r) {
		return
	}
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
	if !h.requireSetupActive(w, r) {
		return
	}
	caps := h.setup.DetectCapabilities()
	respondData(w, http.StatusOK, caps)
}

type completeRequest struct {
	StartScan bool `json:"start_scan"`
}

// DatabaseProfiles surfaces the same one-click DB profiles the admin
// panel uses (today: the docker-compose-bundled Postgres) so the
// wizard step 0 can hide the raw DSN field behind a toggle. Read-only,
// no auth — same gate as the rest of /setup/* (NeedsSetup).
func (h *SetupHandler) DatabaseProfiles(w http.ResponseWriter, r *http.Request) {
	if !h.requireSetupActive(w, r) {
		return
	}
	respondData(w, http.StatusOK, detectDBProfiles())
}

// TestDatabase probes a candidate database driver+DSN/path so the
// wizard's first step can show "✓ reachable" before the operator
// commits to that backend. Shares its core with the admin panel's
// equivalent endpoint (testCandidateDB lives in admin_db.go) so the
// two surfaces never drift on validation rules.
//
// Unauthenticated like every other /setup/* endpoint; gated on
// NeedsSetup so a finished install can't be probed by anonymous
// callers.
func (h *SetupHandler) TestDatabase(w http.ResponseWriter, r *http.Request) {
	if !h.requireSetupActive(w, r) {
		return
	}
	var req dbTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	resp := testCandidateDB(r.Context(), req, h.logger)
	respondData(w, http.StatusOK, resp)
}

// SaveDatabase persists the wizard's database selection to
// hubplay.yaml and optionally triggers a restart so the next boot
// uses the new backend. The wizard calls this on its "Database" step
// after /test passes — at this point the only DB the binary has open
// is the default SQLite at the conventional path; switching to
// Postgres for a fresh install means the operator never sees the
// SQLite file populated with their wizard state.
func (h *SetupHandler) SaveDatabase(w http.ResponseWriter, r *http.Request) {
	if !h.requireSetupActive(w, r) {
		return
	}
	if h.dbSaver == nil {
		respondError(w, r, http.StatusServiceUnavailable, "NOT_AVAILABLE", "database saver not wired")
		return
	}
	var req dbSaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	if req.Driver != db.DriverSQLite && req.Driver != db.DriverPostgres {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "driver must be 'sqlite' or 'postgres'")
		return
	}
	if err := h.dbSaver.SaveDatabaseConfig(req.Driver, req.Path, req.DSN); err != nil {
		h.logger.Error("setup: save database config", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to persist database config")
		return
	}
	resp := map[string]any{"status": "saved", "restart_scheduled": false}
	if req.Restart && h.restart != nil {
		if h.restart.Request("setup wizard saved database config") {
			resp["restart_scheduled"] = true
		}
	}
	respondData(w, http.StatusOK, resp)
}

// Complete marks the setup wizard as finished.
func (h *SetupHandler) Complete(w http.ResponseWriter, r *http.Request) {
	if !h.requireSetupActive(w, r) {
		return
	}
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
