package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const osBaseURL = "https://api.opensubtitles.com/api/v1"

// OpenSubtitlesProvider implements SubtitleProvider using the OpenSubtitles REST API.
type OpenSubtitlesProvider struct {
	apiKey string
	client *http.Client
}

// NewOpenSubtitlesProvider creates a new OpenSubtitles provider.
func NewOpenSubtitlesProvider() *OpenSubtitlesProvider {
	return &OpenSubtitlesProvider{
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (o *OpenSubtitlesProvider) Name() string { return "opensubtitles" }

func (o *OpenSubtitlesProvider) Init(cfg map[string]string) error {
	o.apiKey = cfg["api_key"]
	if o.apiKey == "" {
		return fmt.Errorf("opensubtitles: api_key required")
	}
	return nil
}

func (o *OpenSubtitlesProvider) SearchSubtitles(ctx context.Context, query SubtitleQuery) ([]SubtitleResult, error) {
	params := url.Values{}

	if imdb, ok := query.ExternalIDs["imdb"]; ok {
		// Remove "tt" prefix if present
		params.Set("imdb_id", strings.TrimPrefix(imdb, "tt"))
	} else {
		params.Set("query", query.Title)
		if query.Year > 0 {
			params.Set("year", fmt.Sprintf("%d", query.Year))
		}
	}

	if query.SeasonNumber != nil {
		params.Set("season_number", fmt.Sprintf("%d", *query.SeasonNumber))
	}
	if query.EpisodeNumber != nil {
		params.Set("episode_number", fmt.Sprintf("%d", *query.EpisodeNumber))
	}

	if len(query.Languages) > 0 {
		params.Set("languages", strings.Join(query.Languages, ","))
	}

	switch query.ItemType {
	case ItemMovie:
		params.Set("type", "movie")
	case ItemEpisode:
		params.Set("type", "episode")
	}

	var raw osSearchResponse
	if err := o.request(ctx, http.MethodGet, "/subtitles?"+params.Encode(), nil, &raw); err != nil {
		return nil, err
	}

	results := make([]SubtitleResult, 0, len(raw.Data))
	for _, d := range raw.Data {
		for _, f := range d.Attributes.Files {
			results = append(results, SubtitleResult{
				Language: d.Attributes.Language,
				Format:   d.Attributes.Format,
				URL:      fmt.Sprintf("%d", f.FileID), // fileID used for download
				Score:    float64(d.Attributes.Ratings),
				Source:   "opensubtitles",
			})
		}
	}

	return results, nil
}

func (o *OpenSubtitlesProvider) Download(ctx context.Context, fileID string) ([]byte, error) {
	body := map[string]any{"file_id": fileID}
	bodyJSON, _ := json.Marshal(body)

	var raw osDownloadResponse
	if err := o.request(ctx, http.MethodPost, "/download", bodyJSON, &raw); err != nil {
		return nil, err
	}

	if raw.Link == "" {
		return nil, fmt.Errorf("opensubtitles: no download link returned")
	}

	// Download the actual subtitle file
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw.Link, nil)
	if err != nil {
		return nil, fmt.Errorf("create download request: %w", err)
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download subtitle: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download subtitle: status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read subtitle: %w", err)
	}

	return data, nil
}

func (o *OpenSubtitlesProvider) request(ctx context.Context, method, path string, body []byte, out any) error {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, osBaseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("opensubtitles request: %w", err)
	}
	req.Header.Set("Api-Key", o.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "HubPlay v1.0")

	resp, err := o.client.Do(req)
	if err != nil {
		return fmt.Errorf("opensubtitles fetch: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("opensubtitles: rate limited")
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("opensubtitles: status %d: %s", resp.StatusCode, string(respBody))
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("opensubtitles decode: %w", err)
	}
	return nil
}

// ──────────────────── OpenSubtitles API types ────────────────────

type osSearchResponse struct {
	Data []struct {
		Attributes struct {
			Language string  `json:"language"`
			Format   string  `json:"format"`
			Ratings  float32 `json:"ratings"`
			Files    []struct {
				FileID   int    `json:"file_id"`
				FileName string `json:"file_name"`
			} `json:"files"`
		} `json:"attributes"`
	} `json:"data"`
}

type osDownloadResponse struct {
	Link string `json:"link"`
}
