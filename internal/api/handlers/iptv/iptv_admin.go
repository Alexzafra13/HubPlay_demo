package iptvhandler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"hubplay/internal/api/handlers"
	"hubplay/internal/iptv"
	librarymodel "hubplay/internal/library/model"
)

// iptvAdminOps es el contrato mínimo para los endpoints admin IPTV
// (refresh M3U/EPG, preflight, import público). 7 de ~50 métodos.
type iptvAdminOps interface {
	PreflightCheck(ctx context.Context, m3uURL string, tlsInsecure bool) iptv.PreflightResult
	TryAcquireRefresh(libraryID string) (func(), error)
	RunRefreshM3U(ctx context.Context, libraryID string) (int, error)
	PublishRefreshFailed(libraryID string, err error)
	SpawnBackground(fn func(ctx context.Context))
	RefreshEPG(ctx context.Context, libraryID string) (int, error)
	RefreshM3U(ctx context.Context, libraryID string) (int, error)
}

type iptvAdminHandler struct {
	svc       iptvAdminOps
	libraries handlers.LibraryRepository
	access    handlers.LibraryAccessService
	audit     handlers.AuditEmitter
	logger    *slog.Logger
}

func (h *iptvAdminHandler) auditEmit() handlers.AuditEmitter {
	if h.audit != nil {
		return h.audit
	}
	return handlers.NoopAudit{}
}

// PreflightM3U probes an M3U URL on the operator's behalf so the
// admin UI can show "this is fine" / "provider is hung" / "got HTML
// instead of a playlist" before they commit a save. Bounded to ~12s
// — see iptv.PreflightCheck for the verdict taxonomy.
//
// Admin-only because the request body comes verbatim from the
// caller and a public endpoint would let any unauthenticated user
// turn the server into a generic HTTP probe (SSRF-adjacent).
func (h *iptvAdminHandler) PreflightM3U(w http.ResponseWriter, r *http.Request) {
	var req struct {
		M3UURL      string `json:"m3u_url"`
		TLSInsecure bool   `json:"tls_insecure"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid JSON body")
		return
	}
	if req.M3UURL == "" {
		handlers.RespondError(w, r, http.StatusBadRequest, "MISSING_URL", "m3u_url is required")
		return
	}

	result := h.svc.PreflightCheck(r.Context(), req.M3UURL, req.TLSInsecure)
	handlers.RespondJSON(w, http.StatusOK, result)
}

// refreshM3UAsyncTimeout caps the detached import context. Picked
// against the real-world ceiling we've seen with ~98k-line Xtream
// M3U_PLUS feeds (parse + filter + DB transaction), with margin for
// degraded provider links. Long enough that a slow upstream finishes;
// short enough that a hung fetch eventually frees the per-library
// slot instead of blocking refreshes forever.
const refreshM3UAsyncTimeout = 10 * time.Minute

// RefreshM3U triggers an M3U playlist refresh for a library.
//
// Returns 202 Accepted: the actual import runs in a detached goroutine
// because large M3U_PLUS feeds (Xtream-Codes, ~98k lines) routinely
// exceed the nginx proxy_read_timeout (default 60s) and the request
// context cancellation tears down the DB transaction mid-write,
// dropping every parsed channel. Detaching the import lifts that
// limit and survives client disconnect; completion is signalled
// through SSE (`playlist.refreshed` / `playlist.refresh_failed`).
//
// Already admin-only at the route level, but we also verify library access
// defence-in-depth: admins can see every library regardless of the ACL, so
// this check is effectively a documentation anchor today. It becomes
// load-bearing the day a non-admin role gains access to refresh endpoints.
func (h *iptvAdminHandler) RefreshM3U(w http.ResponseWriter, r *http.Request) {
	libraryID := handlers.RequireParam(w, r, "id")
	if libraryID == "" {
		return
	}
	if !canAccessLibrary(r, h.access, h.logger, libraryID) {
		iptvDenyForbidden(w, r)
		return
	}

	// Acquire the per-library refresh slot synchronously so a
	// concurrent click gets an immediate 409 instead of two
	// goroutines racing into the same lock.
	release, err := h.svc.TryAcquireRefresh(libraryID)
	if err != nil {
		// ErrRefreshInProgress is a benign race (admin double-clicked,
		// scheduler tick raced a manual refresh, frontend reconnected
		// mid-import): respond 409 with structured body so the client
		// can join the in-flight import via SSE instead of treating it
		// as a hard failure. Anything else is unexpected and routes
		// through the generic 500 handler.
		if errors.Is(err, iptv.ErrRefreshInProgress) {
			h.logger.Info("M3U refresh skipped: already in progress", "library", libraryID)
			handlers.RespondJSON(w, http.StatusConflict, map[string]any{
				"data": map[string]any{
					"library_id": libraryID,
					"status":     "in_progress",
				},
			})
			return
		}
		handlers.HandleServiceError(w, r, err)
		return
	}

	// SpawnBackground (no `go func` + context.Background) para que
	// shutdown drene un refresh en vuelo en vez de matarlo a mitad
	// del write (audit olor GGGG).
	h.svc.SpawnBackground(func(bgCtx context.Context) {
		defer release()
		ctx, cancel := context.WithTimeout(bgCtx, refreshM3UAsyncTimeout)
		defer cancel()
		count, err := h.svc.RunRefreshM3U(ctx, libraryID)
		if err != nil {
			h.logger.Error("M3U refresh failed", "library", libraryID, "error", err)
			h.svc.PublishRefreshFailed(libraryID, err)
			return
		}
		h.logger.Info("M3U refresh complete (async)", "library", libraryID, "channels", count)
	})

	handlers.RespondJSON(w, http.StatusAccepted, map[string]any{
		"data": map[string]any{
			"library_id": libraryID,
			"status":     "started",
		},
	})
}

// RefreshEPG triggers an EPG refresh for a library.
func (h *iptvAdminHandler) RefreshEPG(w http.ResponseWriter, r *http.Request) {
	libraryID := handlers.RequireParam(w, r, "id")
	if libraryID == "" {
		return
	}
	if !canAccessLibrary(r, h.access, h.logger, libraryID) {
		iptvDenyForbidden(w, r)
		return
	}

	count, err := h.svc.RefreshEPG(r.Context(), libraryID)
	if err != nil {
		h.logger.Error("EPG refresh failed", "library", libraryID, "error", err)
		handlers.HandleServiceError(w, r, err)
		return
	}

	handlers.RespondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"programs_imported": count,
		},
	})
}

// PublicCountries returns the list of countries with available public IPTV channels.
func (h *iptvAdminHandler) PublicCountries(w http.ResponseWriter, r *http.Request) {
	countries := iptv.PublicCountries()

	result := make([]map[string]any, 0, len(countries))
	for _, c := range countries {
		result = append(result, map[string]any{
			"code": c.Code,
			"name": c.Name,
			"flag": c.Flag,
		})
	}

	handlers.RespondData(w, http.StatusOK, result)
}

// ImportPublicIPTV creates a livetv library for a country and triggers M3U import.
func (h *iptvAdminHandler) ImportPublicIPTV(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Country string `json:"country"`
		Name    string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}

	country, ok := iptv.FindCountry(req.Country)
	if !ok {
		handlers.RespondError(w, r, http.StatusBadRequest, "INVALID_COUNTRY", "unknown country code")
		return
	}

	libraryName := req.Name
	if libraryName == "" {
		libraryName = fmt.Sprintf("Live TV - %s", country.Name)
	}

	now := time.Now()
	lib := &librarymodel.Library{
		ID:          generateLibraryID(),
		Name:        libraryName,
		ContentType: "livetv",
		M3UURL:      country.M3UURL(),
		ScanMode:    "auto",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := h.libraries.Create(r.Context(), lib); err != nil {
		h.logger.Error("create public IPTV library", "error", err)
		handlers.RespondError(w, r, http.StatusInternalServerError, "CREATE_ERROR", "failed to create library")
		return
	}
	// Audit de la creación inmediata. El evento M3U se emite cuando
	// el refresh background completa con su count real.
	h.auditEmit().LogLibraryCreated(r.Context(), r, lib.ID, lib.Name, lib.ContentType)

	// Refresh M3U en background via el lifecycle del service
	// (audit olor GGGG): shutdown drena en vez de cortar el write.
	libID := lib.ID
	// Captura el actor + meta del request HTTP ANTES de spawn — la
	// goroutine NO debe usar el r.Context() del request (se cancela
	// al cerrar la respuesta). El audit emit acepta nil request, así
	// que IP/UA quedarán vacíos; el actor_user_id se preserva via
	// claims que pasamos en un ctx propio.
	actorCtx := r.Context()
	h.svc.SpawnBackground(func(bgCtx context.Context) {
		ctx, cancel := context.WithTimeout(bgCtx, 2*time.Minute)
		defer cancel()
		count, err := h.svc.RefreshM3U(ctx, libID)
		if err != nil {
			h.logger.Error("public IPTV M3U refresh failed", "library", libID, "error", err)
			return
		}
		h.logger.Info("public IPTV imported", "library", libID, "country", req.Country, "channels", count)
		// Audit del M3U import con el count REAL de canales. Usamos
		// actorCtx (con las claims preservadas) pero pasamos nil
		// request porque ya no tenemos el HTTP r en este punto.
		h.auditEmit().LogIPTVImported(actorCtx, nil, libID, count)
	})

	handlers.RespondJSON(w, http.StatusCreated, map[string]any{
		"data": map[string]any{
			"library_id": lib.ID,
			"name":       lib.Name,
			"country":    req.Country,
			"m3u_url":    lib.M3UURL,
		},
	})
}

func generateLibraryID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
