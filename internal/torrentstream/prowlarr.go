package torrentstream

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// This file isolates the "connection to Prowlarr/Torznab" concerns —
// status, connection testing and the optional native indexer listing —
// from the search and curation logic. HubPlay only ever consumes the
// standard Torznab API; Prowlarr is treated as a central aggregator that
// the operator manages on their side.

// IndexerInfo is a config-derived, secret-free view of one configured
// indexer for the admin surface.
type IndexerInfo struct {
	ID               string   `json:"id,omitempty"`
	Name             string   `json:"name"`
	BaseURL          string   `json:"base_url,omitempty"`
	URL              string   `json:"url,omitempty"`
	Enabled          bool     `json:"enabled"`
	HasAPIKey        bool     `json:"has_api_key"`
	MovieCategories  []string `json:"movie_categories,omitempty"`
	SeriesCategories []string `json:"series_categories,omitempty"`
}

// IndexerStatus is IndexerInfo plus the result of a live reachability
// probe (and, when reachable via the Prowlarr native API, the names of the
// trackers it aggregates).
type IndexerStatus struct {
	IndexerInfo
	Reachable bool     `json:"reachable"`
	Error     string   `json:"error,omitempty"`
	Trackers  []string `json:"trackers,omitempty"`
}

// TestTorznabConnection validates an indexer address + API key without a
// pre-built client. Used by the admin "test before save" endpoint.
// baseOrURL may be a full Torznab URL (caps query) or a Prowlarr root (in
// which case we verify via the native indexer-list API).
func TestTorznabConnection(ctx context.Context, baseOrURL, apiKey string) error {
	client := &http.Client{Timeout: 10 * time.Second}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err := probeIndexer(ctx, client, baseOrURL, apiKey)
	return err
}

// probeIndexer checks reachability of an indexer address. For a Torznab
// feed URL it issues a caps query; for a bare Prowlarr root it lists the
// native indexers (returning their enabled names so the admin UI can show
// what the instance aggregates).
func probeIndexer(ctx context.Context, client *http.Client, raw, apiKey string) ([]string, error) {
	if looksLikeTorznab(raw) {
		return nil, pingTorznab(ctx, client, raw, apiKey)
	}
	list, err := fetchProwlarrIndexers(ctx, client, raw, apiKey)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(list))
	for _, ix := range list {
		if ix.Enable {
			names = append(names, ix.Name)
		}
	}
	return names, nil
}

// torznabError mirrors the <error> element Torznab returns for bad
// credentials / parameters.
type torznabError struct {
	XMLName     xml.Name `xml:"error"`
	Code        string   `xml:"code,attr"`
	Description string   `xml:"description,attr"`
}

// pingTorznab issues a `t=caps` request and reports a reachable, correctly
// authenticated endpoint (no Torznab <error> in the body).
func pingTorznab(ctx context.Context, client *http.Client, torznabURL, apiKey string) error {
	u, err := url.Parse(torznabURL)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("invalid url %q", torznabURL)
	}
	q := u.Query()
	q.Set("t", "caps")
	if apiKey != "" {
		q.Set("apikey", apiKey)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("unreachable: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("authentication failed (status %d)", resp.StatusCode)
	}
	// A Torznab <error> can arrive with a 200 status, so always check.
	if e := parseTorznabError(body); e != nil {
		return e
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

// parseTorznabError returns a non-nil error when the body is a Torznab
// <error> element.
func parseTorznabError(body []byte) error {
	if !strings.Contains(string(body), "<error") {
		return nil
	}
	var te torznabError
	if err := xml.Unmarshal(body, &te); err != nil {
		return nil // not actually an error element
	}
	if te.Description != "" {
		return fmt.Errorf("indexer error %s: %s", te.Code, te.Description)
	}
	return fmt.Errorf("indexer error %s", te.Code)
}

// prowlarrIndexer is the slice of Prowlarr's native /api/v1/indexer JSON we
// read to enumerate the per-indexer Torznab feeds (Prowlarr has no combined
// feed). id is needed to build /{id}/api.
type prowlarrIndexer struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Enable bool   `json:"enable"`
}

// fetchProwlarrIndexers queries Prowlarr's native indexer listing (JSON).
// Any failure (unreachable, bad key → 401, not a Prowlarr → 404) is
// returned so the caller can surface "not connected".
func fetchProwlarrIndexers(ctx context.Context, client *http.Client, baseURL, apiKey string) ([]prowlarrIndexer, error) {
	u := strings.TrimRight(baseURL, "/") + "/api/v1/indexer"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if apiKey != "" {
		req.Header.Set("X-Api-Key", apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("unreachable: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prowlarr indexer list status %d", resp.StatusCode)
	}
	var list []prowlarrIndexer
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&list); err != nil {
		return nil, err
	}
	return list, nil
}
