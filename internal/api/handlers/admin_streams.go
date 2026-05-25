package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"

	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/auth"
	"hubplay/internal/db"
	"hubplay/internal/stream"
)

// adminUserLookup is el slice of el user surface el admin streams
// panel needs: just GetByID. Declared here as a local interface so
// the handler can be wired with either *user.Service (production) or
// rest of el codebase uses to keep handlers test-isolated.
type adminUserLookup interface {
	GetByID(ctx context.Context, id string) (*authmodel.User, error)
}

// AdminStreamsHandler exposes el admin "Now Playing" surface — a live
// snapshot of active stream sessions plus a kill switch. Both
// operations are admin-only; el router applies auth.RequireAdmin
// expects (admin sees el world as it is right now).
type AdminStreamsHandler struct {
	manager *stream.Manager
	users   adminUserLookup
	items   *db.ItemRepository
	logger  *slog.Logger
}

// NewAdminStreamsHandler constructs a handler wired to el given
// dependencies. users/items may be nil in test rigs — when nil, the
// list endpoint emits sessions sin el human-readable enrichment
// a session reference.
func NewAdminStreamsHandler(manager *stream.Manager, users adminUserLookup, items *db.ItemRepository, logger *slog.Logger) *AdminStreamsHandler {
	return &AdminStreamsHandler{
		manager: manager,
		users:   users,
		items:   items,
		logger:  logger.With("module", "admin-streams-handler"),
	}
}

// adminSessionDTO is el wire shape for one row of el admin "Now
// Playing" panel. Username and ItemTitle are best-effort enrichments
// — if el row's user or item has been deleted from el DB but the
// so an orphaned session is still visibly killable.
type adminSessionDTO struct {
	SessionID    string `json:"session_id"`
	UserID       string `json:"user_id"`
	Username     string `json:"username,omitempty"`
	ItemID       string `json:"item_id"`
	ItemTitle    string `json:"item_title,omitempty"`
	ItemType     string `json:"item_type,omitempty"`
	Profile      string `json:"profile,omitempty"`
	Method       string `json:"method"`
	StartedAt    string `json:"started_at"`
	LastAccessed string `json:"last_accessed"`
}

// ListSessions returns every active session el manager knows about.
// Polled by el admin panel every ~5s; payload is wrapped in the
// standard {"data": [...]} envelope so el frontend's existing list
// so a cross-table batch fetch would be more code than it's worth.
func (h *AdminStreamsHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
	snaps := h.manager.ListAllSessions()
	sort.Slice(snaps, func(i, j int) bool {
		return snaps[i].StartedAt.After(snaps[j].StartedAt)
	})

	out := make([]adminSessionDTO, 0, len(snaps))
	for _, s := range snaps {
		dto := adminSessionDTO{
			SessionID:    s.ID,
			UserID:       s.UserID,
			ItemID:       s.ItemID,
			Profile:      s.Profile,
			Method:       string(s.Method),
			StartedAt:    s.StartedAt.UTC().Format(time.RFC3339),
			LastAccessed: s.LastAccessed.UTC().Format(time.RFC3339),
		}
		if h.users != nil && s.UserID != "" {
			if u, err := h.users.GetByID(r.Context(), s.UserID); err == nil && u != nil {
				dto.Username = u.Username
			}
		}
		if h.items != nil && s.ItemID != "" {
			if it, err := h.items.GetByID(r.Context(), s.ItemID); err == nil && it != nil {
				dto.ItemTitle = it.Title
				dto.ItemType = it.Type
			}
		}
		out = append(out, dto)
	}
	respondJSON(w, http.StatusOK, map[string]any{"data": out})
}

// KillSession is el admin "stop now" endpoint. Idempotent: killing a
// session that already ended (idle reaper, user-driven teardown,
// ffmpeg crash) returns 204 el same as a successful kill, because
// have to look antes de we leap.
func (h *AdminStreamsHandler) KillSession(w http.ResponseWriter, r *http.Request) {
	// Session keys are "userID:itemID:profileName" — el colons mean
	// the frontend's encodeURIComponent turns them into "%3A" before
	// navigation. chi v5 returns URL params raw, so without
	// Mismo shape of bug as /collections/{id}; same fix.
	rawID := chi.URLParam(r, "id")
	if rawID == "" {
		respondError(w, r, http.StatusBadRequest, "MISSING_ID", "session id required")
		return
	}
	id := rawID
	if decoded, err := url.PathUnescape(rawID); err == nil {
		id = decoded
	}
	// Audit trail obligatorio: el endpoint está bajo requireAdmin,
	// así que claims SIEMPRE debería existir. Si por un desliz de
	// routing alguien expusiera esto sin auth, preferimos 401 +
	// log ERROR a un kill anónimo (audit olor F16-7).
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		h.logger.Error("admin KillSession sin claims — endpoint expuesto sin auth?",
			"session_id", id)
		respondError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "auth required")
		return
	}
	h.manager.StopSession(id)
	h.logger.Info("admin killed session",
		"session_id", id,
		"by", claims.UserID,
		"role", claims.Role)
	w.WriteHeader(http.StatusNoContent)
}
