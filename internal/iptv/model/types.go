// Package model contiene los tipos del dominio IPTV (Channel,
// EPGProgram, etc.). Sub-paquete leaf para romper el ciclo:
//   iptv → db → iptv/model (leaf sin imports beyond stdlib).
package model

import "time"

// Channel es un canal IPTV linkable: row de `channels` con los
// metadatos de salud agregados. `LastProbeStatus` ∈ {"", "ok", "error"};
// `ConsecutiveFailures` reset a 0 en cada probe OK.
type Channel struct {
	ID                  string
	LibraryID           string
	Name                string
	Number              int
	GroupName           string
	LogoURL             string
	StreamURL           string
	TvgID               string
	Language            string
	Country             string
	IsActive            bool
	AddedAt             time.Time
	LastProbeAt         time.Time
	LastProbeStatus     string // "ok" | "error" | "" (never probed)
	LastProbeError      string
	ConsecutiveFailures int
}

// ChannelHealthSummary es el rollup agregado por library que el
// admin panel `/admin/libraries/{id}` muestra junto al gear-icon.
type ChannelHealthSummary struct {
	TotalChannels   int
	UnhealthyCount  int
	WithoutEPGCount int
}

// ChannelFavorite es la marca per-user de "canal favorito" — afecta
// al sort del rail LiveTV (favoritos primero) y al overlay de
// reordenamiento.
type ChannelFavorite struct {
	UserID    string
	ChannelID string
	CreatedAt time.Time
}

// ChannelOverride es el remapping admin-level de un canal: si la
// playlist M3U cambia un stream_url o tvg_id, el override mantiene la
// URL/tvg-id stable para que las favorites de los usuarios no se
// rompan. Clave (library_id, stream_url) — el override aplica a todos
// los canales que comparten una URL post-import.
type ChannelOverride struct {
	LibraryID string
	StreamURL string
	TvgID     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ChannelLogoOverride es el override del logo de un canal — URL externa
// pegada por el admin o archivo subido a local. Una de las dos está
// poblada, nunca ambas (el handler limpia la otra al escribir). Clave
// (library_id, stream_url) mismo patrón que ChannelOverride para que el
// override sobreviva el siguiente re-import del M3U.
type ChannelLogoOverride struct {
	LibraryID string
	StreamURL string
	// URL externa absoluta. Vacía cuando el override es un archivo subido.
	LogoURL string
	// Basename del archivo bajo <imageDir>/channel-logos/. Vacío cuando el
	// override es una URL externa.
	LogoFile  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// EPGProgram es un único programa del EPG con sus tiempos UTC y
// metadatos opcionales. `IconURL` vacío = sin icono específico (el
// frontend cae a un placeholder).
type EPGProgram struct {
	ID          string
	ChannelID   string
	Title       string
	Description string
	Category    string
	IconURL     string
	StartTime   time.Time
	EndTime     time.Time
}

// LibraryChannelOrderEntry es la entrada del overlay admin-level de
// orden de canales (migración 043). `Hidden = true` esconde el canal
// duramente — los users no pueden un-hide lo que el admin oculta.
type LibraryChannelOrderEntry struct {
	LibraryID string
	ChannelID string
	Position  int
	Hidden    bool
	UpdatedAt time.Time
}

// LibraryEPGSource es una fuente EPG enlazada a una library:
// catalog_id (iptv-org) o URL custom. Las dos fuentes nunca conviven
// en la misma row — `CatalogID == ""` significa URL custom.
type LibraryEPGSource struct {
	ID               string
	LibraryID        string
	CatalogID        string // empty = custom URL
	URL              string
	Priority         int
	LastRefreshedAt  time.Time
	LastStatus       string // "ok" | "error" | "" (never refreshed)
	LastError        string
	LastProgramCount int
	LastChannelCount int
	CreatedAt        time.Time
}

// UserChannelOrderEntry es la entrada del overlay per-user de orden
// de canales (migración 042). Aplica DESPUÉS del overlay admin
// (LibraryChannelOrder) — los users pueden ocultar más, nunca menos.
type UserChannelOrderEntry struct {
	UserID    string
	ChannelID string
	Position  int
	Hidden    bool
	UpdatedAt time.Time
}

// IPTVScheduledJob es la planificación de jobs recurrentes per-library
// (refresh M3U, refresh EPG, probe). `Kind` ∈ {"m3u_refresh",
// "epg_refresh", "probe"}; `IntervalHours` 0 → desactivado.
type IPTVScheduledJob struct {
	LibraryID      string
	Kind           string
	IntervalHours  int
	Enabled        bool
	LastRunAt      time.Time
	LastStatus     string // "ok" | "error" | "" (never run)
	LastError      string
	LastDurationMS int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
