package db_test

import (
	"context"
	"testing"

	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

func setupProviderTest(t *testing.T) *db.ProviderRepository {
	t.Helper()
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(database)
	return repos.Providers
}

func TestProvider_UpsertAndGet(t *testing.T) {
	repo := setupProviderTest(t)
	ctx := context.Background()

	cfg := &db.ProviderConfig{
		Name:     "tmdb",
		Type:     "metadata",
		Version:  "1.0",
		Status:   "active",
		Priority: 50,
		APIKey:   "test-key-123",
	}

	if err := repo.Upsert(ctx, cfg); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetByName(ctx, "tmdb")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected provider, got nil")
	}
	if got.Name != "tmdb" {
		t.Errorf("name = %q, want tmdb", got.Name)
	}
	if got.Type != "metadata" {
		t.Errorf("type = %q, want metadata", got.Type)
	}
	if got.Priority != 50 {
		t.Errorf("priority = %d, want 50", got.Priority)
	}
	if got.APIKey != "test-key-123" {
		t.Errorf("api_key = %q, want test-key-123", got.APIKey)
	}

	// Update via upsert
	cfg.Priority = 10
	cfg.APIKey = "new-key"
	if err := repo.Upsert(ctx, cfg); err != nil {
		t.Fatal(err)
	}

	got, _ = repo.GetByName(ctx, "tmdb")
	if got.Priority != 10 {
		t.Errorf("updated priority = %d, want 10", got.Priority)
	}
	if got.APIKey != "new-key" {
		t.Errorf("updated api_key = %q, want new-key", got.APIKey)
	}
}

func TestProvider_GetNotFound(t *testing.T) {
	repo := setupProviderTest(t)

	got, err := repo.GetByName(context.Background(), "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent provider")
	}
}

func TestProvider_ListActive(t *testing.T) {
	repo := setupProviderTest(t)
	ctx := context.Background()

	_ = repo.Upsert(ctx, &db.ProviderConfig{Name: "tmdb", Type: "metadata", Version: "1.0", Status: "active", Priority: 50})
	_ = repo.Upsert(ctx, &db.ProviderConfig{Name: "fanart", Type: "image", Version: "1.0", Status: "active", Priority: 100})
	_ = repo.Upsert(ctx, &db.ProviderConfig{Name: "disabled-one", Type: "metadata", Version: "1.0", Status: "disabled", Priority: 10})

	active, err := repo.ListActive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 2 {
		t.Fatalf("expected 2 active, got %d", len(active))
	}
	// Should be ordered by priority
	if active[0].Name != "tmdb" {
		t.Errorf("first active = %q, want tmdb (priority 50)", active[0].Name)
	}
	if active[1].Name != "fanart" {
		t.Errorf("second active = %q, want fanart (priority 100)", active[1].Name)
	}
}

func TestProvider_ListByType(t *testing.T) {
	repo := setupProviderTest(t)
	ctx := context.Background()

	_ = repo.Upsert(ctx, &db.ProviderConfig{Name: "tmdb", Type: "metadata", Version: "1.0", Status: "active", Priority: 50})
	_ = repo.Upsert(ctx, &db.ProviderConfig{Name: "fanart", Type: "image", Version: "1.0", Status: "active", Priority: 100})
	_ = repo.Upsert(ctx, &db.ProviderConfig{Name: "opensubs", Type: "subtitle", Version: "1.0", Status: "active", Priority: 100})

	metadata, err := repo.ListByType(ctx, "metadata")
	if err != nil {
		t.Fatal(err)
	}
	if len(metadata) != 1 || metadata[0].Name != "tmdb" {
		t.Errorf("metadata providers = %v", metadata)
	}

	images, _ := repo.ListByType(ctx, "image")
	if len(images) != 1 || images[0].Name != "fanart" {
		t.Errorf("image providers = %v", images)
	}
}

func TestProvider_SetStatus(t *testing.T) {
	repo := setupProviderTest(t)
	ctx := context.Background()

	_ = repo.Upsert(ctx, &db.ProviderConfig{Name: "tmdb", Type: "metadata", Version: "1.0", Status: "active", Priority: 50})

	if err := repo.SetStatus(ctx, "tmdb", "disabled"); err != nil {
		t.Fatal(err)
	}

	got, _ := repo.GetByName(ctx, "tmdb")
	if got.Status != "disabled" {
		t.Errorf("status = %q, want disabled", got.Status)
	}

	// Not found
	err := repo.SetStatus(ctx, "nonexistent", "active")
	if err == nil {
		t.Error("expected error for nonexistent provider")
	}
}

func TestProvider_Delete(t *testing.T) {
	repo := setupProviderTest(t)
	ctx := context.Background()

	_ = repo.Upsert(ctx, &db.ProviderConfig{Name: "tmdb", Type: "metadata", Version: "1.0", Status: "active", Priority: 50})
	_ = repo.Delete(ctx, "tmdb")

	got, _ := repo.GetByName(ctx, "tmdb")
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestProvider_ListAll(t *testing.T) {
	repo := setupProviderTest(t)
	ctx := context.Background()

	_ = repo.Upsert(ctx, &db.ProviderConfig{Name: "tmdb", Type: "metadata", Version: "1.0", Status: "active", Priority: 100})
	_ = repo.Upsert(ctx, &db.ProviderConfig{Name: "disabled", Type: "metadata", Version: "1.0", Status: "disabled", Priority: 50})

	all, err := repo.ListAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(all))
	}
	// Disabled one has lower priority number, so comes first
	if all[0].Name != "disabled" {
		t.Errorf("first = %q, want disabled (priority 50)", all[0].Name)
	}
}
