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
	"sync"
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
	Name             string   `json:"name"`
	BaseURL          string   `json:"base_url,omitempty"`
	URL              string   `json:"url"`
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

// Instances returns the secret-free view of every configured indexer.
func (c *TorznabClient) Instances() []IndexerInfo {
	out := make([]IndexerInfo, 0, len(c.indexers))
	for _, idx := range c.indexers {
		out = append(out, IndexerInfo{
			Name:             idx.Name,
			BaseURL:          idx.BaseURL,
			URL:              idx.URL,
			HasAPIKey:        idx.APIKey != "",
			MovieCategories:  idx.MovieCategories,
			SeriesCategories: idx.SeriesCategories,
		})
	}
	return out
}

// Status probes every configured indexer concurrently (caps query) and
// returns each one's reachability. When an indexer exposes a Prowlarr root
// it also tries to enumerate the trackers it aggregates (best-effort).
func (c *TorznabClient) Status(ctx context.Context) []IndexerStatus {
	out := make([]IndexerStatus, len(c.indexers))
	var wg sync.WaitGroup
	for i, idx := range c.indexers {
		wg.Add(1)
		go func(i int, idx TorznabIndexer) {
			defer wg.Done()
			st := IndexerStatus{IndexerInfo: IndexerInfo{
				Name:             idx.Name,
				BaseURL:          idx.BaseURL,
				URL:              idx.URL,
				HasAPIKey:        idx.APIKey != "",
				MovieCategories:  idx.MovieCategories,
				SeriesCategories: idx.SeriesCategories,
			}}
			pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if err := pingTorznab(pctx, c.httpClient, idx.URL, idx.APIKey); err != nil {
				st.Error = err.Error()
			} else {
				st.Reachable = true
				if idx.BaseURL != "" {
					if names, err := fetchProwlarrIndexers(pctx, c.httpClient, idx.BaseURL, idx.APIKey); err == nil {
						st.Trackers = names
					}
				}
			}
			out[i] = st
		}(i, idx)
	}
	wg.Wait()
	return out
}

// TestTorznabConnection validates a Torznab endpoint + API key by issuing a
// caps query, without needing a pre-built client. Used by the admin "test
// before save" endpoint. baseOrURL may be a full Torznab URL or a Prowlarr
// root (in which case the standard path is composed).
func TestTorznabConnection(ctx context.Context, baseOrURL, apiKey string) error {
	torznabURL := resolveTorznabEndpoint(baseOrURL)
	client := &http.Client{Timeout: 10 * time.Second}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return pingTorznab(ctx, client, torznabURL, apiKey)
}

// resolveTorznabEndpoint returns a usable Torznab results URL: the input
// as-is when it already points at a torznab path, otherwise the composed
// Prowlarr path.
func resolveTorznabEndpoint(raw string) string {
	if strings.Contains(raw, "torznab") || strings.Contains(raw, "/api/v2.0/") {
		return raw
	}
	return ProwlarrTorznabURL(raw)
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
// read to enumerate aggregated trackers.
type prowlarrIndexer struct {
	Name   string `json:"name"`
	Enable bool   `json:"enable"`
}

// fetchProwlarrIndexers queries Prowlarr's native indexer listing (JSON)
// and returns the names of the enabled trackers. Best-effort: any failure
// is returned to the caller, which treats it as "not available".
func fetchProwlarrIndexers(ctx context.Context, client *http.Client, baseURL, apiKey string) ([]string, error) {
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
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prowlarr indexer list status %d", resp.StatusCode)
	}
	var list []prowlarrIndexer
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&list); err != nil {
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
