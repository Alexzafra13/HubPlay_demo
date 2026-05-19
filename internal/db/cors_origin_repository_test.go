package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

func TestCorsOriginRepository_InsertAndList(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewCorsOriginRepository(testutil.Driver(), database)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	rows := []db.CorsOriginRow{
		{
			Origin:    "https://app.example.com",
			CreatedBy: "u-owner",
			CreatedAt: now.Add(-2 * time.Hour),
			Note:      "frontend prod",
		},
		{
			Origin:    "https://staging.example.com",
			CreatedBy: "u-owner",
			CreatedAt: now.Add(-1 * time.Hour),
			Note:      "frontend staging",
		},
		{
			Origin:    "https://preview.example.com",
			CreatedBy: "u-owner",
			CreatedAt: now,
			Note:      "",
		},
	}
	for _, r := range rows {
		if err := repo.Insert(ctx, r); err != nil {
			t.Fatalf("insert %s: %v", r.Origin, err)
		}
	}

	got, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 rows, got %d", len(got))
	}
	// DESC order: preview (most recent) → staging → app.
	want := []string{"https://preview.example.com", "https://staging.example.com", "https://app.example.com"}
	for i, o := range want {
		if got[i].Origin != o {
			t.Errorf("row %d = %s, want %s", i, got[i].Origin, o)
		}
	}
}

func TestCorsOriginRepository_Insert_RejectsDuplicate(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewCorsOriginRepository(testutil.Driver(), database)
	ctx := context.Background()

	row := db.CorsOriginRow{Origin: "https://x.example.com", CreatedBy: "u-1"}
	if err := repo.Insert(ctx, row); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	err := repo.Insert(ctx, row)
	if !errors.Is(err, db.ErrCorsOriginExists) {
		t.Errorf("want ErrCorsOriginExists, got %v", err)
	}
}

func TestCorsOriginRepository_Delete(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewCorsOriginRepository(testutil.Driver(), database)
	ctx := context.Background()

	row := db.CorsOriginRow{Origin: "https://gone.example.com"}
	_ = repo.Insert(ctx, row)

	if err := repo.Delete(ctx, "https://gone.example.com"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, _ := repo.List(ctx)
	if len(got) != 0 {
		t.Errorf("want 0 after delete, got %d", len(got))
	}
}

func TestCorsOriginRepository_Delete_NoopOnMissing(t *testing.T) {
	// Delete sobre un origen inexistente no devuelve error — pin de la
	// decisión "idempotente". Sin este pin, alguien podría cambiar el
	// repo a devolver ErrNotFound y romper el contrato 204 del handler.
	database := testutil.NewTestDB(t)
	repo := db.NewCorsOriginRepository(testutil.Driver(), database)

	if err := repo.Delete(context.Background(), "https://never-existed.example.com"); err != nil {
		t.Errorf("delete missing should be no-op, got %v", err)
	}
}

func TestCorsOriginRepository_ListOrigins(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewCorsOriginRepository(testutil.Driver(), database)
	ctx := context.Background()

	for _, o := range []string{"https://a.example.com", "https://b.example.com"} {
		_ = repo.Insert(ctx, db.CorsOriginRow{Origin: o})
	}

	got, err := repo.ListOrigins(ctx)
	if err != nil {
		t.Fatalf("list strings: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d", len(got))
	}
}
