package fedhandler

import (
	"context"
	"log/slog"
	"net/http"

	"hubplay/internal/api/handlers"
	"hubplay/internal/federation"
	librarymodel "hubplay/internal/library/model"
)

// childItemRepository es lo que necesita el listado de hijos: el item
// padre (para el ACL por biblioteca) y sus hijos en orden de temporada
// y episodio.
type childItemRepository interface {
	GetByID(ctx context.Context, id string) (*librarymodel.Item, error)
	GetChildren(ctx context.Context, parentID string) ([]*librarymodel.Item, error)
}

// primaryImageLookup marca has_poster sin un N+1 por episodio.
type primaryImageLookup interface {
	GetPrimaryURLs(ctx context.Context, itemIDs []string) (map[string]map[string]librarymodel.PrimaryImageRef, error)
}

// shareLookup es el trozo del Manager que decide si el peer ve la biblioteca.
type shareLookup interface {
	GetLibraryShare(ctx context.Context, peerID, libraryID string) (*federation.LibraryShare, error)
}

// FederationChildrenHandler sirve las temporadas de una serie y los
// episodios de una temporada a un peer. Sin esto una serie compartida
// solo se podía ver por fuera: el catálogo federado lista items raíz y
// el peer no tenía forma de saber los ids de los episodios (aunque el
// stream ya los aceptaba, el ACL va por biblioteca).
type FederationChildrenHandler struct {
	shares shareLookup
	items  childItemRepository
	images primaryImageLookup
	logger *slog.Logger
}

func NewFederationChildrenHandler(
	shares shareLookup,
	items childItemRepository,
	images primaryImageLookup,
	logger *slog.Logger,
) *FederationChildrenHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &FederationChildrenHandler{
		shares: shares,
		items:  items,
		images: images,
		logger: logger.With("handler", "federation_children"),
	}
}

// ItemChildren devuelve los hijos navegables de un item compartido.
//
// GET /api/v1/peer/items/{itemId}/children  (peer JWT)
//
// Mismo ACL y misma respuesta 404 que el póster: el item debe existir y
// su biblioteca estar compartida con can_browse; "no existe" y "no es
// tuyo" son indistinguibles para que un peer no pueda enumerar ids.
// Solo salen temporadas y episodios — lo único que tiene hijos en un
// catálogo de vídeo es una serie o una temporada. Misma forma que el
// listado de una biblioteca (items + total).
func (h *FederationChildrenHandler) ItemChildren(w http.ResponseWriter, r *http.Request) {
	peer := handlers.RequirePeer(w, r)
	if peer == nil {
		return
	}
	itemID := handlers.RequireParam(w, r, "itemId")
	if itemID == "" {
		return
	}
	item, err := h.items.GetByID(r.Context(), itemID)
	if err != nil || item == nil {
		handlers.RespondError(w, r, http.StatusNotFound, "ITEM_NOT_FOUND", "item not found")
		return
	}
	share, err := h.shares.GetLibraryShare(r.Context(), peer.ID, item.LibraryID)
	if err != nil {
		h.logger.Error("federation: get share for children", "error", err, "peer_id", peer.ID, "library_id", item.LibraryID)
		handlers.RespondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "share lookup failed")
		return
	}
	if share == nil || !share.CanBrowse {
		handlers.RespondError(w, r, http.StatusNotFound, "ITEM_NOT_FOUND", "item not found")
		return
	}
	children, err := h.items.GetChildren(r.Context(), itemID)
	if err != nil {
		h.logger.Error("federation: list children", "error", err, "item_id", itemID)
		handlers.RespondError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list children")
		return
	}
	out := sharedChildren(children)
	h.markPosters(r.Context(), out)
	handlers.RespondJSON(w, http.StatusOK, map[string]any{
		"items": out,
		"total": len(out),
	})
}

// sharedChildren pasa temporadas y episodios a la forma federada,
// conservando el orden del repo (temporada, episodio, título).
func sharedChildren(children []*librarymodel.Item) []*federation.SharedItem {
	out := make([]*federation.SharedItem, 0, len(children))
	for _, c := range children {
		if c.Type != "season" && c.Type != "episode" {
			continue
		}
		out = append(out, &federation.SharedItem{
			ID:            c.ID,
			Type:          c.Type,
			Title:         c.Title,
			Year:          c.Year,
			LibraryID:     c.LibraryID,
			SeasonNumber:  c.SeasonNumber,
			EpisodeNumber: c.EpisodeNumber,
			DurationTicks: c.DurationTicks,
		})
	}
	return out
}

// markPosters pone has_poster donde hay imagen primaria (el proxy de
// póster sirve esa misma). Un fallo aquí no tumba el listado: las
// tarjetas caen a su placeholder.
func (h *FederationChildrenHandler) markPosters(ctx context.Context, items []*federation.SharedItem) {
	if h.images == nil || len(items) == 0 {
		return
	}
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	urls, err := h.images.GetPrimaryURLs(ctx, ids)
	if err != nil {
		h.logger.Warn("federation: primary urls for children", "error", err)
		return
	}
	for _, it := range items {
		if ref, ok := urls[it.ID]["primary"]; ok && ref.Path != "" {
			it.HasPoster = true
		}
	}
}
