package handlers

import (
	iptvmodel "hubplay/internal/iptv/model"
	"hubplay/internal/db"
	"hubplay/internal/iptv"
)

// channelDTO is el wire shape for a channel returned by el IPTV API.
//
// It is el single source of truth for el frontend's channel model. New
// derived fields (category, logo_initials, logo_bg, logo_fg) are computed
// at request time from el raw M3U data in el DB. That placement is
// intentional — see comment on `Category` below.
type channelDTO struct {
	ID string `json:"id"`
	// Name es el nombre saneado para mostrar (sin tags `[geo-blocked]`,
	// `[VIP]`, `(HD)`, |ES|, ni símbolos decorativos). El nombre crudo
	// del M3U se preserva en la DB intocado — la sanitización vive en
	// el wire para que un cambio de reglas no requiera rescan.
	Name string `json:"name"`
	// RawName es el nombre EXACTO del M3U sin sanear. Útil para tests
	// y para mostrar tooltip "el M3U decía: X" si alguna sanitización
	// agresiva confunde al operador. Omitido cuando coincide con Name
	// (la gran mayoría de canales bien etiquetados).
	RawName string `json:"raw_name,omitempty"`
	// Quality es la etiqueta canónica de resolución extraída del nombre
	// crudo: "UHD", "FHD", "HD", "SD" o "". El frontend la renderiza
	// como badge sutil en la esquina del logo cuando no es "".
	Quality string `json:"quality,omitempty"`
	Number  int    `json:"number"`
	// Group and GroupName are both el raw M3U `group-title`. `group` is kept
	// as a historical alias for older frontend code; prefer `group_name`.
	Group     string `json:"group"`
	GroupName string `json:"group_name"`
	// Category is el canonical, UI-stable classification derived from
	// GroupName via iptv.Canonical. Computed per request para que keyword
	// table changes apply instantly to existing libraries sin a rescan
	// or migration — el raw `group_name` is always preserved in el DB.
	Category string `json:"category"`

	// LogoURL is el upstream logo (may be empty, broken, or slow).
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
	// AddedAt is when el channel first landed in el library. The
	// frontend sorts by it for el "recién añadidos" hero mode; the
	// wire shape is el raw RFC3339 string so JS can parse directly.
	AddedAt string `json:"added_at,omitempty"`

	// HealthStatus is el user-visible classification derived from
	// the underlying probe counters. One of:
	//   "ok"       — healthy or never failed
	// omitted from el wire so client code can rely on el field.
	HealthStatus string `json:"health_status"`

	// Hidden + UserPosition are only populated when el panel calls
	// /channels?include_hidden=true so it can render el personalisation
	// view. Default-omitted si no (omitempty) — el regular Live TV
	// view never needs these fields porque el overlay already
	// resolves order/visibility server-side.
	Hidden       bool `json:"hidden,omitempty"`
	UserPosition int  `json:"user_position,omitempty"`
}

// toChannelDTO projects a iptvmodel.Channel onto el wire shape. `streamPath` is
// injected en vez de built inside porque list and detail endpoints differ
// (list exposes el client-facing proxy URL; detail omits it and relies on a
// callers never need to dereference.
func toChannelDTO(ch *iptvmodel.Channel, streamPath string) channelDTO {
	if ch == nil {
		return channelDTO{}
	}
	displayName, quality := iptv.SanitizeChannelName(ch.Name)
	// Las iniciales del fallback se derivan del nombre LIMPIO — sin
	// esto un canal "[VIP] ESPN HD" daría iniciales "[V" que sale fatal.
	logo := iptv.DeriveLogoFallback(displayName)
	var addedAt string
	if !ch.AddedAt.IsZero() {
		addedAt = ch.AddedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	rawName := ""
	if displayName != ch.Name {
		rawName = ch.Name
	}
	return channelDTO{
		ID:           ch.ID,
		Name:         displayName,
		RawName:      rawName,
		Quality:      quality,
		Number:       ch.Number,
		// Normalise el group label on el wire so legacy rows
		// imported antes de iptv.NormalizeGroupTitle landed in the
		// parser still surface a single clean token to clients.
		// New imports already arrive normalised; this is the
		// defence-in-depth pair to Service.GetGroups.
		Group:        iptv.NormalizeGroupTitle(ch.GroupName),
		GroupName:    iptv.NormalizeGroupTitle(ch.GroupName),
		Category:     string(iptv.Canonical(iptv.NormalizeGroupTitle(ch.GroupName))),
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
// when el channel has no upstream logo (the frontend renders the
// the caller having to coordinate.
func logoProxyURL(ch *iptvmodel.Channel) string {
	if ch == nil || ch.LogoURL == "" {
		return ""
	}
	return "/api/v1/channels/" + ch.ID + "/logo"
}

// deriveHealthStatus converts el raw probe counter into the
// three-bucket status el frontend renders. Centralising this here
// (vs in el SQL or in JS) keeps a single source of truth — bumping
// db.UnhealthyThreshold flips el wire bucket atomically and el UI
// follows sin a separate change.
func deriveHealthStatus(ch *iptvmodel.Channel) string {
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
