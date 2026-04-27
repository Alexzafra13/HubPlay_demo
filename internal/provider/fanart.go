package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const fanartBaseURL = "https://webservice.fanart.tv/v3"

// FanartProvider implements ImageProvider using the Fanart.tv API.
// It provides high-quality artwork: logos, clearart, banners, thumbs, and backgrounds.
type FanartProvider struct {
	apiKey    string // project API key
	clientKey string // personal API key (optional, reduces delay)
	client    *http.Client
}

// NewFanartProvider creates a new Fanart.tv provider with caching +
// backoff wired into its HTTP transport. See NewTMDbProvider for the
// rationale; the Fanart contract is the same (free tier rate-limited,
// data rarely changes per item, scans hit the same IDs over and over
// across re-runs).
func NewFanartProvider() *FanartProvider {
	return &FanartProvider{
		client: newCachingClient(15*time.Second, 7*24*time.Hour),
	}
}

func (f *FanartProvider) Name() string { return "fanart" }

func (f *FanartProvider) Init(cfg map[string]string) error {
	f.apiKey = cfg["api_key"]
	if f.apiKey == "" {
		return fmt.Errorf("fanart: api_key required")
	}
	if ck := cfg["client_key"]; ck != "" {
		f.clientKey = ck
	}
	return nil
}

// GetImages fetches artwork from Fanart.tv for the given external IDs.
// For movies it uses the TMDb or IMDb ID; for TV shows it uses the TVDb ID.
func (f *FanartProvider) GetImages(ctx context.Context, externalIDs map[string]string, itemType ItemType) ([]ImageResult, error) {
	switch itemType {
	case ItemMovie:
		return f.getMovieImages(ctx, externalIDs)
	case ItemSeries:
		return f.getShowImages(ctx, externalIDs)
	default:
		return nil, nil
	}
}

func (f *FanartProvider) getMovieImages(ctx context.Context, externalIDs map[string]string) ([]ImageResult, error) {
	// Fanart.tv accepts TMDb or IMDb IDs for movies
	id := externalIDs["tmdb"]
	if id == "" {
		id = externalIDs["imdb"]
	}
	if id == "" {
		return nil, nil
	}

	var raw fanartMovieResponse
	if err := f.get(ctx, fmt.Sprintf("/movies/%s", id), &raw); err != nil {
		return nil, err
	}

	var images []ImageResult

	// HD logos (primary choice for hero banners)
	for _, img := range raw.HDMovieLogo {
		images = append(images, ImageResult{
			URL:      img.URL,
			Type:     "logo",
			Language: img.Lang,
			Width:    800,
			Height:   310,
			Score:    fanartScore(img, 2000), // logos are high priority from fanart
		})
	}

	// Standard logos (fallback)
	for _, img := range raw.MovieLogo {
		images = append(images, ImageResult{
			URL:      img.URL,
			Type:     "logo",
			Language: img.Lang,
			Width:    400,
			Height:   155,
			Score:    fanartScore(img, 1500),
		})
	}

	// Backgrounds/backdrops
	for _, img := range raw.MovieBackground {
		images = append(images, ImageResult{
			URL:      img.URL,
			Type:     "backdrop",
			Language: img.Lang,
			Width:    1920,
			Height:   1080,
			Score:    fanartScore(img, 100),
		})
	}

	// Posters
	for _, img := range raw.MoviePoster {
		images = append(images, ImageResult{
			URL:      img.URL,
			Type:     "primary",
			Language: img.Lang,
			Width:    1000,
			Height:   1426,
			Score:    fanartScore(img, 100),
		})
	}

	// HD clearart
	for _, img := range raw.HDMovieClearart {
		images = append(images, ImageResult{
			URL:      img.URL,
			Type:     "clearart",
			Language: img.Lang,
			Width:    1000,
			Height:   562,
			Score:    fanartScore(img, 100),
		})
	}

	// Banners
	for _, img := range raw.MovieBanner {
		images = append(images, ImageResult{
			URL:      img.URL,
			Type:     "banner",
			Language: img.Lang,
			Width:    1000,
			Height:   185,
			Score:    fanartScore(img, 100),
		})
	}

	// Thumbs
	for _, img := range raw.MovieThumb {
		images = append(images, ImageResult{
			URL:      img.URL,
			Type:     "thumb",
			Language: img.Lang,
			Width:    1000,
			Height:   562,
			Score:    fanartScore(img, 100),
		})
	}

	return images, nil
}

func (f *FanartProvider) getShowImages(ctx context.Context, externalIDs map[string]string) ([]ImageResult, error) {
	// Fanart.tv uses TVDb IDs for TV shows
	id := externalIDs["tvdb"]
	if id == "" {
		return nil, nil
	}

	var raw fanartTVResponse
	if err := f.get(ctx, fmt.Sprintf("/tv/%s", id), &raw); err != nil {
		return nil, err
	}

	var images []ImageResult

	// HD TV logos
	for _, img := range raw.HDTVLogo {
		images = append(images, ImageResult{
			URL:      img.URL,
			Type:     "logo",
			Language: img.Lang,
			Width:    800,
			Height:   310,
			Score:    fanartScore(img, 2000),
		})
	}

	// Standard clearlogos
	for _, img := range raw.ClearLogo {
		images = append(images, ImageResult{
			URL:      img.URL,
			Type:     "logo",
			Language: img.Lang,
			Width:    400,
			Height:   155,
			Score:    fanartScore(img, 1500),
		})
	}

	// Show backgrounds
	for _, img := range raw.ShowBackground {
		images = append(images, ImageResult{
			URL:      img.URL,
			Type:     "backdrop",
			Language: img.Lang,
			Width:    1920,
			Height:   1080,
			Score:    fanartScore(img, 100),
		})
	}

	// TV posters
	for _, img := range raw.TVPoster {
		images = append(images, ImageResult{
			URL:      img.URL,
			Type:     "primary",
			Language: img.Lang,
			Width:    1000,
			Height:   1426,
			Score:    fanartScore(img, 100),
		})
	}

	// HD clearart
	for _, img := range raw.HDClearart {
		images = append(images, ImageResult{
			URL:      img.URL,
			Type:     "clearart",
			Language: img.Lang,
			Width:    1000,
			Height:   562,
			Score:    fanartScore(img, 100),
		})
	}

	// Banners
	for _, img := range raw.TVBanner {
		images = append(images, ImageResult{
			URL:      img.URL,
			Type:     "banner",
			Language: img.Lang,
			Width:    1000,
			Height:   185,
			Score:    fanartScore(img, 100),
		})
	}

	// Thumbs
	for _, img := range raw.TVThumb {
		images = append(images, ImageResult{
			URL:      img.URL,
			Type:     "thumb",
			Language: img.Lang,
			Width:    1000,
			Height:   562,
			Score:    fanartScore(img, 100),
		})
	}

	return images, nil
}

// ──────────────────── HTTP helper ────────────────────

func (f *FanartProvider) get(ctx context.Context, endpoint string, out any) error {
	reqURL := fanartBaseURL + endpoint + "?api_key=" + f.apiKey
	if f.clientKey != "" {
		reqURL += "&client_key=" + f.clientKey
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("fanart request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return fmt.Errorf("fanart fetch: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusNotFound {
		return nil // item not found on fanart.tv, not an error
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("fanart: rate limited")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("fanart: status %d: %s", resp.StatusCode, string(body))
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("fanart decode: %w", err)
	}
	return nil
}

// ──────────────────── Scoring ────────────────────

// fanartScore computes a sort score from likes + a base bonus.
// English images get a small boost; language-neutral ("00") gets higher.
func fanartScore(img fanartImage, base float64) float64 {
	score := base + float64(img.Likes)
	switch img.Lang {
	case "en":
		score += 100
	case "00", "":
		score += 500 // language-neutral (no text) is versatile
	}
	return score
}

// ──────────────────── Fanart.tv API types ────────────────────

type fanartImage struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	Lang  string `json:"lang"`
	Likes int    `json:"likes,string"`
}

type fanartMovieResponse struct {
	Name             string        `json:"name"`
	TmdbID           string        `json:"tmdb_id"`
	ImdbID           string        `json:"imdb_id"`
	HDMovieLogo      []fanartImage `json:"hdmovielogo"`
	MovieLogo        []fanartImage `json:"movielogo"`
	MoviePoster      []fanartImage `json:"movieposter"`
	MovieBackground  []fanartImage `json:"moviebackground"`
	HDMovieClearart  []fanartImage `json:"hdmovieclearart"`
	MovieBanner      []fanartImage `json:"moviebanner"`
	MovieThumb       []fanartImage `json:"moviethumb"`
	MovieDisc        []fanartImage `json:"moviedisc"`
	MovieArt         []fanartImage `json:"movieart"`
}

type fanartTVResponse struct {
	Name           string        `json:"name"`
	TVDbID         string        `json:"thetvdb_id"`
	HDTVLogo       []fanartImage `json:"hdtvlogo"`
	ClearLogo      []fanartImage `json:"clearlogo"`
	TVPoster       []fanartImage `json:"tvposter"`
	ShowBackground []fanartImage `json:"showbackground"`
	HDClearart     []fanartImage `json:"hdclearart"`
	TVBanner       []fanartImage `json:"tvbanner"`
	TVThumb        []fanartImage `json:"tvthumb"`
	SeasonPoster   []fanartImage `json:"seasonposter"`
	CharacterArt   []fanartImage `json:"characterart"`
}
