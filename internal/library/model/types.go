// Package model contiene los tipos del dominio de library (Library, Item,
// MediaStream, Image, Chapter, etc.).
//
// Sub-paquete leaf para romper el ciclo de dependencias:
//   library → db (repos concretos)
//   db → library/model (tipos de retorno)
//   library → library/model (tipos propios)
// Al ser leaf (sin imports fuera de stdlib), el ciclo es imposible.
package model

import "time"

// ─── core: library + item + media stream ────────────────────────────

// Library: unidad de organización top-level del catálogo.
// M3UURL/EPGURL solo aplican cuando ContentType == "livetv".
type Library struct {
	ID              string
	Name            string
	ContentType     string // movies, shows, music, livetv
	ScanMode        string // auto, manual
	ScanInterval    string
	M3UURL          string
	EPGURL          string
	RefreshInterval string
	LanguageFilter  string
	TLSInsecure     bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Paths           []string
}

// Item: unidad atómica del catálogo. ParentID apunta a la temporada
// (episodios) o vacío (movies, series root).
type Item struct {
	ID              string
	LibraryID       string
	ParentID        string
	Type            string // movie, series, season, episode, audio, album, artist
	Title           string
	SortTitle       string
	OriginalTitle   string
	Year            int
	Path            string
	Size            int64
	DurationTicks   int64
	Container       string
	Fingerprint     string
	SeasonNumber    *int
	EpisodeNumber   *int
	CommunityRating *float64
	ContentRating   string
	PremiereDate    *time.Time
	AddedAt         time.Time
	UpdatedAt       time.Time
	IsAvailable     bool
}

// ItemFilter agrupa parámetros de búsqueda/filtrado/ordenación.
// Campos zero = sin restricción; AllowedContentRatings nil = sin cap (fail-open).
type ItemFilter struct {
	LibraryID string
	ParentID  string
	Type      string
	Query     string // FTS search
	Genre     string // case-insensitive, resuelto contra item_values
	YearFrom  int
	YearTo    int
	MinRating float64
	// AllowedContentRatings restringe a items cuyo content_rating esté en la lista.
	// Construido upstream via library.AllowedRatingsAtMost. nil = sin restricción.
	AllowedContentRatings []string
	Limit                 int
	Offset                int
	SortBy                string // sort_title, added_at, year
	SortOrder             string // asc, desc
	Cursor                string // keyset pagination
}

// LatestSeriesActivity: fila para el rail "series recientes". Incluye
// LatestActivityAt (max timestamp entre serie y descendientes) y
// NewEpisodesCount (episodios en ventana de 14 días).
type LatestSeriesActivity struct {
	Item
	LatestActivityAt time.Time
	NewEpisodesCount int
}

// MediaStream: pista (video, audio, subtítulo) dentro de un item.
// StreamIndex es offset 0-based del contenedor (para `-map 0:N` en ffmpeg).
type MediaStream struct {
	ItemID            string
	StreamIndex       int
	StreamType        string // video, audio, subtitle
	Codec             string
	Profile           string
	Bitrate           int
	Width             int
	Height            int
	FrameRate         float64
	HDRType           string
	ColorSpace        string
	Channels          int
	SampleRate        int
	Language          string
	Title             string
	IsDefault         bool
	IsForced          bool
	IsHearingImpaired bool
}

// ─── images ─────────────────────────────────────────────────────────

// Image: imagen asociada a un item (poster, backdrop, etc.).
type Image struct {
	ID        string
	ItemID    string
	Type      string // primary, backdrop, thumb, logo, banner
	Path      string
	Width     int
	Height    int
	Blurhash  string
	Provider  string
	IsPrimary bool
	// IsLocked protege una elección manual de ser sobrescrita por refreshes automáticos.
	IsLocked bool
	AddedAt  time.Time
	// DominantColor/DominantColorMuted: strings CSS rgb() pre-computados al ingestar.
	// Vacíos si la extracción falló — clients hacen fallback a runtime o color estático.
	DominantColor      string
	DominantColorMuted string
}

// PrimaryImageRef: "card de imagen" para rails — solo los campos que
// el frontend necesita para renderizar la poster principal.
type PrimaryImageRef struct {
	Path               string
	Blurhash           string
	DominantColor      string
	DominantColorMuted string
}

// ─── chapter ────────────────────────────────────────────────────────

// Chapter: punto marcado dentro de un item. StartTicks/EndTicks usan
// la codificación 10M-ticks-por-segundo del resto del schema.
type Chapter struct {
	ItemID     string
	StartTicks int64
	EndTicks   int64
	Title      string
	ImagePath  string
}

// ─── episode segments (skip intro / outro / recap) ──────────────────

// EpisodeSegmentKind: tipos reconocidos de segmento.
// Espejado como CHECK constraint en la DB (migración 037).
type EpisodeSegmentKind string

const (
	EpisodeSegmentIntro EpisodeSegmentKind = "intro"
	EpisodeSegmentOutro EpisodeSegmentKind = "outro"
	EpisodeSegmentRecap EpisodeSegmentKind = "recap"
)

// EpisodeSegmentSource: detector que produjo el segmento.
// 'fingerprint' reservado para audio-fingerprint; 'manual' para
// override admin futuro.
type EpisodeSegmentSource string

const (
	EpisodeSegmentSourceChapter     EpisodeSegmentSource = "chapter"
	EpisodeSegmentSourceFingerprint EpisodeSegmentSource = "fingerprint"
	EpisodeSegmentSourceManual      EpisodeSegmentSource = "manual"
)

// EpisodeSegment: rango detectado de intro/outro/recap.
// StartTicks/EndTicks en 10M-ticks/s. Confidence 0..1 — chapter-title
// usa 0.95 (quasi ground truth); detectores de waveform darán valores
// menores y el player decide si auto-mostrar el botón skip.
type EpisodeSegment struct {
	ItemID     string
	Kind       EpisodeSegmentKind
	Source     EpisodeSegmentSource
	StartTicks int64
	EndTicks   int64
	Confidence float64
	DetectedAt int64
}

// ─── studio ─────────────────────────────────────────────────────────

// Studio: productora/red (Lucasfilm, HBO, Disney+, …).
type Studio struct {
	ID      string
	TMDBID  *int64
	Name    string
	Slug    string
	LogoURL string
}

// StudioListEntry: rollup de listing — Studio + cuenta de items.
type StudioListEntry struct {
	ID        string
	Name      string
	Slug      string
	LogoURL   string
	ItemCount int64
}

// StudioItem: card de un item en la página de un studio.
type StudioItem struct {
	ID             string
	Type           string
	Title          string
	Year           int
	PrimaryImageID string
}

// ─── collection ─────────────────────────────────────────────────────

// Collection: saga/franquicia (MCU, Harry Potter, …).
// TMDBID permite re-import idempotente.
type Collection struct {
	ID          string
	TMDBID      int64
	Name        string
	Overview    string
	PosterURL   string
	BackdropURL string
}

// CollectionListEntry: rollup de listing — Collection + cuenta.
type CollectionListEntry struct {
	ID          string
	Name        string
	PosterURL   string
	BackdropURL string
	ItemCount   int64
}

// CollectionImageOverride: override de imagen (poster/backdrop) de una
// colección TMDb. URL o archivo, nunca ambos (CHECK en migración).
// Clave compuesta (collection_id, image_type).
type CollectionImageOverride struct {
	CollectionID string
	ImageType    string // "poster" | "backdrop"
	URL          string // vacía si override es archivo subido
	File         string // vacío si override es URL externa
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// CollectionItem: card de un item en la página de una collection.
type CollectionItem struct {
	ID             string
	Type           string
	Title          string
	Year           int
	PrimaryImageID string
}

// ─── external IDs ───────────────────────────────────────────────────

// ExternalID asocia un Item con un id de provider externo (TMDb, IMDb, TVDB, MusicBrainz).
type ExternalID struct {
	ItemID     string
	Provider   string
	ExternalID string
}

// ─── metadata ───────────────────────────────────────────────────────

// Metadata: blob enriquecido por providers (TMDb/Fanart) para un item.
// Genres y Tags se serializan como JSON arrays para evitar child table.
type Metadata struct {
	ItemID     string
	Overview   string
	Tagline    string
	Studio     string
	GenresJSON string
	TagsJSON   string
	// TrailerKey: id del trailer en la plataforma (ej. YouTube key de TMDb).
	// Vacío si no hay trailer. TrailerSite = nombre de plataforma para el embed URL.
	TrailerKey  string
	TrailerSite string
	// StudioLogoURL: URL absoluta del logo de la productora principal.
	// Construida desde TMDb production_companies[0].logo_path al escanear.
	StudioLogoURL string
	// CollectionID: FK a la saga (TMDb belongs_to_collection). Vacío = sin saga.
	CollectionID string
}

// ─── people ─────────────────────────────────────────────────────────

// Person: actor, director, escritor, etc.
type Person struct {
	ID        string
	Name      string
	Type      string
	ThumbPath string
}

// ItemPersonCredit: enlace many-to-many Person × Item con rol y character name.
type ItemPersonCredit struct {
	PersonID      string
	Name          string
	PersonType    string
	ThumbPath     string
	Role          string
	CharacterName string
	SortOrder     int
}

// FilmographyEntry: fila de filmografía de una persona.
type FilmographyEntry struct {
	ItemID         string
	Type           string
	Title          string
	Year           int
	Role           string
	CharacterName  string
	SortOrder      int
	PrimaryImageID string
}

// ─── item values (genres, tags, …) ──────────────────────────────────

// GenreCount: rollup "género + frecuencia" para admin panel y búsqueda facetada.
type GenreCount struct {
	Name  string
	Count int64
}
