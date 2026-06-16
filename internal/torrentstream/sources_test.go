package torrentstream

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeSearcher counts calls so we can assert the cache short-circuits the
// indexer round-trip.
type fakeSearcher struct {
	calls int
	res   []SearchResult
	err   error
}

func (f *fakeSearcher) Search(_ context.Context, _ Query) ([]SearchResult, error) {
	f.calls++
	return f.res, f.err
}

func TestSourceServiceCaches(t *testing.T) {
	fake := &fakeSearcher{res: []SearchResult{{Identifier: "x", Title: "X"}}}
	svc := NewSourceService(fake, time.Minute, DefaultFilterOptions(), nil)

	r1, err := svc.Sources(context.Background(), MediaTypeMovie, "tt1")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if len(r1) != 1 {
		t.Fatalf("results: %d", len(r1))
	}
	// Second call for the same key must hit the cache.
	if _, err := svc.Sources(context.Background(), MediaTypeMovie, "tt1"); err != nil {
		t.Fatalf("second: %v", err)
	}
	if fake.calls != 1 {
		t.Errorf("calls: got %d want 1 (cache miss only once)", fake.calls)
	}

	// A different media type is a different key → another round-trip.
	if _, err := svc.Sources(context.Background(), MediaTypeSeries, "tt1"); err != nil {
		t.Fatalf("series: %v", err)
	}
	if fake.calls != 2 {
		t.Errorf("calls after series: got %d want 2", fake.calls)
	}
}

func TestSourceServiceCurates(t *testing.T) {
	// Raw set has a cam that curation must drop, and the context can
	// override the default filter options.
	fake := &fakeSearcher{res: []SearchResult{
		src("Movie 1080p WEB-DL", 10, 2e9),
		src("Movie 1080p HDCAM", 999, 1e9),
	}}
	svc := NewSourceService(fake, time.Minute, DefaultFilterOptions(), nil)

	res, err := svc.Sources(context.Background(), MediaTypeMovie, "tt1")
	if err != nil {
		t.Fatalf("sources: %v", err)
	}
	if len(res) != 1 || res[0].IsCam {
		t.Fatalf("curation should drop the cam: %+v", res)
	}
}

func TestSourceServiceErrorNotCached(t *testing.T) {
	fake := &fakeSearcher{err: errors.New("indexer down")}
	svc := NewSourceService(fake, time.Minute, DefaultFilterOptions(), nil)

	if _, err := svc.Sources(context.Background(), MediaTypeMovie, "tt1"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := svc.Sources(context.Background(), MediaTypeMovie, "tt1"); err == nil {
		t.Fatal("expected error on retry")
	}
	if fake.calls != 2 {
		t.Errorf("errors must not be cached: calls=%d want 2", fake.calls)
	}
}

func TestTTLCacheExpiry(t *testing.T) {
	c := newTTLCache[int](time.Minute)
	now := time.Unix(0, 0)
	c.now = func() time.Time { return now }

	c.set("k", 7)
	if v, ok := c.get("k"); !ok || v != 7 {
		t.Fatalf("fresh get: v=%d ok=%v", v, ok)
	}
	// Advance past the TTL → expired miss.
	now = now.Add(2 * time.Minute)
	if _, ok := c.get("k"); ok {
		t.Fatal("expected expired miss")
	}
}
