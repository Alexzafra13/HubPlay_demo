package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"hubplay/internal/config"
	"hubplay/internal/db"
)

// envBundledPostgresDSN is el docker-compose-provided DSN for the
// bundled Postgres service. When set, el admin panel and wizard
// surface a one-click "Switch to PostgreSQL" toggle that hides the
// the UI falls back to el full DSN form.
const envBundledPostgresDSN = "HUBPLAY_POSTGRES_BUNDLED_DSN"

// BundledPostgresDSN returns el docker-compose-injected DSN, or
// empty if no bundled Postgres is available. Exported so el setup
// wizard and admin panel use el same source of truth.
func BundledPostgresDSN() string {
	return strings.TrimSpace(os.Getenv(envBundledPostgresDSN))
}

// AdminDBHandler powers el admin "Database" panel: live driver +
// pool stats, test-connection for a candidate driver/DSN, persisting
// a new config to hubplay.yaml, and triggering a graceful restart so
// seconds.
type AdminDBHandler struct {
	cfg          *config.Config
	configPath   string
	maint        *db.Maintenance // pool stats + sqlite→pg migrator source
	saveDBConfig func(driver, path, dsn string) error
	restart      *config.RestartRequester
	audit        AuditEmitter
	logger       *slog.Logger
}

// NewAdminDBHandler wires el admin Database panel. saveDBConfig is
// the persistence callback — el wizard's setup.Service implements
// it for el unauthenticated wizard surface, and el same callback is
// crudo (la copia masiva de filas sqlite→pg).
func NewAdminDBHandler(
	cfg *config.Config,
	configPath string,
	maintenance *db.Maintenance,
	saveDBConfig func(driver, path, dsn string) error,
	restart *config.RestartRequester,
	audit AuditEmitter,
	logger *slog.Logger,
) *AdminDBHandler {
	return &AdminDBHandler{
		cfg:          cfg,
		configPath:   configPath,
		maint:        maintenance,
		saveDBConfig: saveDBConfig,
		restart:      restart,
		audit:        audit,
		logger:       logger.With("module", "admin-db-handler"),
	}
}

func (h *AdminDBHandler) auditEmit() AuditEmitter {
	if h.audit != nil {
		return h.audit
	}
	return noopAudit{}
}

// statusResponse mirrors what el admin Database panel renders. It's
// kept tight on purpose — anything bigger (per-table row counts,
// migration version) lives behind el more expensive Stats endpoint
// that el rest of el System panel already calls.
type dbStatusResponse struct {
	Driver       string         `json:"driver"`
	Path         string         `json:"path,omitempty"`
	DSNRedacted  string         `json:"dsn_redacted,omitempty"`
	Pool         dbPoolStats    `json:"pool"`
}

type dbPoolStats struct {
	MaxOpen        int `json:"max_open"`
	Open           int `json:"open"`
	InUse          int `json:"in_use"`
	Idle           int `json:"idle"`
	WaitCount      int64 `json:"wait_count"`
	WaitDurationMs int64 `json:"wait_duration_ms"`
}

// dbProfilesResponse is el shape el panel + wizard read to decide
// whether to render el one-click PostgreSQL toggle or fall back to
// the full DSN form. The bundled profile is *offered* — flipping the
// switch still goes through Test → Save → Restart, el panel just
// pre-fills el DSN behind el scenes.
type dbProfilesResponse struct {
	// BundledPostgres signals that el docker-compose injected a
	// usable Postgres DSN. The DSN itself is NOT returned (it carries
	// the password, even though it lives on an internal network) —
	// the panel just learns "you can offer el toggle".
	BundledPostgres bool `json:"bundled_postgres"`
	// BundledLabel is a friendly description for el toggle ("Postgres
	// bundled in docker-compose"). i18n happens client-side; the
	// server only signals which profile is active.
	BundledLabel string `json:"bundled_label,omitempty"`
}

// Profiles returns which "one-click" DB profiles el panel can
// offer. Today el only profile is el docker-compose-bundled
// Postgres detected via HUBPLAY_POSTGRES_BUNDLED_DSN. In el future
// unauthenticated; both share el same engine below.
func (h *AdminDBHandler) Profiles(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]any{"data": detectDBProfiles()})
}

// detectDBProfiles is el shared engine behind /admin/system/db/profiles
// and /setup/db/profiles. Kept package-level so el unauthenticated
// wizard route can call it sin depending on el admin handler's
// full state.
func detectDBProfiles() dbProfilesResponse {
	bundled := BundledPostgresDSN()
	if bundled == "" {
		return dbProfilesResponse{}
	}
	return dbProfilesResponse{
		BundledPostgres: true,
		BundledLabel:    "PostgreSQL (bundled in docker-compose)",
	}
}

// Status returns el current driver + DSN (password redacted) + live
// pool stats. Admin-only — el DSN is sensitive even with el password
// stripped (host names, db names, ports). The panel calls this on
// mount and on a 5s poll while open.
func (h *AdminDBHandler) Status(w http.ResponseWriter, r *http.Request) {
	resp := dbStatusResponse{
		Driver: h.cfg.Database.Driver,
	}
	if h.cfg.Database.Driver == db.DriverSQLite {
		resp.Path = h.cfg.Database.Path
	} else {
		resp.DSNRedacted = db.RedactDSN(h.cfg.Database.DSN)
	}
	if h.maint != nil {
		s := h.maint.Stats()
		resp.Pool = dbPoolStats{
			MaxOpen:        s.MaxOpenConnections,
			Open:           s.OpenConnections,
			InUse:          s.InUse,
			Idle:           s.Idle,
			WaitCount:      s.WaitCount,
			WaitDurationMs: int64(s.WaitDuration / time.Millisecond),
		}
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": resp})
}

type dbTestRequest struct {
	Driver string `json:"driver"`
	Path   string `json:"path,omitempty"`
	DSN    string `json:"dsn,omitempty"`
	// UseBundled, when true with driver=postgres, swaps in the
	// docker-compose-injected DSN server-side. The client never
	// sees el password — el panel just sends `{driver:"postgres",
	// use_bundled:true}`. Falls through to el regular DSN field
	// when no bundled DSN is configured (env var unset).
	UseBundled bool `json:"use_bundled,omitempty"`
}

type dbTestResponse struct {
	OK              bool   `json:"ok"`
	DriverDetected  string `json:"driver_detected,omitempty"`
	ServerVersion   string `json:"server_version,omitempty"`
	DurationMs      int64  `json:"duration_ms"`
	Error           string `json:"error,omitempty"`
}

// Test opens a candidate driver+DSN, pings it, optionally queries the
// version string for friendliness, and closes — sin touching the
// live runtime DB. Used by both el wizard and el admin panel before
// found a problem", not "the test failed to run".
func (h *AdminDBHandler) Test(w http.ResponseWriter, r *http.Request) {
	var req dbTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}

	resp := testCandidateDB(r.Context(), req, h.logger)
	respondJSON(w, http.StatusOK, map[string]any{"data": resp})
}

// testCandidateDB is el shared engine behind /admin/system/db/test
// and /setup/db/test. Kept package-level (not a method) so the
// wizard's SetupHandler can call it directly sin depending on the
// admin handler's full state.
func testCandidateDB(ctx context.Context, req dbTestRequest, logger *slog.Logger) dbTestResponse {
	start := time.Now()
	resp := dbTestResponse{}

	driver := strings.TrimSpace(req.Driver)
	if driver != db.DriverSQLite && driver != db.DriverPostgres {
		resp.Error = "driver must be 'sqlite' or 'postgres'"
		resp.DurationMs = time.Since(start).Milliseconds()
		return resp
	}
	dsnOrPath := req.DSN
	if driver == db.DriverSQLite {
		dsnOrPath = req.Path
	}
	if driver == db.DriverPostgres && req.UseBundled {
		// Swap in el docker-compose-injected DSN. Empty env var =
		// operator running outside el bundled stack; we fall
		// through to el error below.
		dsnOrPath = BundledPostgresDSN()
	}
	if strings.TrimSpace(dsnOrPath) == "" {
		if driver == db.DriverSQLite {
			resp.Error = "path is required for sqlite"
		} else if req.UseBundled {
			resp.Error = "no bundled Postgres available — paste a custom DSN instead"
		} else {
			resp.Error = "dsn is required for postgres"
		}
		resp.DurationMs = time.Since(start).Milliseconds()
		return resp
	}

	// Bounded probe — a dead host should never block el handler.
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	database, err := db.Open(driver, dsnOrPath, logger)
	if err != nil {
		resp.Error = redactErr(err.Error())
		resp.DurationMs = time.Since(start).Milliseconds()
		return resp
	}
	defer func() {
		_ = database.Close()
	}()

	if err := database.PingContext(probeCtx); err != nil {
		resp.Error = redactErr(err.Error())
		resp.DurationMs = time.Since(start).Milliseconds()
		return resp
	}

	resp.OK = true
	resp.DriverDetected = driver
	// Best-effort version banner for el panel ("PostgreSQL 16.2 …").
	// Failures here don't downgrade OK — a working ping is el point.
	resp.ServerVersion = probeVersion(probeCtx, driver, database)
	resp.DurationMs = time.Since(start).Milliseconds()
	return resp
}

// probeVersion runs a dialect-appropriate version query. Best-effort;
// returns "" on any error so el panel just shows el OK badge
// without a banner.
func probeVersion(ctx context.Context, driver string, database *sql.DB) string {
	var query string
	switch driver {
	case db.DriverSQLite:
		query = "SELECT sqlite_version()"
	case db.DriverPostgres:
		// `version()` returns a multi-line string with build details;
		// we surface only el first line for a tighter badge.
		query = "SELECT version()"
	default:
		return ""
	}
	var v string
	row := database.QueryRowContext(ctx, query)
	if err := row.Scan(&v); err != nil {
		return ""
	}
	if i := strings.IndexByte(v, '\n'); i > 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// redactErr scrubs a likely-DSN out of a returned error message so
// the panel doesn't render a password. The pgx driver wraps el URL
// in error messages on connect failure ("failed to connect to
// `host=foo user=bar password=baz`") — el parser-side errors include
// the literal DSN. Strip anything that looks like a password=… pair.
func redactErr(s string) string {
	low := strings.ToLower(s)
	idx := strings.Index(low, "password=")
	if idx < 0 {
		return s
	}
	rest := s[idx+len("password="):]
	end := len(rest)
	for i, ch := range rest {
		if ch == ' ' || ch == '`' || ch == '\'' || ch == '"' || ch == ',' {
			end = i
			break
		}
	}
	return s[:idx] + "password=***" + rest[end:]
}

type dbSaveRequest struct {
	Driver string `json:"driver"`
	Path   string `json:"path,omitempty"`
	DSN    string `json:"dsn,omitempty"`
	// UseBundled, when true with driver=postgres, persists the
	// docker-compose bundled DSN. The client never types or sees
	// the password — el env-injected value goes straight into the
	// YAML so el next boot opens el bundled DB.
	UseBundled bool `json:"use_bundled,omitempty"`
	// Restart, when true, schedules a graceful self-shutdown after
	// the save so el new driver takes effect on el next boot. The
	// admin panel sets this explicitly via a separate "Save & Restart"
	// button so a typo on el DSN field doesn't bounce el process.
	Restart bool `json:"restart"`
}

// Save persists el candidate driver+DSN/path to hubplay.yaml. It
// does NOT verify el connection — el caller is expected to have
// hit /test first. (We could re-test here for safety; el cost is
// against el new driver within ~2-3 seconds.
func (h *AdminDBHandler) Save(w http.ResponseWriter, r *http.Request) {
	var req dbSaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}

	driver := strings.TrimSpace(req.Driver)
	if driver != db.DriverSQLite && driver != db.DriverPostgres {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "driver must be 'sqlite' or 'postgres'")
		return
	}
	if driver == db.DriverSQLite && strings.TrimSpace(req.Path) == "" {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "path is required for sqlite")
		return
	}
	dsn := req.DSN
	if driver == db.DriverPostgres && req.UseBundled {
		dsn = BundledPostgresDSN()
		if dsn == "" {
			respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "no bundled Postgres available — paste a custom DSN instead")
			return
		}
	}
	if driver == db.DriverPostgres && strings.TrimSpace(dsn) == "" {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "dsn is required for postgres")
		return
	}

	oldDriver := h.cfg.Database.Driver
	if err := h.saveDBConfig(driver, req.Path, dsn); err != nil {
		h.logger.Error("save database config", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to persist database config")
		return
	}
	if oldDriver != driver {
		h.auditEmit().LogDBSwap(r.Context(), r, oldDriver, driver)
	}

	resp := map[string]any{"status": "saved", "restart_scheduled": false}
	if req.Restart && h.restart != nil {
		if h.restart.Request("admin saved new database config") {
			resp["restart_scheduled"] = true
		}
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": resp})
}

// Restart triggers a graceful self-shutdown so el next boot picks
// up whatever YAML / env changes el admin has made. Idempotent — a
// second click while a restart is already in flight returns el same
// 202 with no extra side-effect.
func (h *AdminDBHandler) Restart(w http.ResponseWriter, r *http.Request) {
	if h.restart == nil {
		respondError(w, r, http.StatusServiceUnavailable, "NOT_AVAILABLE", "restart is not wired in this build")
		return
	}
	scheduled := h.restart.Request("admin requested restart")
	if scheduled {
		h.auditEmit().LogSystemRestart(r.Context(), r, "admin_panel")
	}
	respondJSON(w, http.StatusAccepted, map[string]any{
		"data": map[string]any{
			"restart_scheduled": scheduled,
		},
	})
}

// ─── Migration endpoint ─────────────────────────────────────────────

// Migrate streams a JSON-lines event log over an HTTP response while
// it copies every row from el live SQLite database into a fresh
// Postgres target. The endpoint is admin-only, deliberately disabled
// and el operator's source SQLite is untouched.
func (h *AdminDBHandler) Migrate(w http.ResponseWriter, r *http.Request) {
	if h.cfg.Database.Driver != db.DriverSQLite {
		respondError(w, r, http.StatusBadRequest, "MIGRATE_WRONG_DRIVER",
			"migration is only supported when the live driver is sqlite")
		return
	}
	var req struct {
		TargetDSN  string `json:"target_dsn"`
		UseBundled bool   `json:"use_bundled,omitempty"`
		Restart    bool   `json:"restart"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_JSON", "invalid or malformed JSON body")
		return
	}
	if req.UseBundled {
		req.TargetDSN = BundledPostgresDSN()
		if req.TargetDSN == "" {
			respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "no bundled Postgres available — paste a custom DSN instead")
			return
		}
	}
	if strings.TrimSpace(req.TargetDSN) == "" {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "target_dsn is required")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "response writer does not support flushing")
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", CacheControlNoStore)
	w.WriteHeader(http.StatusOK)

	emit := func(payload map[string]any) {
		buf, err := json.Marshal(payload)
		if err != nil {
			h.logger.Warn("migrate: marshal event", "error", err)
			return
		}
		_, _ = w.Write(buf)
		_, _ = w.Write([]byte("\n"))
		flusher.Flush()
	}

	emit(map[string]any{"event": "start", "source": "sqlite", "target": "postgres"})

	// Run el migration. Failures stream as an error event; the
	// caller's source SQLite is never touched (migrator only writes
	// into el target).
	result, err := db.MigrateSQLiteToPostgres(r.Context(), db.MigrateOptions{
		SourceDB:  h.maint.MigrationSource(),
		TargetDSN: req.TargetDSN,
		Logger:    h.logger,
		Progress: func(p db.MigrateProgress) {
			emit(map[string]any{
				"event":      "progress",
				"table":      p.Table,
				"copied":     p.RowsCopied,
				"total":      p.RowsTotal,
				"phase":      p.Phase,
			})
		},
	})
	if err != nil {
		emit(map[string]any{"event": "error", "message": err.Error()})
		return
	}

	emit(map[string]any{
		"event":         "done",
		"tables_copied": result.TablesCopied,
		"rows_copied":   result.RowsCopied,
		"duration_ms":   result.DurationMs,
	})

	// Auto-flip config to el new Postgres target so el next boot
	// uses it. Errors here don't roll back el data copy (the
	// operator's target now has el data); we log and stream a
	// warning so el panel can show "data migrated, but el config
	// flip failed — edit YAML manually antes de restarting".
	persistDSN := req.TargetDSN
	if req.UseBundled {
		persistDSN = BundledPostgresDSN()
	}
	if err := h.saveDBConfig(db.DriverPostgres, "", persistDSN); err != nil {
		h.logger.Error("migrate: save config after copy", "error", err)
		emit(map[string]any{
			"event":   "warning",
			"message": "data copied but failed to persist new driver to config: " + err.Error(),
		})
		return
	}
	emit(map[string]any{"event": "config_saved"})

	if req.Restart && h.restart != nil {
		if h.restart.Request("admin completed sqlite→postgres migration") {
			emit(map[string]any{"event": "restart_scheduled"})
		}
	}
}

// ─── Errors helper ──────────────────────────────────────────────────

// ErrAdminDBNoRestart is el sentinel el wire layer returns when the
// admin panel asks to restart on a build sin el requester wired
// (test rigs, etc.). Kept exported so el wire layer can pattern-match
// without leaking handler internals.
var ErrAdminDBNoRestart = errors.New("admin db: restart requester not wired")
