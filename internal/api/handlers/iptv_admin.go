package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"hubplay/internal/db"
	"hubplay/internal/iptv"
)

// PreflightM3U probes an M3U URL on the operator's behalf so the
// admin UI can show "this is fine" / "provider is hung" / "got HTML
// instead of a playlist" before they commit a save. Bounded to ~12s
// — see iptv.PreflightCheck for the verdict taxonomy.
//
// Admin-only because the request body comes verbatim from the
// caller and a public endpoint would let any unauthenticated user
// turn the server into a generic HTTP probe (SSRF-adjacent).
func (h *IPTVHandler) PreflightM3U(w http.ResponseWriter, r *http.Request) {
	var req struct {
		M3UURL      string `json:"m3u_url"`
		TLSInsecure bool   `json:"tls_insecure"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid JSON body")
		return
	}
	if req.M3UURL == "" {
		respondError(w, r, http.StatusBadRequest, "MISSING_URL", "m3u_url is required")
		return
	}

	result := h.svc.PreflightCheck(r.Context(), req.M3UURL, req.TLSInsecure)
	respondJSON(w, http.StatusOK, result)
}

// RefreshM3U triggers an M3U playlist refresh for a library.
//
// Already admin-only at the route level, but we also verify library access
// defence-in-depth: admins can see every library regardless of the ACL, so
// this check is effectively a documentation anchor today. It becomes
// load-bearing the day a non-admin role gains access to refresh endpoints.
func (h *IPTVHandler) RefreshM3U(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")
	if !h.canAccessLibrary(r, libraryID) {
		h.denyForbidden(w, r)
		return
	}

	count, err := h.svc.RefreshM3U(r.Context(), libraryID)
	if err != nil {
		// Log the raw error for operators; handleServiceError renders a safe
		// typed AppError (or a generic 500) without leaking upstream messages.
		h.logger.Error("M3U refresh failed", "library", libraryID, "error", err)
		handleServiceError(w, r, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"channels_imported": count,
		},
	})
}

// RefreshEPG triggers an EPG refresh for a library.
func (h *IPTVHandler) RefreshEPG(w http.ResponseWriter, r *http.Request) {
	libraryID := chi.URLParam(r, "id")
	if !h.canAccessLibrary(r, libraryID) {
		h.denyForbidden(w, r)
		return
	}

	count, err := h.svc.RefreshEPG(r.Context(), libraryID)
	if err != nil {
		h.logger.Error("EPG refresh failed", "library", libraryID, "error", err)
		handleServiceError(w, r, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"programs_imported": count,
		},
	})
}

// PublicCountries returns the list of countries with available public IPTV channels.
func (h *IPTVHandler) PublicCountries(w http.ResponseWriter, r *http.Request) {
	countries := iptv.PublicCountries()

	result := make([]map[string]any, 0, len(countries))
	for _, c := range countries {
		result = append(result, map[string]any{
			"code": c.Code,
			"name": c.Name,
			"flag": c.Flag,
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": result})
}

// ImportPublicIPTV creates a livetv library for a country and triggers M3U import.
func (h *IPTVHandler) ImportPublicIPTV(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Country string `json:"country"`
		Name    string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}

	country, ok := iptv.FindCountry(req.Country)
	if !ok {
		respondError(w, r, http.StatusBadRequest, "INVALID_COUNTRY", "unknown country code")
		return
	}

	libraryName := req.Name
	if libraryName == "" {
		libraryName = fmt.Sprintf("Live TV - %s", country.Name)
	}

	now := time.Now()
	lib := &db.Library{
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
		respondError(w, r, http.StatusInternalServerError, "CREATE_ERROR", "failed to create library")
		return
	}

	// Trigger M3U refresh in background (use detached context)
	libID := lib.ID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		count, err := h.svc.RefreshM3U(ctx, libID)
		if err != nil {
			h.logger.Error("public IPTV M3U refresh failed", "library", libID, "error", err)
			return
		}
		h.logger.Info("public IPTV imported", "library", libID, "country", req.Country, "channels", count)
	}()

	respondJSON(w, http.StatusCreated, map[string]any{
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
