package handlers

import (
	"hubplay/internal/db"
	"hubplay/internal/iptv"
)

// channelDTO is the wire shape for a channel returned by the IPTV API.
//
// It is the single source of truth for the frontend's channel model. New
// derived fields (category, logo_initials, logo_bg, logo_fg) are computed
// at request time from the raw M3U data in the DB. That placement is
// intentional — see comment on `Category` below.
type channelDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Number    int    `json:"number"`
	// Group and GroupName are both the raw M3U `group-title`. `group` is kept
	// as a historical alias for older frontend code; prefer `group_name`.
	Group     string `json:"group"`
	GroupName string `json:"group_name"`
	// Category is the canonical, UI-stable classification derived from
	// GroupName via iptv.Canonical. Computed per request so that keyword
	// table changes apply instantly to existing libraries without a rescan
	// or migration — the raw `group_name` is always preserved in the DB.
	Category string `json:"category"`

	// LogoURL is the upstream logo (may be empty, broken, or slow).
	LogoURL string `json:"logo_url"`
	// LogoInitials / LogoBg / LogoFg form a deterministic placeholder the
	// frontend can render immediately and use as an onError fallback. Always
	// populated, even when LogoURL is present, so error paths have no
	// layout shift.
	LogoInitials string `json:"logo_initials"`
	LogoBg       string `json:"logo_bg"`
	LogoFg       string `json:"logo_fg"`

	StreamURL string `json:"stream_url,omitempty"`
	LibraryID string `json:"library_id"`
	TvgID     string `json:"tvg_id"`
	Language  string `json:"language"`
	Country   string `json:"country"`
	IsActive  bool   `json:"is_active"`
	// AddedAt is when the channel first landed in the library. The
	// frontend sorts by it for the "recién añadidos" hero mode; the
	// wire shape is the raw RFC3339 string so JS can parse directly.
	AddedAt string `json:"added_at,omitempty"`
}

// toChannelDTO projects a db.Channel onto the wire shape. `streamPath` is
// injected rather than built inside because list and detail endpoints differ
// (list exposes the client-facing proxy URL; detail omits it and relies on a
// separate stream endpoint). Pass "" to omit `stream_url`.
//
// Accepts a pointer to match the service layer's return type (*db.Channel);
// callers never need to dereference.
func toChannelDTO(ch *db.Channel, streamPath string) channelDTO {
	if ch == nil {
		return channelDTO{}
	}
	logo := iptv.DeriveLogoFallback(ch.Name)
	var addedAt string
	if !ch.AddedAt.IsZero() {
		addedAt = ch.AddedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	return channelDTO{
		ID:           ch.ID,
		Name:         ch.Name,
		Number:       ch.Number,
		Group:        ch.GroupName,
		GroupName:    ch.GroupName,
		Category:     string(iptv.Canonical(ch.GroupName)),
		LogoURL:      ch.LogoURL,
		LogoInitials: logo.Initials,
		LogoBg:       logo.Background,
		LogoFg:       logo.Foreground,
		StreamURL:    streamPath,
		LibraryID:    ch.LibraryID,
		TvgID:        ch.TvgID,
		Language:     ch.Language,
		Country:      ch.Country,
		IsActive:     ch.IsActive,
		AddedAt:      addedAt,
	}
}
