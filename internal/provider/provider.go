// Package provider defines interfaces and a registry for metadata, image
// and subtitle providers (TMDb, Fanart.tv, OpenSubtitles, etc.).
package provider

import (
	"context"
	"time"
)

// ItemType enumerates the media types providers can handle.
type ItemType string

const (
	ItemMovie   ItemType = "movie"
	ItemSeries  ItemType = "series"
	ItemSeason  ItemType = "season"
	ItemEpisode ItemType = "episode"
)

// SearchQuery contains the information used to look up an item in external sources.
type SearchQuery struct {
	Title         string
	OriginalTitle string
	Year          int
	ItemType      ItemType
	// For episodes
	SeasonNumber  *int
	EpisodeNumber *int
	// Previously resolved external IDs (e.g. tmdb, imdb, tvdb)
	ExternalIDs map[string]string
}

// ──────────────────── Metadata ────────────────────

// MetadataResult is the data returned by a metadata provider.
type MetadataResult struct {
	Title         string
	OriginalTitle string
	Overview      string
	Tagline       string
	Year          int
	PremiereDate  *time.Time
	Rating        *float64    // community/average rating
	ContentRating string      // PG-13, TV-MA, etc.
	Studio        string
	Genres        []string
	Tags          []string
	People        []Person
	ExternalIDs   map[string]string // e.g. {"imdb": "tt1234567", "tmdb": "550"}
	// TrailerKey + TrailerSite identify the best-matched preview clip
	// for the item (typically a YouTube key from TMDb's `/videos`
	// endpoint). Empty when no provider returned a trailer — callers
	// treat absence as "no trailer available, leave the static
	// backdrop alone".
	TrailerKey  string
	TrailerSite string
}

// Person represents a cast/crew member.
type Person struct {
	Name      string
	Role      string // actor, director, writer
	Character string // character name (for actors)
	ThumbURL  string
	Order     int
}

// MetadataProvider can search and fetch metadata for media items.
type MetadataProvider interface {
	Provider
	// Search returns possible matches for the query.
	Search(ctx context.Context, query SearchQuery) ([]SearchResult, error)
	// GetMetadata fetches full metadata for a specific external ID.
	GetMetadata(ctx context.Context, externalID string, itemType ItemType) (*MetadataResult, error)
}

// EpisodeMetadataResult is the per-episode data returned by an episode-aware
// metadata provider. Distinct from MetadataResult because episodes have a
// "still" image (a frame from the episode) instead of poster/backdrop, plus
// an inherent (season, episode) tuple addressing inside their parent show.
type EpisodeMetadataResult struct {
	Title          string
	Overview       string
	PremiereDate   *time.Time
	Rating         *float64
	RuntimeMinutes int      // 0 when unknown
	StillURL       string   // optional — episode-specific frame
	GuestStars     []Person // capped to ~20 by the implementation
}

// EpisodeMetadataProvider is implemented by metadata providers that can fetch
// per-episode details given the show's external ID and a (season, episode)
// pair. Optional capability — providers that only know movies/series simply
// don't satisfy this interface and the manager skips them.
type EpisodeMetadataProvider interface {
	GetEpisodeMetadata(ctx context.Context, showExternalID string, seasonNumber, episodeNumber int) (*EpisodeMetadataResult, error)
}

// SeasonMetadataResult is the per-season data returned by a season-aware
// metadata provider. Lives next to EpisodeMetadataResult because the
// season is the natural unit between series (whole-show metadata) and
// episode (per-instalment frame + synopsis).
type SeasonMetadataResult struct {
	Title        string  // TMDb's friendly season name when set ("Season 1", "Specials", "The Final Chapter")
	Overview     string  // season-level synopsis
	PremiereDate *time.Time
	Rating       *float64 // aggregate audience rating for the season
	EpisodeCount int      // number of episodes TMDb knows about (informational)
	PosterURL    string   // optional — distinct per-season poster
}

// SeasonMetadataProvider is the season counterpart to
// EpisodeMetadataProvider. Optional capability: providers that don't
// implement it simply yield no season-level enrichment and the row
// keeps its scanner-derived "Season N" placeholder.
type SeasonMetadataProvider interface {
	GetSeasonMetadata(ctx context.Context, showExternalID string, seasonNumber int) (*SeasonMetadataResult, error)
}

// SearchResult is a single match from a metadata search.
type SearchResult struct {
	ExternalID string
	Title      string
	Year       int
	Overview   string
	Score      float64 // relevance 0-1
}

// ──────────────────── Images ────────────────────

// ImageResult represents a downloadable image.
type ImageResult struct {
	URL      string
	Type     string // primary, backdrop, logo, thumb, banner
	Language string
	Width    int
	Height   int
	Score    float64 // quality/relevance 0-1
	// Source provider name ("tmdb", "fanart", ...) — set by the
	// implementation that produced the result. The aggregator
	// (`Manager.FetchImages`) does not sort or merge by Source, but
	// the persistence layer (scanner, refresher) records it on
	// `db.Image.provider` so the admin UI can filter by source and
	// surface bad providers without sniffing the URL.
	Source string
}

// ImageProvider can fetch images for media items.
type ImageProvider interface {
	Provider
	// GetImages returns available images for the given external IDs.
	GetImages(ctx context.Context, externalIDs map[string]string, itemType ItemType) ([]ImageResult, error)
}

// ──────────────────── Subtitles ────────────────────

// SubtitleResult represents a downloadable subtitle file.
type SubtitleResult struct {
	Language string // ISO 639-1 (en, es, fr)
	Format   string // srt, ass, vtt
	URL      string
	Score    float64
	Source   string // provider name
}

// SubtitleProvider can search and download subtitles.
type SubtitleProvider interface {
	Provider
	// SearchSubtitles finds subtitles for a media item.
	SearchSubtitles(ctx context.Context, query SubtitleQuery) ([]SubtitleResult, error)
	// Download downloads a subtitle file and returns its content.
	Download(ctx context.Context, url string) ([]byte, error)
}

// SubtitleQuery describes what subtitles to look for.
type SubtitleQuery struct {
	Title         string
	Year          int
	ItemType      ItemType
	SeasonNumber  *int
	EpisodeNumber *int
	Languages     []string          // desired languages (ISO 639-1)
	ExternalIDs   map[string]string // imdb, tmdb, etc.
	FileHash      string            // file hash for exact matching
	FileSize      int64
}

// ──────────────────── Base ────────────────────

// Provider is the base interface all providers implement.
type Provider interface {
	// Name returns the unique provider name (e.g. "tmdb", "fanart", "opensubtitles").
	Name() string
	// Init is called once at startup with the provider's config.
	Init(cfg map[string]string) error
}
