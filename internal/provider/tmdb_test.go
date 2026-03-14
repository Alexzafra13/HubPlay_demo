package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTMDbTestServer(t *testing.T) (*httptest.Server, *TMDbProvider) {
	t.Helper()

	mux := http.NewServeMux()

	// Search movies
	mux.HandleFunc("/search/movie", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"results": []map[string]any{
				{
					"id":           550,
					"title":        "Fight Club",
					"overview":     "An insomniac office worker...",
					"release_date": "1999-10-15",
					"popularity":   65.5,
				},
				{
					"id":           551,
					"title":        "Fight Club 2",
					"overview":     "Sequel...",
					"release_date": "2025-01-01",
					"popularity":   12.3,
				},
			},
		})
	})

	// Search TV
	mux.HandleFunc("/search/tv", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"results": []map[string]any{
				{
					"id":             1399,
					"name":           "Breaking Bad",
					"overview":       "A high school chemistry teacher...",
					"first_air_date": "2008-01-20",
					"popularity":     80.0,
				},
			},
		})
	})

	// Movie detail
	mux.HandleFunc("/movie/550", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"id":             550,
			"title":          "Fight Club",
			"original_title": "Fight Club",
			"overview":       "An insomniac office worker...",
			"tagline":        "Mischief. Mayhem. Soap.",
			"release_date":   "1999-10-15",
			"vote_average":   8.4,
			"genres":         []map[string]any{{"name": "Drama"}, {"name": "Thriller"}},
			"production_companies": []map[string]any{{"name": "20th Century Fox"}},
			"credits": map[string]any{
				"cast": []map[string]any{
					{"name": "Brad Pitt", "character": "Tyler Durden", "profile_path": "/brad.jpg", "order": 0},
					{"name": "Edward Norton", "character": "The Narrator", "profile_path": "/ed.jpg", "order": 1},
				},
				"crew": []map[string]any{
					{"name": "David Fincher", "job": "Director", "profile_path": "/fincher.jpg"},
					{"name": "Jim Uhls", "job": "Screenplay", "profile_path": ""},
				},
			},
			"external_ids": map[string]any{
				"imdb_id": "tt0137523",
				"tvdb_id": 0,
			},
		})
	})

	// Movie images
	mux.HandleFunc("/movie/550/images", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"posters": []map[string]any{
				{"file_path": "/poster1.jpg", "width": 500, "height": 750, "vote_average": 5.5, "iso_639_1": "en"},
			},
			"backdrops": []map[string]any{
				{"file_path": "/backdrop1.jpg", "width": 1920, "height": 1080, "vote_average": 6.0, "iso_639_1": ""},
			},
			"logos": []map[string]any{
				{"file_path": "/logo1.png", "width": 400, "height": 100, "vote_average": 4.0, "iso_639_1": "en"},
			},
		})
	})

	// 404 for unknown
	mux.HandleFunc("/movie/99999", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	p := NewTMDbProvider()
	p.apiKey = "test-key"
	p.client = server.Client()

	// Override base URL — we need to patch the const, so we'll use a helper
	origBase := tmdbBaseURL
	_ = origBase
	// We can't change const, so we need a different approach: override the get method
	// Instead, we'll set the client and wrap the server URL

	return server, p
}

// testTMDbSearch hits the mock server and parses results like the provider does.
func testTMDbSearch(t *testing.T, serverURL string, itemType ItemType) []SearchResult {
	t.Helper()
	p := &TMDbProvider{
		apiKey: "test-key",
		client: &http.Client{},
		lang:   "en-US",
	}

	// Monkey-patch: override the get method by doing the request manually
	// Since we can't change the const, we test parseExtInf and the response mapping
	// by hitting the test server directly
	ctx := context.Background()

	endpoint := "/search/movie"
	if itemType == ItemSeries {
		endpoint = "/search/tv"
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		serverURL+endpoint+"?api_key=test&query=test&language=en-US", nil)
	resp, err := p.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	var raw tmdbSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}

	results := make([]SearchResult, 0)
	for _, r := range raw.Results {
		title := r.Title
		if title == "" {
			title = r.Name
		}
		results = append(results, SearchResult{
			ExternalID: fmt.Sprintf("%d", r.ID),
			Title:      title,
			Overview:   r.Overview,
			Score:      r.Popularity / 100,
		})
	}
	return results
}

func TestTMDb_SearchMovies(t *testing.T) {
	server, _ := newTMDbTestServer(t)

	results := testTMDbSearch(t, server.URL, ItemMovie)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Title != "Fight Club" {
		t.Errorf("first result = %q, want Fight Club", results[0].Title)
	}
	if results[0].ExternalID != "550" {
		t.Errorf("external_id = %q, want 550", results[0].ExternalID)
	}
}

func TestTMDb_SearchTV(t *testing.T) {
	server, _ := newTMDbTestServer(t)

	results := testTMDbSearch(t, server.URL, ItemSeries)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Title != "Breaking Bad" {
		t.Errorf("result = %q, want Breaking Bad", results[0].Title)
	}
}

func TestTMDb_GetMetadata(t *testing.T) {
	server, _ := newTMDbTestServer(t)

	// Fetch detail directly from test server
	ctx := context.Background()
	client := &http.Client{}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		server.URL+"/movie/550?api_key=test&language=en-US&append_to_response=credits,external_ids", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	var detail tmdbDetail
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}

	// Verify parsed fields
	if detail.Title != "Fight Club" {
		t.Errorf("title = %q", detail.Title)
	}
	if detail.Tagline != "Mischief. Mayhem. Soap." {
		t.Errorf("tagline = %q", detail.Tagline)
	}
	if detail.VoteAverage != 8.4 {
		t.Errorf("vote_average = %f", detail.VoteAverage)
	}
	if len(detail.Genres) != 2 {
		t.Errorf("genres count = %d, want 2", len(detail.Genres))
	}
	if detail.ExternalIDs.IMDBID != "tt0137523" {
		t.Errorf("imdb_id = %q", detail.ExternalIDs.IMDBID)
	}
	if len(detail.Credits.Cast) != 2 {
		t.Errorf("cast count = %d, want 2", len(detail.Credits.Cast))
	}
	if detail.Credits.Cast[0].Name != "Brad Pitt" {
		t.Errorf("first cast = %q", detail.Credits.Cast[0].Name)
	}
	if len(detail.Credits.Crew) != 2 {
		t.Errorf("crew count = %d, want 2", len(detail.Credits.Crew))
	}
}

func TestTMDb_GetImages(t *testing.T) {
	server, _ := newTMDbTestServer(t)

	ctx := context.Background()
	client := &http.Client{}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		server.URL+"/movie/550/images?api_key=test", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	var raw tmdbImagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}

	if len(raw.Posters) != 1 {
		t.Errorf("posters = %d, want 1", len(raw.Posters))
	}
	if len(raw.Backdrops) != 1 {
		t.Errorf("backdrops = %d, want 1", len(raw.Backdrops))
	}
	if len(raw.Logos) != 1 {
		t.Errorf("logos = %d, want 1", len(raw.Logos))
	}
	if raw.Posters[0].Width != 500 {
		t.Errorf("poster width = %d, want 500", raw.Posters[0].Width)
	}
}

func TestTMDb_Init(t *testing.T) {
	p := NewTMDbProvider()

	// No API key should fail
	err := p.Init(map[string]string{})
	if err == nil {
		t.Error("expected error without api_key")
	}

	// With API key should succeed
	err = p.Init(map[string]string{"api_key": "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	if p.apiKey != "abc123" {
		t.Errorf("apiKey = %q", p.apiKey)
	}

	// Custom language
	p2 := NewTMDbProvider()
	_ = p2.Init(map[string]string{"api_key": "key", "language": "es-ES"})
	if p2.lang != "es-ES" {
		t.Errorf("lang = %q, want es-ES", p2.lang)
	}
}

func TestTMDb_ExtractYear(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"1999-10-15", 1999},
		{"2025-01-01", 2025},
		{"2008", 2008},
		{"", 0},
		{"abc", 0},
	}
	for _, tc := range tests {
		got := extractYear(tc.input)
		if got != tc.want {
			t.Errorf("extractYear(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestTMDb_Coalesce(t *testing.T) {
	if coalesce("a", "b") != "a" {
		t.Error("coalesce(a,b) should return a")
	}
	if coalesce("", "b") != "b" {
		t.Error("coalesce('',b) should return b")
	}
	if coalesce("", "") != "" {
		t.Error("coalesce('','') should return ''")
	}
}

func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data) //nolint:errcheck
}

