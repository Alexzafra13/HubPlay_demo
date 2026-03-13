package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/domain"
	"hubplay/internal/testutil"
)

func seedItemForImages(t *testing.T, database *db.LibraryRepository, itemRepo *db.ItemRepository) {
	t.Helper()
	now := time.Now()
	database.Create(context.Background(), &db.Library{
		ID: "lib-img", Name: "Movies", ContentType: "movies", ScanMode: "auto",
		ScanInterval: "6h", CreatedAt: now, UpdatedAt: now, Paths: []string{"/media"},
	})
	itemRepo.Create(context.Background(), &db.Item{
		ID: "item-img", LibraryID: "lib-img", Type: "movie", Title: "Test",
		SortTitle: "test", Path: "/media/test.mkv",
		AddedAt: now, UpdatedAt: now, IsAvailable: true,
	})
}

func TestImageRepository_Create_And_ListByItem(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	itemRepo := db.NewItemRepository(database)
	repo := db.NewImageRepository(database)
	seedItemForImages(t, libRepo, itemRepo)

	now := time.Now()
	img1 := &db.Image{
		ID: "img-1", ItemID: "item-img", Type: "primary",
		Path: "/images/poster.jpg", Width: 300, Height: 450,
		IsPrimary: true, AddedAt: now,
	}
	img2 := &db.Image{
		ID: "img-2", ItemID: "item-img", Type: "backdrop",
		Path: "/images/backdrop.jpg", Width: 1920, Height: 1080,
		AddedAt: now,
	}

	if err := repo.Create(context.Background(), img1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := repo.Create(context.Background(), img2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := repo.ListByItem(context.Background(), "item-img")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 images, got %d", len(got))
	}
	// Primary first
	if got[0].IsPrimary != true {
		t.Error("expected primary image first")
	}
}

func TestImageRepository_GetPrimary(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	itemRepo := db.NewItemRepository(database)
	repo := db.NewImageRepository(database)
	seedItemForImages(t, libRepo, itemRepo)

	now := time.Now()
	repo.Create(context.Background(), &db.Image{
		ID: "img-p", ItemID: "item-img", Type: "primary",
		Path: "/images/poster.jpg", IsPrimary: true, AddedAt: now,
	})

	got, err := repo.GetPrimary(context.Background(), "item-img", "primary")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Path != "/images/poster.jpg" {
		t.Errorf("expected poster path, got %q", got.Path)
	}
}

func TestImageRepository_GetPrimary_NotFound(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewImageRepository(database)

	_, err := repo.GetPrimary(context.Background(), "item-img", "primary")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestImageRepository_DeleteByItem(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	itemRepo := db.NewItemRepository(database)
	repo := db.NewImageRepository(database)
	seedItemForImages(t, libRepo, itemRepo)

	now := time.Now()
	repo.Create(context.Background(), &db.Image{
		ID: "img-d", ItemID: "item-img", Type: "primary",
		Path: "/images/del.jpg", AddedAt: now,
	})

	if err := repo.DeleteByItem(context.Background(), "item-img"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := repo.ListByItem(context.Background(), "item-img")
	if len(got) != 0 {
		t.Errorf("expected 0 images after delete, got %d", len(got))
	}
}
