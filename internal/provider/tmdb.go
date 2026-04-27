package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	tmdbBaseURL  = "https://api.themoviedb.org/3"
	tmdbImageURL = "https://image.tmdb.org/t/p/"
)

// TMDbProvider implements MetadataProvider and ImageProvider using The Movie Database API.
type TMDbProvider struct {
	apiKey string
	client *http.Client
	lang   string // ISO 639-1 language code
}

// NewTMDbProvider creates a new TMDb provider with caching + backoff
// wired into its HTTP transport. Every TMDb response is keyed by URL
// and cached for 7 days; 429s and 5xxs are retried with backoff that
// honours `Retry-After`. Tests can still swap `client` for an
// httptest.Server client to bypass the cache.
func NewTMDbProvider() *TMDbProvider {
	return &TMDbProvider{
		client: newCachingClient(15*time.Second, 7*24*time.Hour),
		lang:   "en-US",
	}
}

func (t *TMDbProvider) Name() string { return "tmdb" }

func (t *TMDbProvider) Init(cfg map[string]string) error {
	t.apiKey = cfg["api_key"]
	if t.apiKey == "" {
		return fmt.Errorf("tmdb: api_key required")
	}
	if lang := cfg["language"]; lang != "" {
		t.lang = lang
	}
	return nil
}

// ──────────────────── MetadataProvider ────────────────────

func (t *TMDbProvider) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	endpoint := "/search/movie"
	if query.ItemType == ItemSeries {
		endpoint = "/search/tv"
	}

	params := url.Values{
		"query":    {query.Title},
		"language": {t.lang},
	}
	if query.Year > 0 {
		if query.ItemType == ItemSeries {
			params.Set("first_air_date_year", strconv.Itoa(query.Year))
		} else {
			params.Set("year", strconv.Itoa(query.Year))
		}
	}

	var raw tmdbSearchResponse
	if err := t.get(ctx, endpoint, params, &raw); err != nil {
		return nil, err
	}

	results := make([]SearchResult, 0, len(raw.Results))
	for _, r := range raw.Results {
		title := r.Title
		if title == "" {
			title = r.Name
		}
		year := extractYear(r.ReleaseDate)
		if year == 0 {
			year = extractYear(r.FirstAirDate)
		}

		results = append(results, SearchResult{
			ExternalID: strconv.Itoa(r.ID),
			Title:      title,
			Year:       year,
			Overview:   r.Overview,
			Score:      r.Popularity / 100, // normalize
		})
	}

	return results, nil
}

func (t *TMDbProvider) GetMetadata(ctx context.Context, externalID string, itemType ItemType) (*MetadataResult, error) {
	mediaType := "movie"
	if itemType == ItemSeries {
		mediaType = "tv"
	}

	params := url.Values{
		"language":           {t.lang},
		"append_to_response": {"credits,external_ids"},
	}

	var detail tmdbDetail
	if err := t.get(ctx, fmt.Sprintf("/%s/%s", mediaType, externalID), params, &detail); err != nil {
		return nil, err
	}

	result := &MetadataResult{
		Title:         coalesce(detail.Title, detail.Name),
		OriginalTitle: coalesce(detail.OriginalTitle, detail.OriginalName),
		Overview:      detail.Overview,
		Tagline:       detail.Tagline,
		ExternalIDs:   make(map[string]string),
	}

	result.ExternalIDs["tmdb"] = externalID
	if detail.ExternalIDs.IMDBID != "" {
		result.ExternalIDs["imdb"] = detail.ExternalIDs.IMDBID
	}
	if detail.ExternalIDs.TVDBID > 0 {
		result.ExternalIDs["tvdb"] = strconv.Itoa(detail.ExternalIDs.TVDBID)
	}

	// Year & premiere
	dateStr := coalesce(detail.ReleaseDate, detail.FirstAirDate)
	if t, err := time.Parse("2006-01-02", dateStr); err == nil {
		result.PremiereDate = &t
		result.Year = t.Year()
	}

	// Rating
	if detail.VoteAverage > 0 {
		r := detail.VoteAverage
		result.Rating = &r
	}

	// Content rating
	result.ContentRating = detail.ContentRating

	// Studio
	if len(detail.ProductionCompanies) > 0 {
		result.Studio = detail.ProductionCompanies[0].Name
	}
	if len(detail.Networks) > 0 && result.Studio == "" {
		result.Studio = detail.Networks[0].Name
	}

	// Genres
	for _, g := range detail.Genres {
		result.Genres = append(result.Genres, g.Name)
	}

	// People (cast + crew)
	if detail.Credits.Cast != nil {
		for i, c := range detail.Credits.Cast {
			if i >= 30 { // limit
				break
			}
			p := Person{
				Name:      c.Name,
				Role:      "actor",
				Character: c.Character,
				Order:     c.Order,
			}
			if c.ProfilePath != "" {
				p.ThumbURL = tmdbImageURL + "w185" + c.ProfilePath
			}
			result.People = append(result.People, p)
		}
	}
	if detail.Credits.Crew != nil {
		for _, c := range detail.Credits.Crew {
			if c.Job != "Director" && c.Job != "Writer" && c.Job != "Screenplay" {
				continue
			}
			role := strings.ToLower(c.Job)
			if role == "screenplay" {
				role = "writer"
			}
			p := Person{
				Name:  c.Name,
				Role:  role,
				Order: 1000, // after actors
			}
			if c.ProfilePath != "" {
				p.ThumbURL = tmdbImageURL + "w185" + c.ProfilePath
			}
			result.People = append(result.People, p)
		}
	}

	return result, nil
}

// ──────────────────── ImageProvider ────────────────────

func (t *TMDbProvider) GetImages(ctx context.Context, externalIDs map[string]string, itemType ItemType) ([]ImageResult, error) {
	tmdbID, ok := externalIDs["tmdb"]
	if !ok {
		return nil, nil
	}

	mediaType := "movie"
	if itemType == ItemSeries {
		mediaType = "tv"
	}

	// Pass include_image_language to get preferred language + English + null (no-text) images
	langCode := strings.Split(t.lang, "-")[0] // "en-US" → "en"
	params := url.Values{
		"include_image_language": {langCode + ",en,null"},
	}

	var raw tmdbImagesResponse
	if err := t.get(ctx, fmt.Sprintf("/%s/%s/images", mediaType, tmdbID), params, &raw); err != nil {
		return nil, err
	}

	var images []ImageResult

	for _, img := range raw.Posters {
		images = append(images, ImageResult{
			URL:      tmdbImageURL + "original" + img.FilePath,
			Type:     "primary",
			Language: img.Language,
			Width:    img.Width,
			Height:   img.Height,
			Score:    t.langScore(img, langCode),
		})
	}

	for _, img := range raw.Backdrops {
		images = append(images, ImageResult{
			URL:      tmdbImageURL + "original" + img.FilePath,
			Type:     "backdrop",
			Language: img.Language,
			Width:    img.Width,
			Height:   img.Height,
			Score:    t.langScore(img, langCode),
		})
	}

	for _, img := range raw.Logos {
		images = append(images, ImageResult{
			URL:      tmdbImageURL + "original" + img.FilePath,
			Type:     "logo",
			Language: img.Language,
			Width:    img.Width,
			Height:   img.Height,
			Score:    t.langScore(img, langCode),
		})
	}

	return images, nil
}

// langScore boosts images that match the preferred language so they sort first.
// Preferred language → +1000, language-neutral (no text) → +500, English fallback → +100.
func (t *TMDbProvider) langScore(img tmdbImage, langCode string) float64 {
	bonus := 0.0
	switch {
	case img.Language == langCode:
		bonus = 1000
	case img.Language == "" || img.Language == "xx":
		bonus = 500
	case img.Language == "en":
		bonus = 100
	}
	return img.VoteAverage + bonus
}

// ──────────────────── HTTP helpers ────────────────────

func (t *TMDbProvider) get(ctx context.Context, endpoint string, params url.Values, out any) error {
	params.Set("api_key", t.apiKey)
	reqURL := tmdbBaseURL + endpoint + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("tmdb request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("tmdb fetch: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("tmdb: rate limited")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("tmdb: status %d: %s", resp.StatusCode, string(body))
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("tmdb decode: %w", err)
	}
	return nil
}

// ──────────────────── TMDb API types ────────────────────

type tmdbSearchResponse struct {
	Results []struct {
		ID           int     `json:"id"`
		Title        string  `json:"title"`
		Name         string  `json:"name"`
		Overview     string  `json:"overview"`
		ReleaseDate  string  `json:"release_date"`
		FirstAirDate string  `json:"first_air_date"`
		Popularity   float64 `json:"popularity"`
	} `json:"results"`
}

type tmdbDetail struct {
	ID                  int     `json:"id"`
	Title               string  `json:"title"`
	Name                string  `json:"name"`
	OriginalTitle       string  `json:"original_title"`
	OriginalName        string  `json:"original_name"`
	Overview            string  `json:"overview"`
	Tagline             string  `json:"tagline"`
	ReleaseDate         string  `json:"release_date"`
	FirstAirDate        string  `json:"first_air_date"`
	VoteAverage         float64 `json:"vote_average"`
	ContentRating       string  `json:"certification"`
	Genres              []struct{ Name string `json:"name"` } `json:"genres"`
	ProductionCompanies []struct{ Name string `json:"name"` } `json:"production_companies"`
	Networks            []struct{ Name string `json:"name"` } `json:"networks"`
	Credits             struct {
		Cast []struct {
			Name        string `json:"name"`
			Character   string `json:"character"`
			ProfilePath string `json:"profile_path"`
			Order       int    `json:"order"`
		} `json:"cast"`
		Crew []struct {
			Name        string `json:"name"`
			Job         string `json:"job"`
			ProfilePath string `json:"profile_path"`
		} `json:"crew"`
	} `json:"credits"`
	ExternalIDs struct {
		IMDBID string `json:"imdb_id"`
		TVDBID int    `json:"tvdb_id"`
	} `json:"external_ids"`
}

type tmdbImagesResponse struct {
	Posters   []tmdbImage `json:"posters"`
	Backdrops []tmdbImage `json:"backdrops"`
	Logos     []tmdbImage `json:"logos"`
}

type tmdbImage struct {
	FilePath    string  `json:"file_path"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	VoteAverage float64 `json:"vote_average"`
	Language    string  `json:"iso_639_1"`
}

func extractYear(dateStr string) int {
	if len(dateStr) >= 4 {
		y, _ := strconv.Atoi(dateStr[:4])
		return y
	}
	return 0
}

func coalesce(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
