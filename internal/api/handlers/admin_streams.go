package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"time"

	"hubplay/internal/auth"
	authmodel "hubplay/internal/auth/model"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/stream"
)

// adminUserLookup is the slice of the user surface the admin streams
// panel needs: just GetByID. Declared here as a local interface so
// the handler can be wired with either *user.Service (production) or
// *db.UserRepository (or a fake in tests) without growing a hard
// dependency on either concrete type. Mirrors the sink-pattern the
// rest of the codebase uses to keep handlers test-isolated.
type adminUserLookup interface {
	GetByID(ctx context.Context, id string) (*authmodel.User, error)
}

// AdminStreamsHandler exposes the admin "Now Playing" surface — a live
// snapshot of active stream sessions plus a kill switch. Both
// operations are admin-only; the router applies auth.RequireAdmin
// before reaching here, so the methods don't re-check role.
//
// The handler is a thin wrapper around the stream manager: ListSessions
// reads stream.SessionSnapshot values out of the manager and enriches
// each with username + item title for display, KillSession delegates to
// manager.StopSession (which is idempotent and publishes the same
// transcode-completed event the player teardown would). No state lives
// here — every request hits the manager fresh, which is what the panel
// expects (admin sees the world as it is right now).
type AdminStreamsHandler struct {
	manager *stream.Manager
	users   adminUserLookup
	items   adminStreamsItemLookup
	logger  *slog.Logger
}

// adminStreamsItemLookup es el contrato estrecho que el handler
// admin/streams usa del repo de items (sólo GetByID, para enriquecer
// sessions con título). Interface local en lugar del concreto cierra
// la "doble expresión" del contrato — olor H fase 2 del audit.
type adminStreamsItemLookup interface {
	GetByID(ctx context.Context, id string) (*librarymodel.Item, error)
}

// NewAdminStreamsHandler constructs a handler wired to the given
// dependencies. users/items may be nil in test rigs — when nil, the
// list endpoint emits sessions without the human-readable enrichment
// (username / item title), matching the behaviour for orphaned rows
// where the user / item has been deleted but the manager still holds
// a session reference.
func NewAdminStreamsHandler(manager *stream.Manager, users adminUserLookup, items adminStreamsItemLookup, logger *slog.Logger) *AdminStreamsHandler {
	return &AdminStreamsHandler{
		manager: manager,
		users:   users,
		items:   items,
		logger:  logger.With("module", "admin-streams-handler"),
	}
}

// adminSessionDTO is the wire shape for one row of the admin "Now
// Playing" panel. Username and ItemTitle are best-effort enrichments
// — if the row's user or item has been deleted from the DB but the
// manager still tracks the session (the kill button hasn't been
// pressed yet), the handler emits the IDs without the human-readable
// name rather than erroring out. The frontend falls back to the IDs
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

// ListSessions returns every active session the manager knows about.
// Polled by the admin panel every ~5s; payload is wrapped in the
// standard {"data": [...]} envelope so the frontend's existing list
// fetching helpers don't need a special case.
//
// Sessions are sorted by StartedAt descending so the freshest reads
// at the top — matches the typical "what's happening now?" workflow.
// Per-session enrichment lookups are sequential because N is bounded
// (max_transcode_sessions defaults to 8, hard-capped on construction),
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
	respondData(w, http.StatusOK, out)
}

// KillSession is the admin "stop now" endpoint. Idempotent: killing a
// session that already ended (idle reaper, user-driven teardown,
// ffmpeg crash) returns 204 the same as a successful kill, because
// from the admin's perspective the user-visible result is identical
// — that session isn't running anymore. The manager's StopSession
// short-circuits cleanly when the key isn't in the map, so we don't
// have to look before we leap.
func (h *AdminStreamsHandler) KillSession(w http.ResponseWriter, r *http.Request) {
	// Session keys are "userID:itemID:profileName" — the colons mean
	// the frontend's encodeURIComponent turns them into "%3A" before
	// navigation. chi v5 returns URL params raw, so without
	// PathUnescape on the way in StopSession would receive the literal
	// "user%3Aitem%3Aprofile" string, miss the map lookup, and 204
	// without actually killing anything (StopSession is idempotent).
	// Same shape of bug as /collections/{id}; same fix.
	rawID := requireParam(w, r, "id")
	if rawID == "" {
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
