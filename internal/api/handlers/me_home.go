// me_home.go — endpoints powering the configurable home page.
//
// Routes (all under /api/v1, all behind Auth middleware):
//
//   GET  /me/home/layout       returns the caller's home layout, with
//                              defaults generated server-side if the
//                              user has never customised theirs.
//   PUT  /me/home/layout       persists the caller's layout. Accepts
//                              the same shape returned by GET. Sections
//                              with unknown types or unreachable
//                              library_ids are dropped silently — the
//                              client doesn't need to know which
//                              libraries it can see.
//   GET  /me/home/trending     "trending this week": top items by
//                              distinct-user plays in the trailing
//                              7-day window, scoped to libraries the
//                              caller can access.
//   GET  /me/home/live-now     up to N live channels with their
//                              currently airing EPG program.
//
// "Latest in library" doesn't get a new endpoint — the existing
// /api/v1/items/latest?library_id=... already serves the same payload
// shape the frontend's LatestInLibraryRail consumes.

package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"hubplay/internal/auth"
	"hubplay/internal/db"
	"hubplay/internal/iptv"
)

// HomeHandler exposes the home-page customisation + discovery rails.
type HomeHandler struct {
	home     *db.HomeRepository
	prefs    UserPreferencesRepo
	libs     HomeLibraryLister
	items    *db.ItemRepository
	images   ImageRepository
	metadata HomeMetadataRepo
	logger   *slog.Logger
}

// HomeLibraryLister is the slice of LibraryRepository the home
// handler needs — kept narrow so tests can stub without dragging in
// the entire library service.
type HomeLibraryLister interface {
	ListForUser(ctx context.Context, userID string) ([]*db.Library, error)
	GetByID(ctx context.Context, id string) (*db.Library, error)
}

// HomeMetadataRepo is the slice of MetadataRepository this handler
// uses to enrich trending cards with overview/genres/poster colour
// hints. Optional — handler degrades gracefully when nil.
type HomeMetadataRepo interface {
	GetMetadataBatch(ctx context.Context, itemIDs []string) (map[string]*db.Metadata, error)
}

func NewHomeHandler(
	home *db.HomeRepository,
	prefs UserPreferencesRepo,
	libs HomeLibraryLister,
	items *db.ItemRepository,
	images ImageRepository,
	metadata HomeMetadataRepo,
	logger *slog.Logger,
) *HomeHandler {
	return &HomeHandler{
		home:     home,
		prefs:    prefs,
		libs:     libs,
		items:    items,
		images:   images,
		metadata: metadata,
		logger:   logger.With("module", "home-handler"),
	}
}

// homeLayoutKey is the well-known user_preferences row that stores
// the user's home layout JSON. One key per user; the value is the
// full HomeLayout struct serialised.
const homeLayoutKey = "home_layout"

// HomeLayout is the document persisted under home_layout for one
// user. Versioned so a future schema change can detect old payloads
// and migrate them in-place rather than wiping every user's setup.
type HomeLayout struct {
	Version  int           `json:"version"`
	Sections []HomeSection `json:"sections"`
}

// HomeSection is one rail in the user's home page. The same shape is
// used both on the wire and in storage. `LibraryID` is set only when
// `Type == "latest_in_library"`; for global rails (continue_watching,
// trending, live_now, next_up) it is empty.
//
// `LibraryName` is computed server-side on GET (so the client
// renders the rail title without a second round-trip when a library
// is renamed) and ignored on PUT (read-only).
type HomeSection struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	LibraryID   string `json:"library_id,omitempty"`
	LibraryName string `json:"library_name,omitempty"`
	Visible     bool   `json:"visible"`
}

// validSectionType returns true for any rail type the home renderer
// understands. PUTs containing unknown types are rejected at write
// time so the persisted layout stays a normal form the renderer can
// trust.
func validSectionType(t string) bool {
	switch t {
	case "continue_watching", "next_up", "trending", "live_now", "latest_in_library":
		return true
	default:
		return false
	}
}

// ─── GET /me/home/layout ─────────────────────────────────────────────

// GetLayout returns the caller's home layout. If the user has never
// saved a layout, generate a sensible default from their accessible
// libraries (movies/series get a "latest_in_library" rail each;
// livetv libraries don't, since the global "live_now" rail covers
// them). The default is NOT persisted — it stays implicit until the
// user actively customises and saves.
func (h *HomeHandler) GetLayout(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	libs, err := h.libs.ListForUser(r.Context(), claims.UserID)
	if err != nil {
		h.logger.Error("list libraries for layout", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load libraries")
		return
	}
	libByID := indexLibraries(libs)

	stored, err := h.loadStoredLayout(r.Context(), claims.UserID)
	if err != nil {
		h.logger.Error("load stored layout", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load layout")
		return
	}

	var layout HomeLayout
	if stored != nil {
		layout = *stored
		// Reconcile: drop sections whose library_id no longer exists
		// (deleted library) and append rails for newly added
		// libraries the user hasn't seen yet, defaulted to visible.
		// Same pattern Jellyfin uses — the user's manual ordering
		// for libraries they've already seen is preserved, new ones
		// just slot in at the end.
		layout.Sections = reconcileLayout(layout.Sections, libs)
	} else {
		layout = defaultLayout(libs)
	}

	// Resolve display names for every latest_in_library section so
	// the client can render rail titles without a second round-trip.
	for i := range layout.Sections {
		if layout.Sections[i].Type == "latest_in_library" {
			if lib, ok := libByID[layout.Sections[i].LibraryID]; ok {
				layout.Sections[i].LibraryName = lib.Name
			}
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{"data": layout})
}

// ─── PUT /me/home/layout ─────────────────────────────────────────────

// PutLayout persists the caller's home layout. Sections with
// unknown types are dropped; latest_in_library sections referencing
// libraries the user can't see are dropped (defence in depth — the
// handler doesn't need to trust the client's view of access).
// Returns the persisted, normalised layout so the client can rehydrate
// without a follow-up GET.
func (h *HomeHandler) PutLayout(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	var body HomeLayout
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		respondError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid JSON body")
		return
	}

	libs, err := h.libs.ListForUser(r.Context(), claims.UserID)
	if err != nil {
		h.logger.Error("list libraries for layout put", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load libraries")
		return
	}
	libByID := indexLibraries(libs)

	cleaned := make([]HomeSection, 0, len(body.Sections))
	for _, s := range body.Sections {
		if !validSectionType(s.Type) {
			continue
		}
		if s.Type == "latest_in_library" {
			if _, ok := libByID[s.LibraryID]; !ok {
				continue
			}
		}
		// LibraryName is read-only — strip whatever the client sent.
		s.LibraryName = ""
		// IDs are required so the frontend can key drag-reorder
		// stably; synthesise one if missing.
		if s.ID == "" {
			s.ID = synthSectionID(s)
		}
		cleaned = append(cleaned, s)
	}

	persist := HomeLayout{Version: 1, Sections: cleaned}
	raw, err := json.Marshal(persist)
	if err != nil {
		h.logger.Error("marshal layout", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to encode layout")
		return
	}
	if _, err := h.prefs.Set(r.Context(), claims.UserID, homeLayoutKey, string(raw)); err != nil {
		h.logger.Error("save layout", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to save layout")
		return
	}

	// Echo back with library names resolved, same as GET.
	for i := range persist.Sections {
		if persist.Sections[i].Type == "latest_in_library" {
			if lib, ok := libByID[persist.Sections[i].LibraryID]; ok {
				persist.Sections[i].LibraryName = lib.Name
			}
		}
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": persist})
}

// ─── GET /me/home/trending ───────────────────────────────────────────

// Trending returns the top items watched across all users in the
// trailing 7-day window, scoped to libraries the caller can see.
// `limit` query param caps the response size (default 12, max 30).
func (h *HomeHandler) Trending(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	limit := parseLimit(r, 12, 30)

	rows, err := h.home.Trending(r.Context(), claims.UserID, 7, limit)
	if err != nil {
		h.logger.Error("trending query", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load trending")
		return
	}

	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		entry := map[string]any{
			"id":         row.ID,
			"type":       row.Type,
			"title":      row.Title,
			"library_id": row.LibraryID,
			"play_count": row.PlayCount,
		}
		if row.Year.Valid {
			entry["year"] = row.Year.Int64
		}
		if row.CommunityRating.Valid {
			entry["community_rating"] = row.CommunityRating.Float64
		}
		out = append(out, entry)
	}

	// Enrich with images (poster + backdrop + logo) so the cards
	// look identical to those rendered by /items/latest.
	if h.images != nil {
		ids := db.IDsFromTrending(rows)
		imgs, ierr := h.images.GetPrimaryURLs(r.Context(), ids)
		if ierr != nil {
			h.logger.Warn("trending image fetch", "error", ierr)
		} else {
			for i, row := range rows {
				if urls, ok := imgs[row.ID]; ok {
					if poster, ok := urls["primary"]; ok {
						out[i]["poster_url"] = poster.Path
						attachPosterPlaceholder(out[i], poster)
					}
					if backdrop, ok := urls["backdrop"]; ok {
						out[i]["backdrop_url"] = backdrop.Path
					}
					if logo, ok := urls["logo"]; ok {
						out[i]["logo_url"] = logo.Path
					}
				}
			}
		}
	}

	// Enrich with overview/genres so trending cards can show the
	// same chips as Continue Watching.
	if h.metadata != nil {
		ids := db.IDsFromTrending(rows)
		metas, merr := h.metadata.GetMetadataBatch(r.Context(), ids)
		if merr != nil {
			h.logger.Warn("trending metadata fetch", "error", merr)
		} else {
			for i, row := range rows {
				if m, ok := metas[row.ID]; ok {
					if m.Overview != "" {
						out[i]["overview"] = m.Overview
					}
					if m.GenresJSON != "" {
						var genres []string
						if jerr := json.Unmarshal([]byte(m.GenresJSON), &genres); jerr == nil && len(genres) > 0 {
							out[i]["genres"] = genres
						}
					}
				}
			}
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"items": out,
			"total": len(out),
		},
	})
}

// ─── GET /me/home/recommended ────────────────────────────────────────

// Recommended powers the "Recomendado para ti" tier of the home hero.
// Picks unwatched movies / series that share genres with what the
// caller most actively watches, attaching the matched genres so the
// frontend can render a "Porque te gusta {{genre}}" subtitle.
//
// Returns an empty list (200) when the caller has no engagement
// history — the cold-start case. The hero hides the slot rather than
// erroring; falling back to a generic "newest" pick would just be
// the New tier rendered twice with different copy.
//
// `limit` caps the response size (default 5, max 20).
func (h *HomeHandler) Recommended(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	limit := parseLimit(r, 5, 20)

	rows, err := h.home.Recommended(r.Context(), claims.UserID, limit)
	if err != nil {
		h.logger.Error("recommended query", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load recommended")
		return
	}

	out := make([]map[string]any, 0, len(rows))
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		entry := map[string]any{
			"id":         row.ID,
			"type":       row.Type,
			"title":      row.Title,
			"library_id": row.LibraryID,
			// `recommended_because` is a stable wire field the hero
			// renders as "Porque te gusta {{genres[0]}} y {{genres[1]}}".
			// Carrying the matched genres (not the user's top-3) means
			// the copy honestly explains *this* item's match instead
			// of overpromising shared affinity.
			"recommended_because": map[string]any{"genres": row.Because},
		}
		if row.Year.Valid {
			entry["year"] = row.Year.Int64
		}
		if row.CommunityRating.Valid {
			entry["community_rating"] = row.CommunityRating.Float64
		}
		out = append(out, entry)
		ids = append(ids, row.ID)
	}

	if h.images != nil && len(ids) > 0 {
		imgs, ierr := h.images.GetPrimaryURLs(r.Context(), ids)
		if ierr != nil {
			h.logger.Warn("recommended image fetch", "error", ierr)
		} else {
			for i, row := range rows {
				if urls, ok := imgs[row.ID]; ok {
					if poster, ok := urls["primary"]; ok {
						out[i]["poster_url"] = poster.Path
						attachPosterPlaceholder(out[i], poster)
					}
					if backdrop, ok := urls["backdrop"]; ok {
						out[i]["backdrop_url"] = backdrop.Path
					}
					if logo, ok := urls["logo"]; ok {
						out[i]["logo_url"] = logo.Path
					}
				}
			}
		}
	}

	if h.metadata != nil && len(ids) > 0 {
		metas, merr := h.metadata.GetMetadataBatch(r.Context(), ids)
		if merr != nil {
			h.logger.Warn("recommended metadata fetch", "error", merr)
		} else {
			for i, row := range rows {
				if m, ok := metas[row.ID]; ok {
					if m.Overview != "" {
						out[i]["overview"] = m.Overview
					}
					if m.GenresJSON != "" {
						var genres []string
						if jerr := json.Unmarshal([]byte(m.GenresJSON), &genres); jerr == nil && len(genres) > 0 {
							out[i]["genres"] = genres
						}
					}
				}
			}
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"items": out,
			"total": len(out),
		},
	})
}

// ─── GET /me/home/live-now ───────────────────────────────────────────

// LiveNow returns up to N live channels with their currently airing
// EPG program. `limit` query param caps the response size (default 5,
// max 20).
func (h *HomeHandler) LiveNow(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	limit := parseLimit(r, 5, 20)

	rows, err := h.home.LiveNow(r.Context(), claims.UserID, limit)
	if err != nil {
		h.logger.Error("live now query", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load live now")
		return
	}

	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		// Deterministic placeholder avatar so the home rail can match
		// the LiveTV browser's look when a channel has no logo or the
		// upstream 404s. Same recipe as channelDTO (iptv_dto.go) — both
		// surfaces hand the frontend identical (initials, bg, fg) for
		// the same channel name, so a card on the home page and on
		// /live-tv don't drift in colour or letters. Always populated,
		// even when channel_logo is present, so the onError fallback
		// in <ChannelLogo> never has to guess.
		logo := iptv.DeriveLogoFallback(row.ChannelName)
		entry := map[string]any{
			"channel_id":    row.ChannelID,
			"channel_name":  row.ChannelName,
			"library_id":    row.LibraryID,
			"library_name":  row.LibraryName,
			"logo_initials": logo.Initials,
			"logo_bg":       logo.Background,
			"logo_fg":       logo.Foreground,
		}
		// Channel logos go through the same-origin proxy so a strict
		// img-src CSP doesn't have to whitelist every upstream the
		// M3U references. Empty when the channel has no logo at all
		// — the LiveNowCard falls back to initials in that case.
		if row.ChannelLogo.Valid && row.ChannelLogo.String != "" {
			entry["channel_logo"] = "/api/v1/channels/" + row.ChannelID + "/logo"
		}
		if row.ProgramTitle.Valid {
			entry["program_title"] = row.ProgramTitle.String
		}
		if row.ProgramStart.Valid {
			entry["program_start"] = row.ProgramStart.Time
		}
		if row.ProgramEnd.Valid {
			entry["program_end"] = row.ProgramEnd.Time
		}
		if row.ProgramIcon.Valid {
			entry["program_icon"] = row.ProgramIcon.String
		}
		out = append(out, entry)
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"items": out,
			"total": len(out),
		},
	})
}

// ─── helpers ─────────────────────────────────────────────────────────

func (h *HomeHandler) loadStoredLayout(ctx context.Context, userID string) (*HomeLayout, error) {
	rows, err := h.prefs.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, p := range rows {
		if p.Key == homeLayoutKey {
			var layout HomeLayout
			if jerr := json.Unmarshal([]byte(p.Value), &layout); jerr != nil {
				// Corrupt payload → fall back to defaults rather
				// than failing the whole home page.
				return nil, nil //nolint:nilerr
			}
			return &layout, nil
		}
	}
	return nil, nil
}

func indexLibraries(libs []*db.Library) map[string]*db.Library {
	out := make(map[string]*db.Library, len(libs))
	for _, l := range libs {
		out[l.ID] = l
	}
	return out
}

// defaultLayout generates the home layout for a user who has never
// customised theirs. Order matches the most common Jellyfin /
// Plex web layout: continue → next-up → trending → live → catalog
// rails (one per non-livetv library).
func defaultLayout(libs []*db.Library) HomeLayout {
	sections := []HomeSection{
		{ID: "continue_watching", Type: "continue_watching", Visible: true},
		{ID: "next_up", Type: "next_up", Visible: true},
		{ID: "trending", Type: "trending", Visible: true},
	}

	// Live Now section only matters when there's at least one
	// livetv library; otherwise hide it from the default so an
	// empty rail doesn't render a stub.
	hasLiveTV := false
	for _, l := range libs {
		if l.ContentType == "livetv" {
			hasLiveTV = true
			break
		}
	}
	sections = append(sections, HomeSection{
		ID: "live_now", Type: "live_now", Visible: hasLiveTV,
	})

	for _, l := range libs {
		if l.ContentType == "livetv" {
			continue
		}
		sections = append(sections, HomeSection{
			ID:        "latest_in_lib_" + l.ID,
			Type:      "latest_in_library",
			LibraryID: l.ID,
			Visible:   true,
		})
	}
	return HomeLayout{Version: 1, Sections: sections}
}

// reconcileLayout merges a stored layout against the user's current
// library set. Sections referencing dead libraries get dropped;
// libraries that have no section yet get appended (visible by
// default), preserving the user's manual order for everything else.
func reconcileLayout(stored []HomeSection, libs []*db.Library) []HomeSection {
	libByID := indexLibraries(libs)
	seenLib := make(map[string]bool, len(stored))

	out := make([]HomeSection, 0, len(stored))
	for _, s := range stored {
		if !validSectionType(s.Type) {
			continue
		}
		if s.Type == "latest_in_library" {
			if _, ok := libByID[s.LibraryID]; !ok {
				continue
			}
			seenLib[s.LibraryID] = true
		}
		out = append(out, s)
	}

	for _, l := range libs {
		if l.ContentType == "livetv" || seenLib[l.ID] {
			continue
		}
		out = append(out, HomeSection{
			ID:        "latest_in_lib_" + l.ID,
			Type:      "latest_in_library",
			LibraryID: l.ID,
			Visible:   true,
		})
	}
	return out
}

// synthSectionID generates a stable id for a section that arrived
// without one. Kept deterministic so the same logical section gets
// the same id across PUTs.
func synthSectionID(s HomeSection) string {
	if s.Type == "latest_in_library" {
		return "latest_in_lib_" + s.LibraryID
	}
	return s.Type
}

// parseLimit is a small util used by Trending and LiveNow.
func parseLimit(r *http.Request, def, max int) int {
	q := r.URL.Query().Get("limit")
	if q == "" {
		return def
	}
	n, err := strconv.Atoi(q)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

