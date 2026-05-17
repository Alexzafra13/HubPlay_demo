// Package model contains the library-domain types (Library, Item,
// MediaStream, Image, Chapter, EpisodeSegment, Studio, Collection,
// ExternalID, Metadata, Person, ...) the rest of the codebase
// consumes.
//
// Vive en su propio sub-paquete leaf — en lugar de en `internal/library/`
// directo — para romper el ciclo de dependencias:
//
//   internal/library         imports internal/db            (for repo concretes)
//   internal/db              imports internal/library/model (for return types)
//   internal/library         imports internal/library/model (for types it also uses)
//
// `library/model` es un leaf (sin imports más allá de stdlib) →
// ciclo imposible. Tercer y último sub-bloque de Iteración 3 del
// plan de intervención post-auditoría 2026-05-14; los sub-bloques
// auth y iptv ya están cerrados (commits ac60ba0 y 4c686d0).
//
// Cierra "Opción B" del olor A para el feature library, el más grande:
// los tipos del dominio viven en el feature, no en `internal/db/`.
package model

import "time"

// ─── core: library + item + media stream ────────────────────────────

// Library es la unidad de organización top-level del catálogo: un
// directorio (o conjunto de paths) escaneado periódicamente que
// produce items de un único content_type. M3UURL / EPGURL solo
// aplican cuando ContentType == "livetv".
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
	Paths           []string // populated by GetByID/List
}

// Item es la unidad atómica del catálogo: película, serie, temporada,
// episodio, audio, álbum o artista. Para episodios, `ParentID` apunta
// a la temporada (que a su vez apunta a la serie). Para movies, vacío.
type Item struct {
	ID              string
	LibraryID       string
	ParentID        string // empty if root item (movie, series)
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

// ItemFilter agrupa los parámetros de búsqueda + filtrado + ordenación
// que el handler list de items pasa al repo. Optional fields zero =
// sin restricción; AllowedContentRatings nil = sin cap (caller sin
// profile o cap unknown — fail-open).
type ItemFilter struct {
	LibraryID string
	ParentID  string // filter by parent (e.g., episodes of a season)
	Type      string // filter by type
	Query     string // FTS search
	Genre     string // genre name (case-insensitive); resolved against item_values
	YearFrom  int    // inclusive lower year bound; 0 disables
	YearTo    int    // inclusive upper year bound; 0 disables
	MinRating float64 // inclusive lower community_rating bound; 0 disables
	// AllowedContentRatings, when non-nil, restricts the result set
	// to items whose `content_rating` matches one of the values.
	// Built upstream from the caller's profile via
	// library.AllowedRatingsAtMost(profile.max_content_rating). nil
	// = no restriction (caller has no cap or unknown cap → fail-open).
	AllowedContentRatings []string
	Limit                 int
	Offset                int
	SortBy                string // sort_title, added_at, year
	SortOrder             string // asc, desc
	Cursor                string // cursor for keyset pagination (item ID after which to fetch)
}

// LatestSeriesActivity is the rail-ready row for the "series
// recientemente activas" home rail: an Item plus a derived
// LatestActivityAt that climbs into season + episode descendants so
// shows with a fresh weekly episode sort above older static ones.
type LatestSeriesActivity struct {
	Item
	// LatestActivityAt is the most recent timestamp from either the
	// series row's own added_at or any of its descendants' (seasons /
	// episodes). Drives the rail order so a show that just received a
	// new weekly episode sorts above a show added six months ago that
	// hasn't moved since.
	LatestActivityAt time.Time
	// NewEpisodesCount is how many episodes under this series were
	// added inside the rolling 14-day window. Zero for shows that
	// haven't received anything in two weeks. The frontend only
	// renders the "+N nuevos" badge when this is > 0, so the field
	// is optional from the wire perspective.
	NewEpisodesCount int
}

// MediaStream es una pista (video, audio, subtítulo) dentro de un
// item. `StreamIndex` es el offset 0-based dentro del contenedor —
// el player lo usa para mapear directamente con `-map 0:N` en ffmpeg.
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

// Image es una imagen asociada a un item (poster, backdrop, etc.).
// `IsLocked = true` la protege de refreshes automáticos.
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
	// IsLocked guards a manual choice (admin uploaded a custom poster
	// or picked a specific candidate) from being overwritten by a
	// scheduled or scanner-triggered refresh. Plex and Jellyfin both
	// expose this as "lock". Default is false — refreshes work as
	// before until the admin explicitly locks something.
	IsLocked bool
	AddedAt  time.Time
	// DominantColor / DominantColorMuted are pre-computed CSS rgb()
	// strings extracted at ingest time. Empty when extraction failed
	// (non-decodable formats, undecidable palette) — clients fall back
	// to runtime extraction or a static colour in that case.
	DominantColor      string
	DominantColorMuted string
}

// PrimaryImageRef es el "card de imagen" usado por los rails: solo
// los campos que el frontend necesita para renderizar la poster
// principal sin un round-trip extra al endpoint de imágenes.
type PrimaryImageRef struct {
	Path               string
	Blurhash           string
	DominantColor      string
	DominantColorMuted string
}

// ─── chapter ────────────────────────────────────────────────────────

// Chapter es un punto marcado dentro de un item (intro, opening, …).
// `StartTicks`/`EndTicks` usan la encoding 10M-ticks-per-second que
// el resto del schema habla (duration_ticks, etc.).
type Chapter struct {
	ItemID     string
	StartTicks int64
	EndTicks   int64
	Title      string
	ImagePath  string
}

// ─── episode segments (skip intro / outro / recap) ──────────────────

// EpisodeSegmentKind enumerates the recognised segment types.
// Mirrored as a CHECK constraint at the DB layer (migración 037)
// so unknown values never make it past Replace().
type EpisodeSegmentKind string

const (
	EpisodeSegmentIntro EpisodeSegmentKind = "intro"
	EpisodeSegmentOutro EpisodeSegmentKind = "outro"
	EpisodeSegmentRecap EpisodeSegmentKind = "recap"
)

// EpisodeSegmentSource is the detector that produced the segment.
// 'chapter' is the only one wired today; 'fingerprint' is reserved
// for the audio-fingerprint detector and 'manual' for an admin
// override path that doesn't exist yet but will share this storage.
type EpisodeSegmentSource string

const (
	EpisodeSegmentSourceChapter     EpisodeSegmentSource = "chapter"
	EpisodeSegmentSourceFingerprint EpisodeSegmentSource = "fingerprint"
	EpisodeSegmentSourceManual      EpisodeSegmentSource = "manual"
)

// EpisodeSegment is one detected intro / outro / recap range.
//
// StartTicks and EndTicks use the same 10M-ticks-per-second encoding
// the rest of the schema speaks (chapters, items.duration_ticks).
// Confidence is 0..1 — chapter-title matches use 0.95 because a
// chapter literally titled "Intro" is essentially ground truth, but
// detectors that infer from waveform similarity will surface lower
// numbers and the player can decide whether to auto-show or hide
// the skip button accordingly.
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

// Studio es una productora / red (Lucasfilm, HBO, Disney+, …).
// `TMDBID` opcional permite re-import / dedup. `Slug` es la
// URL-friendly version del Name.
type Studio struct {
	ID      string
	TMDBID  *int64
	Name    string
	Slug    string
	LogoURL string
}

// StudioListEntry es el rollup de listing — Studio + cuenta de items.
type StudioListEntry struct {
	ID        string
	Name      string
	Slug      string
	LogoURL   string
	ItemCount int64
}

// StudioItem es el "card" de un item dentro de la página de un studio.
type StudioItem struct {
	ID             string
	Type           string
	Title          string
	Year           int
	PrimaryImageID string
}

// ─── collection ─────────────────────────────────────────────────────

// Collection es una saga / franquicia (Marvel Cinematic Universe,
// Harry Potter, …). `TMDBID` viene de provider y permite re-import
// idempotente.
type Collection struct {
	ID          string
	TMDBID      int64
	Name        string
	Overview    string
	PosterURL   string
	BackdropURL string
}

// CollectionListEntry es el rollup de listing — Collection + cuenta.
type CollectionListEntry struct {
	ID          string
	Name        string
	PosterURL   string
	BackdropURL string
	ItemCount   int64
}

// CollectionItem es el "card" de un item dentro de la página de una
// collection.
type CollectionItem struct {
	ID             string
	Type           string
	Title          string
	Year           int
	PrimaryImageID string
}

// ─── external IDs ───────────────────────────────────────────────────

// ExternalID asocia un Item con un id de provider externo (TMDb,
// IMDb, TVDB, MusicBrainz). Permite resolver duplicados y enrich
// idempotente.
type ExternalID struct {
	ItemID     string
	Provider   string
	ExternalID string
}

// ─── metadata ───────────────────────────────────────────────────────

// Metadata es el blob enriquecido por providers (TMDb / Fanart) para
// un item: descripción larga, genres, tags, trailer key y collection
// FK. Genres y Tags se serializan como JSON arrays para evitar un
// child table cuando solo se leen.
type Metadata struct {
	ItemID     string
	Overview   string
	Tagline    string
	Studio     string
	GenresJSON string
	TagsJSON   string
	// TrailerKey is the platform-specific id of the best-matched
	// trailer/teaser at scan time (typically a YouTube key from TMDb).
	// Empty when no trailer was returned for the item — the
	// SeriesHero treats absence as "no preview, just show the
	// backdrop". TrailerSite is the platform name ("YouTube",
	// "Vimeo") so the frontend picks the right embed URL.
	TrailerKey  string
	TrailerSite string
	// StudioLogoURL is the absolute image URL of the headline
	// production company / network logo (Lucasfilm, HBO, Disney+, …).
	// Built from TMDb's `production_companies[0].logo_path` at scan
	// time using the configured image base, so the frontend renders
	// it with a single `<img src>` and falls back to the studio text
	// when empty (older studios with no TMDb logo, or failed match).
	StudioLogoURL string
	// CollectionID is the FK to the saga (TMDb belongs_to_collection)
	// this movie belongs to — populated at scan time when the
	// provider returned one. Empty / NULL means "no saga"; the
	// detail page renders the optional "Part of: X" link only when
	// non-empty.
	CollectionID string
}

// ─── people ─────────────────────────────────────────────────────────

// Person es un actor, director, escritor, etc. `Type` discrimina el
// rol global; el rol específico en un item vive en ItemPersonCredit.
type Person struct {
	ID        string
	Name      string
	Type      string
	ThumbPath string
}

// ItemPersonCredit es el enlace many-to-many Person × Item con el
// rol específico (e.g., "Actor", "Director") y el character name
// (e.g., "Walter White") cuando aplica.
type ItemPersonCredit struct {
	PersonID      string
	Name          string
	PersonType    string
	ThumbPath     string
	Role          string
	CharacterName string
	SortOrder     int
}

// FilmographyEntry es una fila de la página "filmografía de X actor":
// muestra los items en los que la person está acreditada.
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

// GenreCount es el rollup admin "géneros del catálogo con su
// frecuencia", usado por el panel de admin para mostrar la nube de
// géneros y por el endpoint de búsqueda facetada.
type GenreCount struct {
	Name  string
	Count int64
}
