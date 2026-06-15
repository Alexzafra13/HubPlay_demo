package torrentstream

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"hubplay/internal/domain"
)

// fakeKV is an in-memory KVStore mirroring the settings repo semantics
// (missing key → domain.ErrNotFound).
type fakeKV struct {
	mu sync.Mutex
	m  map[string]string
}

func newFakeKV() *fakeKV { return &fakeKV{m: map[string]string{}} }

func (k *fakeKV) Get(_ context.Context, key string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.m[key]
	if !ok {
		return "", fmt.Errorf("setting %q: %w", key, domain.ErrNotFound)
	}
	return v, nil
}

func (k *fakeKV) Set(_ context.Context, key, value string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.m[key] = value
	return nil
}

func TestIndexerStoreCRUD(t *testing.T) {
	ctx := context.Background()
	s := NewIndexerStore(newFakeKV())

	// Empty to start.
	if recs, err := s.List(ctx); err != nil || len(recs) != 0 {
		t.Fatalf("empty list: recs=%v err=%v", recs, err)
	}

	rec, err := s.Add(ctx, IndexerInput{Name: "prowlarr", BaseURL: "http://localhost:9696", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if rec.ID == "" {
		t.Fatal("Add must assign an id")
	}

	recs, _ := s.List(ctx)
	if len(recs) != 1 || recs[0].Name != "prowlarr" {
		t.Fatalf("after add: %+v", recs)
	}

	// Update.
	if err := s.Update(ctx, rec.ID, IndexerInput{Name: "renamed", URL: "http://x/torznab", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	recs, _ = s.List(ctx)
	if recs[0].Name != "renamed" || recs[0].Enabled {
		t.Fatalf("after update: %+v", recs[0])
	}

	// Update unknown id → ErrNotFound.
	if err := s.Update(ctx, "nope", IndexerInput{Name: "x", URL: "http://y"}); err == nil {
		t.Fatal("expected ErrNotFound for unknown id")
	}

	// Delete.
	if err := s.Delete(ctx, rec.ID); err != nil {
		t.Fatal(err)
	}
	if recs, _ := s.List(ctx); len(recs) != 0 {
		t.Fatalf("after delete: %+v", recs)
	}
}

func TestIndexerStoreSeed(t *testing.T) {
	ctx := context.Background()
	s := NewIndexerStore(newFakeKV())

	seed := []IndexerRecord{
		{IndexerInput: IndexerInput{Name: "seeded", BaseURL: "http://p:9696", Enabled: true}},
	}
	if err := s.Seed(ctx, seed); err != nil {
		t.Fatal(err)
	}
	recs, _ := s.List(ctx)
	if len(recs) != 1 || recs[0].ID == "" {
		t.Fatalf("seed should import with an id: %+v", recs)
	}

	// Seeding again is a no-op (store already populated).
	if err := s.Seed(ctx, []IndexerRecord{{IndexerInput: IndexerInput{Name: "other", URL: "http://x"}}}); err != nil {
		t.Fatal(err)
	}
	if recs, _ := s.List(ctx); len(recs) != 1 {
		t.Fatalf("second seed must be a no-op: %+v", recs)
	}
}

func TestIndexerStoreIndexersFiltersAndResolves(t *testing.T) {
	ctx := context.Background()
	s := NewIndexerStore(newFakeKV())
	_, _ = s.Add(ctx, IndexerInput{Name: "on", BaseURL: "http://localhost:9696", Enabled: true})
	_, _ = s.Add(ctx, IndexerInput{Name: "off", URL: "http://x/torznab", Enabled: false})

	idx := s.Indexers(ctx)
	if len(idx) != 1 {
		t.Fatalf("only enabled indexers: got %d", len(idx))
	}
	want := "http://localhost:9696/api/v1/indexers/all/results/torznab"
	if idx[0].URL != want {
		t.Errorf("base_url should resolve to prowlarr torznab path: got %q", idx[0].URL)
	}
}
