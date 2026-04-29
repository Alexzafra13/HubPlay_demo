package db_test

import (
	"context"
	"errors"
	"testing"

	"hubplay/internal/db"
	"hubplay/internal/domain"
	"hubplay/internal/testutil"
)

func TestSettingsRepository_GetOr_FallsBackToDefault(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewSettingsRepository(database)

	// Empty table → fallback wins. This is the load-bearing path
	// through which YAML / env defaults reach runtime; if it ever
	// regressed, every fresh install would silently lose its
	// configured base_url and look broken.
	got, err := repo.GetOr(context.Background(), "server.base_url", "https://default.example/")
	if err != nil {
		t.Fatalf("GetOr unexpected error: %v", err)
	}
	if got != "https://default.example/" {
		t.Errorf("default fallback: got %q want %q", got, "https://default.example/")
	}
}

func TestSettingsRepository_SetThenGet_OverridesDefault(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewSettingsRepository(database)
	ctx := context.Background()

	if err := repo.Set(ctx, "server.base_url", "https://prod.example/"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := repo.GetOr(ctx, "server.base_url", "https://default.example/")
	if err != nil {
		t.Fatalf("GetOr: %v", err)
	}
	if got != "https://prod.example/" {
		t.Errorf("override: got %q want %q", got, "https://prod.example/")
	}
}

// Set is upsert: a second Set on the same key replaces the value, not
// appends. ON CONFLICT(key) DO UPDATE in the SQL is what backs that;
// the test pins the contract so a future refactor that drops the
// upsert clause fails here instead of in production.
func TestSettingsRepository_Set_IsUpsert(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewSettingsRepository(database)
	ctx := context.Background()

	if err := repo.Set(ctx, "k", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Set(ctx, "k", "v2"); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, "k")
	if err != nil {
		t.Fatal(err)
	}
	if got != "v2" {
		t.Errorf("upsert: got %q want v2", got)
	}
}

func TestSettingsRepository_Get_NotFound(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewSettingsRepository(database)

	_, err := repo.Get(context.Background(), "missing")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// Delete reverts to the default — the explicit "reset to default"
// affordance the admin UI exposes. Without this a pinned override
// could only be replaced, never explicitly cleared.
func TestSettingsRepository_Delete_ReturnsToDefault(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewSettingsRepository(database)
	ctx := context.Background()

	if err := repo.Set(ctx, "server.base_url", "https://override.example/"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, "server.base_url"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, err := repo.GetOr(ctx, "server.base_url", "https://default.example/")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://default.example/" {
		t.Errorf("after delete: got %q want default", got)
	}
}

func TestSettingsRepository_All_ReturnsAllStored(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewSettingsRepository(database)
	ctx := context.Background()

	if err := repo.Set(ctx, "a", "1"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Set(ctx, "b", "2"); err != nil {
		t.Fatal(err)
	}
	got, err := repo.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got["a"] != "1" || got["b"] != "2" || len(got) != 2 {
		t.Errorf("All: got %v", got)
	}
}
