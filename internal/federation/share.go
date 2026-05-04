package federation

import (
	"time"
)

// LibraryShare is an admin's deliberate opt-in: "this peer can see
// THIS library with THESE scopes". The presence of a row says "yes
// you can see it"; the boolean columns refine what they can do.
//
// A peer with no row for a given library_id sees that library as if
// it doesn't exist (404 on direct lookup, absent from list responses)
// — we deliberately don't return 403 because that confirms the
// library's existence to a non-authorised peer.
type LibraryShare struct {
	ID           string
	PeerID       string
	LibraryID    string
	CanBrowse    bool
	CanPlay      bool
	CanDownload  bool
	CanLiveTV    bool
	ExtraScopes  string // JSON, may be empty
	CreatedByUserID string
	CreatedAt    time.Time
}

// ShareScopes is a small input struct for admin endpoints — accepts
// scope flags from JSON without exposing the full LibraryShare row.
type ShareScopes struct {
	CanBrowse   bool `json:"can_browse"`
	CanPlay     bool `json:"can_play"`
	CanDownload bool `json:"can_download"`
	CanLiveTV   bool `json:"can_livetv"`
}

// DefaultShareScopes — the admin's most likely intent when sharing a
// library: browse + play allowed (the obvious "share my movies"
// case), download + livetv off (these consume our resources directly,
// so they need explicit opt-in).
func DefaultShareScopes() ShareScopes {
	return ShareScopes{CanBrowse: true, CanPlay: true}
}

// SharedLibrary is what a peer sees in their /peer/libraries response.
// Subset of the local Library struct — only the metadata the peer
// needs to navigate the catalog. We deliberately DON'T leak fields
// like m3u_url, scan_mode, refresh_interval — those are internal
// operator concerns.
type SharedLibrary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Scopes      ShareScopes `json:"scopes"`
}

// SharedItem is the subset of an Item exposed to a peer browsing the
// catalog. Mirrors what the local PosterCard renders — just enough
// to identify the title and decide whether to play / download / etc.
//
// HasPoster signals to the requesting peer that it can fetch a poster
// from `GET /api/v1/peer/items/{id}/poster`. We deliberately don't
// expose the underlying image's URL or path: the bytes flow through
// the federation poster endpoint so the origin can re-check the
// CanBrowse scope on every fetch and audit the access. A peer that
// catalog-cached us last week and lost their share since then will
// get a 404 on poster fetch — same gate as the catalog itself.
type SharedItem struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Year      int    `json:"year,omitempty"`
	Overview  string `json:"overview,omitempty"`
	HasPoster bool   `json:"has_poster,omitempty"`
}

// CachedItem extends SharedItem with the peer + library it belongs
// to and a cached_at timestamp. Stored in federation_item_cache for
// offline-friendly browsing.
type CachedItem struct {
	PeerID    string
	LibraryID string
	Item      SharedItem
	CachedAt  time.Time
}
