package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"hubplay/internal/auth"
	"hubplay/internal/db"

	"github.com/go-chi/chi/v5"
)

// IPTVScheduleRepository is the subset of db.IPTVScheduleRepository the
// schedule handler needs. Kept narrow so handler tests can supply a
// fake without wiring a real *sql.DB.
type IPTVScheduleRepository interface {
	ListByLibrary(ctx context.Context, libraryID string) ([]*db.IPTVScheduledJob, error)
	Get(ctx context.Context, libraryID, kind string) (*db.IPTVScheduledJob, error)
	Upsert(ctx context.Context, job *db.IPTVScheduledJob) error
	Delete(ctx context.Context, libraryID, kind string) error
}

// IPTVScheduleRunner is the subset of iptv.Scheduler the handler needs
// for the "Run now" button. The worker owns the actual refresh path;
// the handler just forwards.
type IPTVScheduleRunner interface {
	RunNow(ctx context.Context, libraryID, kind string) error
}

// IPTVScheduleHandler owns the admin endpoints for scheduled IPTV
// jobs (M3U refresh + EPG refresh). ACL + admin gating are shared
// with IPTVHandler via the same LibraryAccessService / RequireAdmin
// middleware; this handler only deals with the schedule CRUD surface.
type IPTVScheduleHandler struct {
	repo    IPTVScheduleRepository
	runner  IPTVScheduleRunner
	access  LibraryAccessService
	logger  *slog.Logger
}

// NewIPTVScheduleHandler wires a schedule handler. access is shared
// with the main IPTV handler so the ACL check behaves identically.
func NewIPTVScheduleHandler(
	repo IPTVScheduleRepository,
	runner IPTVScheduleRunner,
	access LibraryAccessService,
	logger *slog.Logger,
) *IPTVScheduleHandler {
	return &IPTVScheduleHandler{
		repo:   repo,
		runner: runner,
		access: access,
		logger: logger.With("module", "iptv-schedule-handler"),
	}
}

// scheduledJobDTO is the JSON shape the frontend consumes. Fields
// follow the snake_case convention the rest of the IPTV API uses.
type scheduledJobDTO struct {
	LibraryID      string `json:"library_id"`
	Kind           string `json:"kind"`
	IntervalHours  int    `json:"interval_hours"`
	Enabled        bool   `json:"enabled"`
	LastRunAt      string `json:"last_run_at,omitempty"`
	LastStatus     string `json:"last_status"`
	LastError      string `json:"last_error,omitempty"`
	LastDurationMS int    `json:"last_duration_ms"`
}

func jobToDTO(j *db.IPTVScheduledJob) scheduledJobDTO {
	dto := scheduledJobDTO{
		LibraryID:      j.LibraryID,
		Kind:           j.Kind,
		IntervalHours:  j.IntervalHours,
		Enabled:        j.Enabled,
		LastStatus:     j.LastStatus,
		LastError:      j.LastError,
		LastDurationMS: j.LastDurationMS,
	}
	if !j.LastRunAt.IsZero() {
		dto.LastRunAt = j.LastRunAt.UTC().Format(time.RFC3339)
	}
	return dto
}

// ListSchedule returns both job kinds for a library. Synthesises
// "never configured" placeholders for missing kinds so the UI can
// always render both rows without a separate zero-state.
func (h *IPTVScheduleHandler) List(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")
	if !h.canAccess(r, libraryID) {
		respondError(w, r, http.StatusNotFound, "NOT_FOUND", "library not found")
		return
	}
	rows, err := h.repo.ListByLibrary(r.Context(), libraryID)
	if err != nil {
		h.logger.Error("list iptv schedule", "library", libraryID, "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL", "")
		return
	}
	byKind := make(map[string]*db.IPTVScheduledJob, 2)
	for _, r := range rows {
		byKind[r.Kind] = r
	}
	out := make([]scheduledJobDTO, 0, 2)
	for _, kind := range []string{db.IPTVJobKindM3URefresh, db.IPTVJobKindEPGRefresh} {
		if row, ok := byKind[kind]; ok {
			out = append(out, jobToDTO(row))
			continue
		}
		// Placeholder: disabled, default interval. Not persisted.
		out = append(out, scheduledJobDTO{
			LibraryID:     libraryID,
			Kind:          kind,
			IntervalHours: defaultIntervalFor(kind),
			Enabled:       false,
		})
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out})
}

// upsertRequest is the body accepted by PUT /schedule/{kind}.
type upsertScheduleRequest struct {
	IntervalHours int   `json:"interval_hours"`
	Enabled       *bool `json:"enabled,omitempty"`
}

// Upsert creates or updates one schedule row. Body specifies the
// interval (1..720 h) and enabled flag. Missing `enabled` keeps the
// current value so the UI can save just the interval without
// accidentally toggling.
func (h *IPTVScheduleHandler) Upsert(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")
	kind := chi.URLParam(r, "kind")
	if !h.canAccess(r, libraryID) {
		respondError(w, r, http.StatusNotFound, "NOT_FOUND", "library not found")
		return
	}
	if !isValidJobKind(kind) {
		respondError(w, r, http.StatusBadRequest, "INVALID_KIND", "unknown job kind")
		return
	}
	var body upsertScheduleRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid JSON body")
		return
	}
	if body.IntervalHours < 1 || body.IntervalHours > 720 {
		respondError(w, r, http.StatusBadRequest, "INVALID_INTERVAL",
			"interval_hours must be between 1 and 720")
		return
	}

	existing, err := h.repo.Get(r.Context(), libraryID, kind)
	if err != nil && !errors.Is(err, db.ErrIPTVScheduledJobNotFound) {
		h.logger.Error("load existing iptv schedule",
			"library", libraryID, "kind", kind, "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL", "")
		return
	}

	enabled := false
	if existing != nil {
		enabled = existing.Enabled
	}
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	job := &db.IPTVScheduledJob{
		LibraryID:     libraryID,
		Kind:          kind,
		IntervalHours: body.IntervalHours,
		Enabled:       enabled,
	}
	if err := h.repo.Upsert(r.Context(), job); err != nil {
		h.logger.Error("upsert iptv schedule",
			"library", libraryID, "kind", kind, "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL", "")
		return
	}
	// Return the freshly-persisted row so the UI has the canonical
	// last_* fields (which Upsert preserves from the existing row).
	saved, err := h.repo.Get(r.Context(), libraryID, kind)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, "INTERNAL", "")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": jobToDTO(saved)})
}

// Delete removes a schedule row. Equivalent to "stop scheduling";
// the admin keeps the manual Refrescar button in the existing panels.
func (h *IPTVScheduleHandler) Delete(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")
	kind := chi.URLParam(r, "kind")
	if !h.canAccess(r, libraryID) {
		respondError(w, r, http.StatusNotFound, "NOT_FOUND", "library not found")
		return
	}
	if !isValidJobKind(kind) {
		respondError(w, r, http.StatusBadRequest, "INVALID_KIND", "unknown job kind")
		return
	}
	if err := h.repo.Delete(r.Context(), libraryID, kind); err != nil {
		h.logger.Error("delete iptv schedule",
			"library", libraryID, "kind", kind, "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL", "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RunNow fires a single refresh synchronously through the scheduler so
// the outcome is recorded against the same row the periodic worker
// would update. Blocks the HTTP response until the refresh finishes
// (or the runner's internal timeout fires) — the admin expects
// immediate feedback.
func (h *IPTVScheduleHandler) RunNow(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")
	kind := chi.URLParam(r, "kind")
	if !h.canAccess(r, libraryID) {
		respondError(w, r, http.StatusNotFound, "NOT_FOUND", "library not found")
		return
	}
	if !isValidJobKind(kind) {
		respondError(w, r, http.StatusBadRequest, "INVALID_KIND", "unknown job kind")
		return
	}
	if err := h.runner.RunNow(r.Context(), libraryID, kind); err != nil {
		h.logger.Warn("iptv schedule run-now failed",
			"library", libraryID, "kind", kind, "error", err)
		respondError(w, r, http.StatusBadGateway, "REFRESH_FAILED", err.Error())
		return
	}
	// Return the updated row so the UI can refresh timestamps
	// without a second round-trip.
	saved, err := h.repo.Get(r.Context(), libraryID, kind)
	if err != nil && !errors.Is(err, db.ErrIPTVScheduledJobNotFound) {
		respondError(w, r, http.StatusInternalServerError, "INTERNAL", "")
		return
	}
	if saved == nil {
		// RunNow was invoked against a library with no schedule row;
		// the refresh still ran and was logged, but there's no
		// record to return — answer 204 so the UI can refetch the
		// list (the list endpoint synthesises placeholders).
		w.WriteHeader(http.StatusNoContent)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": jobToDTO(saved)})
}

func (h *IPTVScheduleHandler) canAccess(r *http.Request, libraryID string) bool {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		return false
	}
	if claims.Role == "admin" {
		return true
	}
	ok, err := h.access.UserHasAccess(r.Context(), claims.UserID, libraryID)
	if err != nil {
		h.logger.Error("library access check failed",
			"user", claims.UserID, "library", libraryID, "error", err)
		return false
	}
	return ok
}

func isValidJobKind(kind string) bool {
	return kind == db.IPTVJobKindM3URefresh || kind == db.IPTVJobKindEPGRefresh
}

// defaultIntervalFor picks a sensible out-of-the-box cadence for each
// job kind. M3U sources rarely change so 24 h is fine; EPG XML feeds
// from davidmuma / epg.pw refresh nightly, so 6 h covers mid-day slot
// shifts without hammering the CDN.
func defaultIntervalFor(kind string) int {
	switch kind {
	case db.IPTVJobKindEPGRefresh:
		return 6
	case db.IPTVJobKindM3URefresh:
		return 24
	default:
		return 24
	}
}
