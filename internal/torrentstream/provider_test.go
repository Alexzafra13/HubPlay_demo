package torrentstream

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

type stubProvider struct {
	name string
	res  []SearchResult
	err  error
}

func (s stubProvider) Name() string { return s.name }
func (s stubProvider) Search(context.Context, string, int) ([]SearchResult, error) {
	return s.res, s.err
}

func newSearchTestManager(providers ...SearchProvider) *Manager {
	return &Manager{logger: slog.Default(), providers: providers}
}

func TestManagerSearch_MergesProviders(t *testing.T) {
	m := newSearchTestManager(
		stubProvider{name: "a", res: []SearchResult{{Identifier: "1"}}},
		stubProvider{name: "b", res: []SearchResult{{Identifier: "2"}, {Identifier: "3"}}},
	)
	out, err := m.Search(context.Background(), "q", 10)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("results: got %d want 3", len(out))
	}
}

func TestManagerSearch_SkipsFailingProvider(t *testing.T) {
	m := newSearchTestManager(
		stubProvider{name: "bad", err: errors.New("boom")},
		stubProvider{name: "good", res: []SearchResult{{Identifier: "ok"}}},
	)
	out, err := m.Search(context.Background(), "q", 10)
	if err != nil {
		t.Fatalf("err should be swallowed when another provider succeeds: %v", err)
	}
	if len(out) != 1 || out[0].Identifier != "ok" {
		t.Fatalf("expected the good provider's result, got %+v", out)
	}
}

func TestManagerSearch_AllFail_ReturnsError(t *testing.T) {
	m := newSearchTestManager(
		stubProvider{name: "bad", err: errors.New("boom")},
	)
	if _, err := m.Search(context.Background(), "q", 10); err == nil {
		t.Fatal("expected an error when every provider fails")
	}
}

func TestDefaultProvidersIsArchiveOnly(t *testing.T) {
	ps := defaultProviders()
	if len(ps) != 1 || ps[0].Name() != "internetarchive" {
		t.Fatalf("default providers should be Internet Archive only, got %v", ps)
	}
}
