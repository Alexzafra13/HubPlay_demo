package model

import "time"

// UserData es el estado per-(usuario, item): progreso, favorito,
// preferencias de audio/subtítulo. Extraído de db/ para que los
// handlers no importen la capa de persistencia (PP).
type UserData struct {
	UserID              string
	ItemID              string
	PositionTicks       int64
	PlayCount           int
	Completed           bool
	IsFavorite          bool
	Liked               *bool
	AudioStreamIndex    *int
	SubtitleStreamIndex *int
	LastPlayedAt        *time.Time
	UpdatedAt           time.Time
}

// ContinueWatchingItem es una entrada del rail "Sigue viendo".
type ContinueWatchingItem struct {
	ItemID        string
	PositionTicks int64
	LastPlayedAt  *time.Time
	Title         string
	Type          string
	DurationTicks int64
	ParentID      string
	Container     string
	SeasonNumber  *int
	EpisodeNumber *int
	SeriesTitle   string
	SeriesID      string
}

// FavoriteItem es una entrada del listado de favoritos.
type FavoriteItem struct {
	ItemID        string
	FavoritedAt   time.Time
	Title         string
	Type          string
	Year          int
	DurationTicks int64
}

// NextUpItem es el siguiente episodio sin ver de una serie en progreso.
type NextUpItem struct {
	EpisodeID     string
	EpisodeTitle  string
	SeasonNumber  *int
	EpisodeNumber *int
	DurationTicks int64
	SeriesTitle   string
	SeriesID      string
}

// UserPreference es una preferencia clave/valor per-usuario (layout
// del home, idioma preferido, etc.).
type UserPreference struct {
	UserID    string
	Key       string
	Value     string
	UpdatedAt time.Time
}
