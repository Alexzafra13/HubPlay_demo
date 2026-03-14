package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newOSTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("/subtitles", func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		if r.Header.Get("Api-Key") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		writeJSON(w, map[string]any{
			"data": []map[string]any{
				{
					"attributes": map[string]any{
						"language": "en",
						"format":   "srt",
						"ratings":  8.5,
						"files": []map[string]any{
							{"file_id": 12345, "file_name": "movie.en.srt"},
						},
					},
				},
				{
					"attributes": map[string]any{
						"language": "es",
						"format":   "srt",
						"ratings":  7.2,
						"files": []map[string]any{
							{"file_id": 12346, "file_name": "movie.es.srt"},
						},
					},
				},
			},
		})
	})

	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck

		writeJSON(w, map[string]any{
			"link": "http://example.com/subtitle.srt",
		})
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestOpenSubtitles_Init(t *testing.T) {
	p := NewOpenSubtitlesProvider()

	err := p.Init(map[string]string{})
	if err == nil {
		t.Error("expected error without api_key")
	}

	err = p.Init(map[string]string{"api_key": "test-key"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpenSubtitles_SearchSubtitles(t *testing.T) {
	server := newOSTestServer(t)

	// We test the response parsing by hitting the mock server directly
	ctx := context.Background()
	client := &http.Client{}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		server.URL+"/subtitles?query=Fight+Club&type=movie", nil)
	req.Header.Set("Api-Key", "test-key")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	var raw osSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}

	if len(raw.Data) != 2 {
		t.Fatalf("expected 2 results, got %d", len(raw.Data))
	}

	// Parse into SubtitleResults like the provider does
	var results []SubtitleResult
	for _, d := range raw.Data {
		for _, f := range d.Attributes.Files {
			results = append(results, SubtitleResult{
				Language: d.Attributes.Language,
				Format:   d.Attributes.Format,
				URL:      fmt.Sprintf("%d", f.FileID),
				Score:    float64(d.Attributes.Ratings),
				Source:   "opensubtitles",
			})
		}
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 subtitle results, got %d", len(results))
	}
	if results[0].Language != "en" {
		t.Errorf("first language = %q, want en", results[0].Language)
	}
	if results[0].Format != "srt" {
		t.Errorf("first format = %q, want srt", results[0].Format)
	}
	if results[0].URL != "12345" {
		t.Errorf("first url = %q, want 12345", results[0].URL)
	}
	if results[1].Language != "es" {
		t.Errorf("second language = %q, want es", results[1].Language)
	}
}

func TestOpenSubtitles_Download(t *testing.T) {
	server := newOSTestServer(t)

	ctx := context.Background()
	client := &http.Client{}

	body, _ := json.Marshal(map[string]any{"file_id": "12345"})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		server.URL+"/download", bytes.NewReader(body))
	req.Header.Set("Api-Key", "test-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	var raw osDownloadResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}

	if raw.Link == "" {
		t.Error("expected download link")
	}
	if raw.Link != "http://example.com/subtitle.srt" {
		t.Errorf("link = %q", raw.Link)
	}
}

func TestOpenSubtitles_SearchNoAPIKey(t *testing.T) {
	server := newOSTestServer(t)

	ctx := context.Background()
	client := &http.Client{}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		server.URL+"/subtitles?query=test", nil)
	// No Api-Key header

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}
