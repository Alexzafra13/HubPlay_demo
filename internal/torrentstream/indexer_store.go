package torrentstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"hubplay/internal/domain"
)

// indexerStoreKey is the app_settings key under which the indexer list is
// persisted as JSON. Using the generic settings KV keeps this plug-and-play
// (no dedicated table / migration) and works on both SQLite and Postgres.
const indexerStoreKey = "torznab.indexers"

// KVStore is the slice of the settings repository the indexer store needs.
// A local interface (sink pattern) keeps torrentstream from importing the
// db package.
type KVStore interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string) error
}

// IndexerInput is the create/update payload for an indexer (no server-side
// fields like ID).
type IndexerInput struct {
	Name             string   `json:"name"`
	BaseURL          string   `json:"base_url"`
	URL              string   `json:"url"`
	APIKey           string   `json:"api_key"`
	Enabled          bool     `json:"enabled"`
	MovieCategories  []string `json:"movie_categories"`
	SeriesCategories []string `json:"series_categories"`
	Trackers         []string `json:"trackers"`
}

// IndexerRecord is a persisted indexer (input + server-assigned id).
type IndexerRecord struct {
	ID string `json:"id"`
	IndexerInput
}

// IndexerStore persists the admin-managed indexer list in the settings KV
// and serves it to the search client. It is the single source of truth for
// indexers at runtime — edits via the admin API take effect on the next
// search, no restart required.
//
// Safe for concurrent use; writes are serialised so concurrent admin edits
// don't lose each other (read-modify-write under the mutex).
type IndexerStore struct {
	kv         KVStore
	httpClient *http.Client
	mu         sync.Mutex
}

// NewIndexerStore builds the store over the given KV (the app_settings
// repository).
func NewIndexerStore(kv KVStore) *IndexerStore {
	return &IndexerStore{
		kv:         kv,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// List returns every persisted indexer record (enabled and disabled). An
// absent key is an empty list, not an error.
func (s *IndexerStore) List(ctx context.Context) ([]IndexerRecord, error) {
	raw, err := s.kv.Get(ctx, indexerStoreKey)
	if errors.Is(err, domain.ErrNotFound) {
		return []IndexerRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(raw) == "" {
		return []IndexerRecord{}, nil
	}
	var recs []IndexerRecord
	if err := json.Unmarshal([]byte(raw), &recs); err != nil {
		return nil, fmt.Errorf("torznab: decode indexer store: %w", err)
	}
	return recs, nil
}

// Add persists a new indexer and returns the stored record (with its id).
func (s *IndexerStore) Add(ctx context.Context, in IndexerInput) (IndexerRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	recs, err := s.List(ctx)
	if err != nil {
		return IndexerRecord{}, err
	}
	rec := IndexerRecord{ID: uuid.NewString(), IndexerInput: in}
	recs = append(recs, rec)
	if err := s.save(ctx, recs); err != nil {
		return IndexerRecord{}, err
	}
	return rec, nil
}

// Update replaces the input fields of the record with the given id. Returns
// domain.ErrNotFound when the id is unknown.
func (s *IndexerStore) Update(ctx context.Context, id string, in IndexerInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	recs, err := s.List(ctx)
	if err != nil {
		return err
	}
	for i := range recs {
		if recs[i].ID == id {
			// An empty api_key means "leave unchanged" — the admin list
			// never echoes the secret, so a round-tripped edit/toggle
			// would otherwise wipe it.
			if in.APIKey == "" {
				in.APIKey = recs[i].APIKey
			}
			recs[i].IndexerInput = in
			return s.save(ctx, recs)
		}
	}
	return domain.ErrNotFound
}

// Delete removes the record with the given id. Idempotent: deleting an
// unknown id is a no-op (no error) so the UI's delete is safe to retry.
func (s *IndexerStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	recs, err := s.List(ctx)
	if err != nil {
		return err
	}
	out := recs[:0]
	for _, r := range recs {
		if r.ID != id {
			out = append(out, r)
		}
	}
	return s.save(ctx, out)
}

// Seed imports the given records when the store is empty (one-time import
// from YAML/env config). Once the operator manages indexers via the API,
// the config seed is ignored. No-op when records already exist or the seed
// is empty.
func (s *IndexerStore) Seed(ctx context.Context, seed []IndexerRecord) error {
	if len(seed) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	recs, err := s.List(ctx)
	if err != nil {
		return err
	}
	if len(recs) > 0 {
		return nil
	}
	for i := range seed {
		if seed[i].ID == "" {
			seed[i].ID = uuid.NewString()
		}
	}
	return s.save(ctx, seed)
}

func (s *IndexerStore) save(ctx context.Context, recs []IndexerRecord) error {
	data, err := json.Marshal(recs)
	if err != nil {
		return fmt.Errorf("torznab: encode indexer store: %w", err)
	}
	return s.kv.Set(ctx, indexerStoreKey, string(data))
}

// Indexers implements IndexerProvider: the enabled records resolved into
// search-ready TorznabIndexers. Read fresh on every search so admin edits
// apply immediately.
func (s *IndexerStore) Indexers(ctx context.Context) []TorznabIndexer {
	recs, err := s.List(ctx)
	if err != nil {
		return nil
	}
	out := make([]TorznabIndexer, 0, len(recs))
	for _, r := range recs {
		if !r.Enabled {
			continue
		}
		out = append(out, r.toTorznabIndexer())
	}
	return out
}

// toTorznabIndexer resolves a record into a search-ready indexer (composing
// the Prowlarr Torznab path from base_url when no full url is set).
func (r IndexerRecord) toTorznabIndexer() TorznabIndexer {
	resolved := r.URL
	if resolved == "" && r.BaseURL != "" {
		resolved = ProwlarrTorznabURL(r.BaseURL)
	}
	return TorznabIndexer{
		Name:             r.Name,
		URL:              resolved,
		BaseURL:          r.BaseURL,
		APIKey:           r.APIKey,
		MovieCategories:  r.MovieCategories,
		SeriesCategories: r.SeriesCategories,
		Trackers:         r.Trackers,
	}
}

// Statuses returns every configured indexer with a live reachability probe
// (enabled ones are pinged concurrently; disabled ones report reachable=
// false without a probe). Secret-free — the API key is never echoed, only
// HasAPIKey.
func (s *IndexerStore) Statuses(ctx context.Context) ([]IndexerStatus, error) {
	recs, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]IndexerStatus, len(recs))
	var wg sync.WaitGroup
	for i, r := range recs {
		out[i] = IndexerStatus{IndexerInfo: r.Info()}
		if !r.Enabled {
			continue
		}
		wg.Add(1)
		go func(i int, r IndexerRecord) {
			defer wg.Done()
			pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			ti := r.toTorznabIndexer()
			if err := pingTorznab(pctx, s.httpClient, ti.URL, ti.APIKey); err != nil {
				out[i].Error = err.Error()
			} else {
				out[i].Reachable = true
				// Best-effort: when it's a Prowlarr root, list the
				// trackers it aggregates so the admin UI can show them.
				if r.BaseURL != "" {
					if names, err := fetchProwlarrIndexers(pctx, s.httpClient, r.BaseURL, r.APIKey); err == nil {
						out[i].Trackers = names
					}
				}
			}
		}(i, r)
	}
	wg.Wait()
	// Stable order: by name so the admin list doesn't jump around.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Info returns the secret-free view of the record (HasAPIKey instead of
// the key itself).
func (r IndexerRecord) Info() IndexerInfo {
	return IndexerInfo{
		ID:               r.ID,
		Name:             r.Name,
		BaseURL:          r.BaseURL,
		URL:              r.URL,
		Enabled:          r.Enabled,
		HasAPIKey:        r.APIKey != "",
		MovieCategories:  r.MovieCategories,
		SeriesCategories: r.SeriesCategories,
	}
}
