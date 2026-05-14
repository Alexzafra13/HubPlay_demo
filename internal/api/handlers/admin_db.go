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

// envBundledPostgresDSN is the docker-compose-provided DSN for the
// bundled Postgres service. When set, the admin panel and wizard
// surface a one-click "Switch to PostgreSQL" toggle that hides the
// DSN field behind an "Advanced — custom DSN" section. Empty when
// the operator runs the binary outside docker-compose, in which case
// the UI falls back to the full DSN form.
const envBundledPostgresDSN = "HUBPLAY_POSTGRES_BUNDLED_DSN"

// BundledPostgresDSN returns the docker-compose-injected DSN, or
// empty if no bundled Postgres is available. Exported so the setup
// wizard and admin panel use the same source of truth.
func BundledPostgresDSN() string {
	return strings.TrimSpace(os.Getenv(envBundledPostgresDSN))
}

// AdminDBHandler powers the admin "Database" panel: live driver +
// pool stats, test-connection for a candidate driver/DSN, persisting
// a new config to hubplay.yaml, and triggering a graceful restart so
// the new driver takes effect.
//
// The handler intentionally never swaps the live `*sql.DB`. A driver
// change is a process-level restart concern: the repos cable to the
// concrete connection at boot, migrations run against a specific
// dialect, and FTS indexes / boot-time stats are per-driver. Hot-swap
// would multiply the surface area we'd have to test for marginal UX
// gain — the container restart loop is fast enough that the panel can
// say "saved, restarting…" and the operator sees the new driver in
// seconds.
type AdminDBHandler struct {
	cfg          *config.Config
	configPath   string
	maint        *db.Maintenance // pool stats + sqlite→pg migrator source
	saveDBConfig func(driver, path, dsn string) error
	restart      *config.RestartRequester
	logger       *slog.Logger
}

// NewAdminDBHandler wires the admin Database panel. saveDBConfig is
// the persistence callback — the wizard's setup.Service implements
// it for the unauthenticated wizard surface, and the same callback is
// reused here so both surfaces write through one code path.
//
// Recibe *db.Maintenance en lugar del `*sql.DB` raw: stats vía
// PoolStatsReporter, conexión origen para el migrator vía
// MigrationSource(). Cierra el olor K (admin handlers no reciben
// `*sql.DB` directo) preservando el único uso legítimo del handle
// crudo (la copia masiva de filas sqlite→pg).
func NewAdminDBHandler(
	cfg *config.Config,
	configPath string,
	maintenance *db.Maintenance,
	saveDBConfig func(driver, path, dsn string) error,
	restart *config.RestartRequester,
	logger *slog.Logger,
) *AdminDBHandler {
	return &AdminDBHandler{
		cfg:          cfg,
		configPath:   configPath,
		maint:        maintenance,
		saveDBConfig: saveDBConfig,
		restart:      restart,
		logger:       logger.With("module", "admin-db-handler"),
	}
}

// statusResponse mirrors what the admin Database panel renders. It's
// kept tight on purpose — anything bigger (per-table row counts,
// migration version) lives behind the more expensive Stats endpoint
// that the rest of the System panel already calls.
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

// dbProfilesResponse is the shape the panel + wizard read to decide
// whether to render the one-click PostgreSQL toggle or fall back to
// the full DSN form. The bundled profile is *offered* — flipping the
// switch still goes through Test → Save → Restart, the panel just
// pre-fills the DSN behind the scenes.
type dbProfilesResponse struct {
	// BundledPostgres signals that the docker-compose injected a
	// usable Postgres DSN. The DSN itself is NOT returned (it carries
	// the password, even though it lives on an internal network) —
	// the panel just learns "you can offer the toggle".
	BundledPostgres bool `json:"bundled_postgres"`
	// BundledLabel is a friendly description for the toggle ("Postgres
	// bundled in docker-compose"). i18n happens client-side; the
	// server only signals which profile is active.
	BundledLabel string `json:"bundled_label,omitempty"`
}

// Profiles returns which "one-click" DB profiles the panel can
// offer. Today the only profile is the docker-compose-bundled
// Postgres detected via HUBPLAY_POSTGRES_BUNDLED_DSN. In the future
// this is where managed-provider presets (Supabase, RDS) could
// land if the project ever wants opinionated integrations.
//
// Auth-required by the routing layer (same prefix as the rest of
// /admin/system/*). The corresponding /setup/db/profiles is
// unauthenticated; both share the same engine below.
func (h *AdminDBHandler) Profiles(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]any{"data": detectDBProfiles()})
}

// detectDBProfiles is the shared engine behind /admin/system/db/profiles
// and /setup/db/profiles. Kept package-level so the unauthenticated
// wizard route can call it without depending on the admin handler's
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

// Status returns the current driver + DSN (password redacted) + live
// pool stats. Admin-only — the DSN is sensitive even with the password
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
	// sees the password — the panel just sends `{driver:"postgres",
	// use_bundled:true}`. Falls through to the regular DSN field
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
// version string for friendliness, and closes — without touching the
// live runtime DB. Used by both the wizard and the admin panel before
// they offer a "Save" button so the operator gets immediate feedback
// on typos / missing sslmode params / firewalled hosts.
//
// Errors come back as 200 with `ok: false` (rather than 4xx/5xx) so
// the UI can render the message inline without a generic "request
// failed" toast — the operator's mental model is "the test ran and
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

// testCandidateDB is the shared engine behind /admin/system/db/test
// and /setup/db/test. Kept package-level (not a method) so the
// wizard's SetupHandler can call it directly without depending on the
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
		// Swap in the docker-compose-injected DSN. Empty env var =
		// operator running outside the bundled stack; we fall
		// through to the error below.
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

	// Bounded probe — a dead host should never block the handler.
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
	// Best-effort version banner for the panel ("PostgreSQL 16.2 …").
	// Failures here don't downgrade OK — a working ping is the point.
	resp.ServerVersion = probeVersion(probeCtx, driver, database)
	resp.DurationMs = time.Since(start).Milliseconds()
	return resp
}

// probeVersion runs a dialect-appropriate version query. Best-effort;
// returns "" on any error so the panel just shows the OK badge
// without a banner.
func probeVersion(ctx context.Context, driver string, database *sql.DB) string {
	var query string
	switch driver {
	case db.DriverSQLite:
		query = "SELECT sqlite_version()"
	case db.DriverPostgres:
		// `version()` returns a multi-line string with build details;
		// we surface only the first line for a tighter badge.
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
// the panel doesn't render a password. The pgx driver wraps the URL
// in error messages on connect failure ("failed to connect to
// `host=foo user=bar password=baz`") — the parser-side errors include
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
	// the password — the env-injected value goes straight into the
	// YAML so the next boot opens the bundled DB.
	UseBundled bool `json:"use_bundled,omitempty"`
	// Restart, when true, schedules a graceful self-shutdown after
	// the save so the new driver takes effect on the next boot. The
	// admin panel sets this explicitly via a separate "Save & Restart"
	// button so a typo on the DSN field doesn't bounce the process.
	Restart bool `json:"restart"`
}

// Save persists the candidate driver+DSN/path to hubplay.yaml. It
// does NOT verify the connection — the caller is expected to have
// hit /test first. (We could re-test here for safety; the cost is
// adding a second 10s probe on a path the admin already vetted, so
// the UX team's preference is "trust the explicit Save click".)
//
// When req.Restart is true the handler triggers a graceful shutdown
// after the response flushes. Under docker-compose's `restart:
// unless-stopped` (the project default), the container is back up
// against the new driver within ~2-3 seconds.
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

	if err := h.saveDBConfig(driver, req.Path, dsn); err != nil {
		h.logger.Error("save database config", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to persist database config")
		return
	}

	resp := map[string]any{"status": "saved", "restart_scheduled": false}
	if req.Restart && h.restart != nil {
		if h.restart.Request("admin saved new database config") {
			resp["restart_scheduled"] = true
		}
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": resp})
}

// Restart triggers a graceful self-shutdown so the next boot picks
// up whatever YAML / env changes the admin has made. Idempotent — a
// second click while a restart is already in flight returns the same
// 202 with no extra side-effect.
func (h *AdminDBHandler) Restart(w http.ResponseWriter, r *http.Request) {
	if h.restart == nil {
		respondError(w, r, http.StatusServiceUnavailable, "NOT_AVAILABLE", "restart is not wired in this build")
		return
	}
	scheduled := h.restart.Request("admin requested restart")
	respondJSON(w, http.StatusAccepted, map[string]any{
		"data": map[string]any{
			"restart_scheduled": scheduled,
		},
	})
}

// ─── Migration endpoint ─────────────────────────────────────────────

// Migrate streams a JSON-lines event log over an HTTP response while
// it copies every row from the live SQLite database into a fresh
// Postgres target. The endpoint is admin-only, deliberately disabled
// when the live driver is already Postgres (target == source has no
// meaning), and writes against a target schema the operator must
// have prepared in advance — see the runbook at docs/operations/postgres.md.
//
// The response is text/event-stream so the panel can render live
// progress (per-table row counts) without a separate WebSocket. On
// any failure the stream emits a final {"event":"error", ...} record
// and the operator's source SQLite is untouched.
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
	w.Header().Set("Cache-Control", "no-store")
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

	// Run the migration. Failures stream as an error event; the
	// caller's source SQLite is never touched (migrator only writes
	// into the target).
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

	// Auto-flip config to the new Postgres target so the next boot
	// uses it. Errors here don't roll back the data copy (the
	// operator's target now has the data); we log and stream a
	// warning so the panel can show "data migrated, but the config
	// flip failed — edit YAML manually before restarting".
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

// ErrAdminDBNoRestart is the sentinel the wire layer returns when the
// admin panel asks to restart on a build without the requester wired
// (test rigs, etc.). Kept exported so the wire layer can pattern-match
// without leaking handler internals.
var ErrAdminDBNoRestart = errors.New("admin db: restart requester not wired")
