package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"hubplay/internal/auth"
	"hubplay/internal/library"
	librarymodel "hubplay/internal/library/model"
)

// SearchHandler aísla la búsqueda de items vía
// `GET /items/search?q=…&library_id=&type=&genre=&year_from=&year_to=&min_rating=`.
// Mismo gate de content-rating per-profile que las rails Latest /
// Browse — un profile con cap PG-13 no puede ver resultados R aunque
// tipée la query exacta.
//
// Extraído como sub-handler para cerrar parte del olor P (ItemHandler
// god-handler). Sus 5 deps son disjuntas del resto del split: lib +
// images + userData para el enrich, users sólo para el cap-rating.
type SearchHandler struct {
	lib      LibraryService
	images   ImageRepository
	userData UserDataRepository
	users    UserService
	logger   *slog.Logger
}

func newSearchHandler(lib LibraryService, images ImageRepository, userData UserDataRepository, users UserService, logger *slog.Logger) *SearchHandler {
	return &SearchHandler{
		lib:      lib,
		images:   images,
		userData: userData,
		users:    users,
		logger:   logger,
	}
}

// callerCapRating resuelve el cap del caller (mismo helper duplicado en
// ItemHandler, LibraryHandler y HomeHandler). nil-safe — sin
// UserService o sin claims devuelve "" (fail-open) que `AllowedRatings`
// trata como "todo permitido".
func (h *SearchHandler) callerCapRating(ctx context.Context) string {
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

// Search atiende `GET /items/search`. MediaBrowse hace JOIN del filter
// surface (genre/year/rating) con la query — pasar la `q` solo no es
// suficiente: hay que respetar lo que el usuario ya tenía aplicado en
// la grid, o tipear en el topbar deshace silenciosamente la selección.
// El SearchBar global pasa null para esos campos.
func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := q.Get("q")
	if query == "" {
		respondError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "query parameter 'q' is required")
		return
	}

	offset, limit, _ := parsePaginationFromValues(w, r, q)
	libraryID := q.Get("library_id")
	itemType := q.Get("type")
	genre := q.Get("genre")
	yearFrom, _ := strconv.Atoi(q.Get("year_from"))
	yearTo, _ := strconv.Atoi(q.Get("year_to"))
	minRating, _ := strconv.ParseFloat(q.Get("min_rating"), 64)

	// Per-profile content cap — mismo gate que Latest / Browse. Un
	// profile con max_content_rating="PG-13" tipeando "fight club" en
	// la search global NO debe ver el resultado R; la implementación
	// previa skipeaba este filtro y dejaba la search bar bypass de
	// kid mode entero.
	cap := h.callerCapRating(r.Context())

	items, total, err := h.lib.ListItems(r.Context(), librarymodel.ItemFilter{
		LibraryID:             libraryID,
		Type:                  itemType,
		Query:                 query,
		Genre:                 genre,
		YearFrom:              yearFrom,
		YearTo:                yearTo,
		MinRating:             minRating,
		Limit:                 limit,
		Offset:                offset,
		AllowedContentRatings: library.AllowedRatingsAtMost(cap),
	})
	if err != nil {
		handleServiceError(w, r, err)
		return
	}

	data := make([]map[string]any, len(items))
	for i, item := range items {
		data[i] = itemSummaryResponse(item)
	}

	// Enrich con poster URLs.
	if h.images != nil && len(items) > 0 {
		itemIDs := make([]string, len(items))
		for i, item := range items {
			itemIDs[i] = item.ID
		}
		if imageURLs, err := h.images.GetPrimaryURLs(r.Context(), itemIDs); err == nil {
			for i, item := range items {
				if urls, ok := imageURLs[item.ID]; ok {
					if poster, ok := urls["primary"]; ok {
						data[i]["poster_url"] = poster.Path
						attachPosterPlaceholder(data[i], poster)
					}
				}
			}
		}
	}

	// Per-user state para los resultados (badges watched/in-progress).
	if h.userData != nil && len(items) > 0 {
		if claims := auth.GetClaims(r.Context()); claims != nil {
			itemIDs := make([]string, len(items))
			for i, item := range items {
				itemIDs[i] = item.ID
			}
			if userDataByID, err := h.userData.GetBatch(r.Context(), claims.UserID, itemIDs); err != nil {
				h.logger.Warn("get user data batch", "error", err)
			} else if len(userDataByID) > 0 {
				for i, item := range items {
					if ud, ok := userDataByID[item.ID]; ok {
						data[i]["user_data"] = userDataResponse(ud, item.DurationTicks)
					}
				}
			}
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data":  data,
		"total": total,
	})
}
