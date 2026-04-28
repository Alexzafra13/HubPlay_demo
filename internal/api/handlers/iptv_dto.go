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

	// HealthStatus is the user-visible classification derived from
	// the underlying probe counters. One of:
	//   "ok"       — healthy or never failed
	//   "degraded" — 1..N-1 consecutive failures (still surfaced)
	//   "dead"     — >= UnhealthyThreshold consecutive failures
	// The frontend uses this to drive the "Sin señal" category and
	// the small status pill on a channel tile. Always present; never
	// omitted from the wire so client code can rely on the field.
	HealthStatus string `json:"health_status"`
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
		LogoURL:      logoProxyURL(ch),
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
		HealthStatus: deriveHealthStatus(ch),
	}
}

// logoProxyURL maps a channel's stored upstream logo URL to the
// same-origin endpoint that fetches + caches + serves it. Empty
// when the channel has no upstream logo (the frontend renders the
// initials/colour fallback).
//
// Always returning the proxy URL — instead of the upstream — is what
// keeps a strict img-src CSP enforceable: the browser only ever
// loads images from `self`. The endpoint itself returns 404 when
// the upstream is unfetchable, and the React `onError` handler
// flips back to initials, so the UI degrades gracefully without
// the caller having to coordinate.
func logoProxyURL(ch *db.Channel) string {
	if ch == nil || ch.LogoURL == "" {
		return ""
	}
	return "/api/v1/channels/" + ch.ID + "/logo"
}

// deriveHealthStatus converts the raw probe counter into the
// three-bucket status the frontend renders. Centralising this here
// (vs in the SQL or in JS) keeps a single source of truth — bumping
// db.UnhealthyThreshold flips the wire bucket atomically and the UI
// follows without a separate change.
func deriveHealthStatus(ch *db.Channel) string {
	if ch == nil {
		return "ok"
	}
	switch {
	case ch.ConsecutiveFailures >= db.UnhealthyThreshold:
		return "dead"
	case ch.ConsecutiveFailures > 0:
		return "degraded"
	default:
		return "ok"
	}
}
