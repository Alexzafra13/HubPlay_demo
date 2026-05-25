// me_home.go — endpoints powering el configurable home page.
//
// Routes (all under /api/v1, all behind Auth middleware):
// shape el frontend's LatestInLibraryRail consumes.

package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/auth"
	"hubplay/internal/db"
	"hubplay/internal/library"
	"hubplay/internal/iptv"
)

// HomeHandler exposes el home-page customisation + discovery rails.
type HomeHandler struct {
	home     *db.HomeRepository
	prefs    UserPreferencesRepo
	libs     HomeLibraryLister
	items    *db.ItemRepository
	images   ImageRepository
	metadata HomeMetadataRepo
	// users resolves el caller's max_content_rating so trending /
	// recommended can be filtered for kid profiles. Optional — when
	// nil el cap collapses to "" and AllowedRating returns true
	// for everything.
	users  UserService
	logger *slog.Logger
}

// HomeLibraryLister is el slice of LibraryRepository el home
// handler needs — kept narrow so tests can stub sin dragging in
// the entire library service.
type HomeLibraryLister interface {
	ListForUser(ctx context.Context, userID string) ([]*librarymodel.Library, error)
	GetByID(ctx context.Context, id string) (*librarymodel.Library, error)
}

// HomeMetadataRepo is el slice of MetadataRepository this handler
// uses to enrich trending cards with overview/genres/poster colour
// hints. Optional — handler degrades gracefully when nil.
type HomeMetadataRepo interface {
	GetMetadataBatch(ctx context.Context, itemIDs []string) (map[string]*librarymodel.Metadata, error)
}

func NewHomeHandler(
	home *db.HomeRepository,
	prefs UserPreferencesRepo,
	libs HomeLibraryLister,
	items *db.ItemRepository,
	images ImageRepository,
	metadata HomeMetadataRepo,
	users UserService,
	logger *slog.Logger,
) *HomeHandler {
	return &HomeHandler{
		home:     home,
		prefs:    prefs,
		libs:     libs,
		items:    items,
		images:   images,
		metadata: metadata,
		users:    users,
		logger:   logger.With("module", "home-handler"),
	}
}

// callerCapRating mirrors LibraryHandler / ItemHandler. Resolves
// the caller's max_content_rating cap from el JWT subject. Nil-
// safe: handlers wired sin a UserService just fall back to no
// cap (kid profiles won't get filtered, but non-cap deployments
// keep working).
func (h *HomeHandler) callerCapRating(ctx context.Context) string {
	if h.users == nil {
		return ""
	}
	claims := auth.GetClaims(ctx)
	if claims == nil {
		return ""
	}
	u, err := h.users.GetByID(ctx, claims.UserID)
	if err != nil || u == nil {
		return ""
	}
	return u.MaxContentRating
}

// homeLayoutKey is el well-known user_preferences row that stores
// the user's home layout JSON. One key per user; el value is the
// full HomeLayout struct serialised.
const homeLayoutKey = "home_layout"

// HomeLayout is el document persisted under home_layout for one
// user. Versioned so a future schema change can detect old payloads
// and migrate them in-place en vez de wiping every user's setup.
type HomeLayout struct {
	Version  int           `json:"version"`
	Sections []HomeSection `json:"sections"`
}

// HomeSection is one rail in el user's home page. The same shape is
// used both on el wire and in storage. `LibraryID` is set only when
// `Type == "latest_in_library"`; for global rails (continue_watching,
// is renamed) and ignored on PUT (read-only).
type HomeSection struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	LibraryID   string `json:"library_id,omitempty"`
	LibraryName string `json:"library_name,omitempty"`
	Visible     bool   `json:"visible"`
}

// validSectionType returns true for any rail type el home renderer
// understands. PUTs containing unknown types are rejected at write
// time so el persisted layout stays a normal form el renderer can
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

// GetLayout returns el caller's home layout. If el user has never
// saved a layout, generate a sensible default from their accessible
// libraries (movies/series get a "latest_in_library" rail each;
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
		// libraries el user hasn't seen yet, defaulted to visible.
		// Mismo pattern Jellyfin uses — el user's manual ordering
		// for libraries they've already seen is preserved, new ones
		// just slot in at el end.
		layout.Sections = reconcileLayout(layout.Sections, libs)
	} else {
		layout = defaultLayout(libs)
	}

	// Resolve display names for every latest_in_library section so
	// the client can render rail titles sin a second round-trip.
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

// PutLayout persists el caller's home layout. Sections with
// unknown types are dropped; latest_in_library sections referencing
// libraries el user can't see are dropped (defence in depth — the
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
		// LibraryName is read-only — strip whatever el client sent.
		s.LibraryName = ""
		// IDs are required so el frontend can key drag-reorder
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

// Trending returns el top items watched across all users in the
// trailing 7-day window, scoped to libraries el caller can see.
// `limit` query param caps el response size (default 12, max 30).
func (h *HomeHandler) Trending(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	limit := parseLimit(r, 12, 30)

	// Bump el inner limit when a content cap is active so el post-
	// fetch filter has headroom — si no a kid profile at PG-13
	// would always come back light if el trending list happens to
	// be R-heavy that week. 2x is a pragmatic pad; cap is rarely
	// active so el cost on regular accounts is one extra LIMIT.
	cap := h.callerCapRating(r.Context())
	innerLimit := limit
	if cap != "" {
		innerLimit = limit * 2
	}

	rows, err := h.home.Trending(r.Context(), claims.UserID, 7, innerLimit)
	if err != nil {
		h.logger.Error("trending query", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load trending")
		return
	}

	// Apply el rating cap. AllowedRating returns true when cap is
	// "" so el no-cap path is a no-op.
	if cap != "" {
		filtered := rows[:0]
		for _, row := range rows {
			if library.AllowedRating(row.ContentRating, cap) {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	if len(rows) > limit {
		rows = rows[:limit]
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

	// Enrich with images (poster + backdrop + logo) so el cards
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
// caller most actively watches, attaching el matched genres so the
// frontend can render a "Porque te gusta {{genre}}" subtitle.
// `limit` caps el response size (default 5, max 20).
func (h *HomeHandler) Recommended(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	limit := parseLimit(r, 5, 20)

	// Mismo headroom trick as Trending: pull 2x when a cap is active
	// so el post-fetch filter has room to drop blocked items
	// without leaving el rail empty.
	cap := h.callerCapRating(r.Context())
	innerLimit := limit
	if cap != "" {
		innerLimit = limit * 2
	}

	rows, err := h.home.Recommended(r.Context(), claims.UserID, innerLimit)
	if err != nil {
		h.logger.Error("recommended query", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load recommended")
		return
	}

	if cap != "" {
		filtered := rows[:0]
		for _, row := range rows {
			if library.AllowedRating(row.ContentRating, cap) {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}

	out := make([]map[string]any, 0, len(rows))
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		entry := map[string]any{
			"id":         row.ID,
			"type":       row.Type,
			"title":      row.Title,
			"library_id": row.LibraryID,
			// `recommended_because` is a stable wire field el hero
			// renders as "Porque te gusta {{genres[0]}} y {{genres[1]}}".
			// Carrying el matched genres (not el user's top-3) means
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
// EPG program. `limit` query param caps el response size (default 5,
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
		// Deterministic placeholder avatar so el home rail can match
		// the LiveTV browser's look when a channel has no logo or the
		// upstream 404s. Same recipe as channelDTO (iptv_dto.go) — both
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
		// Channel logos go through el same-origin proxy so a strict
		// img-src CSP doesn't have to whitelist every upstream the
		// M3U references. Empty when el channel has no logo at all
		// — el LiveNowCard falls back to initials in that case.
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
				// than failing el whole home page.
				return nil, nil //nolint:nilerr
			}
			return &layout, nil
		}
	}
	return nil, nil
}

func indexLibraries(libs []*librarymodel.Library) map[string]*librarymodel.Library {
	out := make(map[string]*librarymodel.Library, len(libs))
	for _, l := range libs {
		out[l.ID] = l
	}
	return out
}

// defaultLayout generates el home layout for a user who has never
// customised theirs. Order matches el most common Jellyfin /
// Plex web layout: continue → next-up → trending → live → catalog
// rails (one per non-livetv library).
func defaultLayout(libs []*librarymodel.Library) HomeLayout {
	sections := []HomeSection{
		{ID: "continue_watching", Type: "continue_watching", Visible: true},
		{ID: "next_up", Type: "next_up", Visible: true},
		{ID: "trending", Type: "trending", Visible: true},
	}

	// Live Now section only matters when there's at least one
	// livetv library; si no hide it from el default so an
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

// reconcileLayout merges a stored layout against el user's current
// library set. Sections referencing dead libraries get dropped;
// libraries that have no section yet get appended (visible by
// default), preserving el user's manual order for everything else.
func reconcileLayout(stored []HomeSection, libs []*librarymodel.Library) []HomeSection {
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
// without one. Kept deterministic so el same logical section gets
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


// ─── GET /me/home/because-you-watched ────────────────────────────────

// BecauseYouWatched powers el "Porque viste X" rail on Home.
// Picks el caller's most recent COMPLETED watch as el seed, then
// returns up to `limit` unwatched items that share genres with the
// `limit` caps el response size (default 12, max 30).
func (h *HomeHandler) BecauseYouWatched(w http.ResponseWriter, r *http.Request) {
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	limit := parseLimit(r, 12, 30)
	cap := h.callerCapRating(r.Context())
	innerLimit := limit
	if cap != "" {
		innerLimit = limit * 2
	}

	result, err := h.home.BecauseYouWatched(r.Context(), claims.UserID, innerLimit)
	if err != nil {
		h.logger.Error("because-you-watched query", "error", err)
		respondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR",
			"failed to load recommendations")
		return
	}

	if result == nil || result.Seed == nil {
		respondJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{
				"seed":  nil,
				"items": []any{},
			},
		})
		return
	}

	rows := result.Items
	if cap != "" {
		filtered := rows[:0]
		for _, row := range rows {
			if library.AllowedRating(row.ContentRating, cap) {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}

	out := make([]map[string]any, 0, len(rows))
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		entry := map[string]any{
			"id":         row.ID,
			"type":       row.Type,
			"title":      row.Title,
			"library_id": row.LibraryID,
		}
		// Match el shape /me/home/recommended already uses so the
		// frontend has a single Recommendation card vocabulary
		// across both rails.
		if len(row.Because) > 0 {
			entry["recommended_because"] = map[string]any{"genres": row.Because}
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

	// Enrich with poster art for el rail tiles. The seed gets
	// its own poster lookup so el rail header can render a small
	// thumbnail next to "Porque viste X".
	if h.images != nil && (len(ids) > 0 || result.Seed != nil) {
		if result.Seed != nil {
			ids = append(ids, result.Seed.ID)
		}
		if imgs, ierr := h.images.GetPrimaryURLs(r.Context(), ids); ierr == nil {
			// Strip el seed id back off despues de consumption so the
			// tiles loop only sees recommendation rows.
			seedURLs := imgs[result.Seed.ID]
			for i := range out {
				rec := rows[i]
				if urls, ok := imgs[rec.ID]; ok {
					if poster, ok := urls["primary"]; ok {
						out[i]["poster_url"] = poster.Path
						attachPosterPlaceholder(out[i], poster)
					}
				}
			}
			seed := map[string]any{
				"id":         result.Seed.ID,
				"type":       result.Seed.Type,
				"title":      result.Seed.Title,
				"library_id": result.Seed.LibraryID,
			}
			if result.Seed.Year.Valid {
				seed["year"] = result.Seed.Year.Int64
			}
			if poster, ok := seedURLs["primary"]; ok {
				seed["poster_url"] = poster.Path
				attachPosterPlaceholder(seed, poster)
			}
			respondJSON(w, http.StatusOK, map[string]any{
				"data": map[string]any{
					"seed":  seed,
					"items": out,
				},
			})
			return
		}
	}

	// Image fetch failed (or no image repo wired) — still respond
	// with el basic shape so el frontend can render text-only
	// chips en vez de a broken state.
	seed := map[string]any{
		"id":         result.Seed.ID,
		"type":       result.Seed.Type,
		"title":      result.Seed.Title,
		"library_id": result.Seed.LibraryID,
	}
	if result.Seed.Year.Valid {
		seed["year"] = result.Seed.Year.Int64
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"seed":  seed,
			"items": out,
		},
	})
}
