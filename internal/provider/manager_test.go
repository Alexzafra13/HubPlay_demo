package provider_test

import (
	"context"
	"fmt"
	"testing"

	"hubplay/internal/db"
	"hubplay/internal/provider"
	"hubplay/internal/testutil"
)

// fakeMetadataProvider is a test double for MetadataProvider.
type fakeMetadataProvider struct {
	name       string
	initErr    error
	searchData []provider.SearchResult
	metadata   *provider.MetadataResult
}

func (f *fakeMetadataProvider) Name() string                { return f.name }
func (f *fakeMetadataProvider) Init(map[string]string) error { return f.initErr }

func (f *fakeMetadataProvider) Search(_ context.Context, _ provider.SearchQuery) ([]provider.SearchResult, error) {
	return f.searchData, nil
}

func (f *fakeMetadataProvider) GetMetadata(_ context.Context, _ string, _ provider.ItemType) (*provider.MetadataResult, error) {
	return f.metadata, nil
}

// fakeImageProvider is a test double for ImageProvider.
type fakeImageProvider struct {
	name   string
	images []provider.ImageResult
}

func (f *fakeImageProvider) Name() string                { return f.name }
func (f *fakeImageProvider) Init(map[string]string) error { return nil }

func (f *fakeImageProvider) GetImages(_ context.Context, _ map[string]string, _ provider.ItemType) ([]provider.ImageResult, error) {
	return f.images, nil
}

func setupManager(t *testing.T) *provider.Manager {
	t.Helper()
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(database)
	return provider.NewManager(repos.Providers, testutil.TestLogger())
}

func TestManager_Register(t *testing.T) {
	m := setupManager(t)
	ctx := context.Background()

	p := &fakeMetadataProvider{name: "test-provider"}
	if err := m.Register(ctx, p); err != nil {
		t.Fatal(err)
	}

	names := m.ListProviders()
	if len(names) != 1 || names[0] != "test-provider" {
		t.Errorf("providers = %v, want [test-provider]", names)
	}
}

func TestManager_RegisterDuplicate(t *testing.T) {
	m := setupManager(t)
	ctx := context.Background()

	p := &fakeMetadataProvider{name: "dup"}
	_ = m.Register(ctx, p)

	err := m.Register(ctx, &fakeMetadataProvider{name: "dup"})
	if err == nil {
		t.Error("expected error registering duplicate")
	}
}

func TestManager_RegisterInitFail(t *testing.T) {
	m := setupManager(t)
	ctx := context.Background()

	p := &fakeMetadataProvider{name: "bad", initErr: fmt.Errorf("no api key")}
	// Should not error — just skip
	if err := m.Register(ctx, p); err != nil {
		t.Fatal(err)
	}

	// Should not be registered (init failed)
	names := m.ListProviders()
	if len(names) != 0 {
		t.Errorf("expected 0 providers after init fail, got %v", names)
	}
}

func TestManager_SearchMetadata(t *testing.T) {
	m := setupManager(t)
	ctx := context.Background()

	_ = m.Register(ctx, &fakeMetadataProvider{
		name: "provider-a",
		searchData: []provider.SearchResult{
			{ExternalID: "1", Title: "Movie A", Score: 0.9},
		},
	})
	_ = m.Register(ctx, &fakeMetadataProvider{
		name: "provider-b",
		searchData: []provider.SearchResult{
			{ExternalID: "2", Title: "Movie B", Score: 0.95},
		},
	})

	results, err := m.SearchMetadata(ctx, provider.SearchQuery{Title: "test"})
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// Should be sorted by score descending
	if results[0].Title != "Movie B" {
		t.Errorf("first result = %q, want Movie B (higher score)", results[0].Title)
	}
}

func TestManager_FetchMetadata(t *testing.T) {
	m := setupManager(t)
	ctx := context.Background()

	_ = m.Register(ctx, &fakeMetadataProvider{
		name: "tmdb-fake",
		metadata: &provider.MetadataResult{
			Title:   "Inception",
			Year:    2010,
			Overview: "A thief who steals corporate secrets...",
		},
	})

	result, err := m.FetchMetadata(ctx, "27205", provider.ItemMovie)
	if err != nil {
		t.Fatal(err)
	}
	if result.Title != "Inception" {
		t.Errorf("title = %q, want Inception", result.Title)
	}
}

func TestManager_FetchImages(t *testing.T) {
	m := setupManager(t)
	ctx := context.Background()

	_ = m.Register(ctx, &fakeImageProvider{
		name: "img-provider",
		images: []provider.ImageResult{
			{URL: "http://img.com/poster.jpg", Type: "primary", Score: 0.8},
			{URL: "http://img.com/backdrop.jpg", Type: "backdrop", Score: 0.9},
		},
	})

	images, err := m.FetchImages(ctx, map[string]string{"tmdb": "550"}, provider.ItemMovie)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 2 {
		t.Fatalf("expected 2 images, got %d", len(images))
	}
	// Sorted by score
	if images[0].Type != "backdrop" {
		t.Errorf("first image type = %q, want backdrop (higher score)", images[0].Type)
	}
}

func TestManager_GetProvider(t *testing.T) {
	m := setupManager(t)
	ctx := context.Background()

	_ = m.Register(ctx, &fakeMetadataProvider{name: "findme"})

	p, ok := m.GetProvider("findme")
	if !ok || p == nil {
		t.Error("expected to find provider")
	}

	_, ok = m.GetProvider("nope")
	if ok {
		t.Error("expected not to find nonexistent provider")
	}
}
