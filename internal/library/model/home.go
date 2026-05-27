package model

import "time"

// HomeTrendingItem es una entrada del rail "Trending" del home.
type HomeTrendingItem struct {
	ID              string
	Type            string
	Title           string
	Year            *int
	CommunityRating *float64
	LibraryID       string
	PlayCount       int64
	LastPlayedAt    time.Time
	ContentRating   string
}

// HomeRecommendation es una entrada del rail "Recomendado para ti".
type HomeRecommendation struct {
	ID              string
	Type            string
	Title           string
	Year            *int
	CommunityRating *float64
	LibraryID       string
	Because         []string
	ContentRating   string
}

// HomeBecauseSeed es el item que origina el rail "Porque viste X".
type HomeBecauseSeed struct {
	ID        string
	Type      string
	Title     string
	Year      *int
	LibraryID string
}

// HomeBecauseResult agrupa seed + recomendaciones para el rail
// "Porque viste X".
type HomeBecauseResult struct {
	Seed  *HomeBecauseSeed
	Items []HomeRecommendation
}

// HomeLiveNowChannel es una entrada del rail "En vivo ahora".
type HomeLiveNowChannel struct {
	ChannelID    string
	ChannelName  string
	ChannelLogo  string
	LibraryID    string
	LibraryName  string
	ProgramTitle string
	ProgramStart *time.Time
	ProgramEnd   *time.Time
	ProgramIcon  string
}
