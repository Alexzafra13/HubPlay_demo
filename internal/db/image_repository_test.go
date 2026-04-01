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
	if err := database.Create(context.Background(), &db.Library{
		ID: "lib-img", Name: "Movies", ContentType: "movies", ScanMode: "auto",
		ScanInterval: "6h", CreatedAt: now, UpdatedAt: now, Paths: []string{"/media"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := itemRepo.Create(context.Background(), &db.Item{
		ID: "item-img", LibraryID: "lib-img", Type: "movie", Title: "Test",
		SortTitle: "test", Path: "/media/test.mkv",
		AddedAt: now, UpdatedAt: now, IsAvailable: true,
	}); err != nil {
		t.Fatal(err)
	}
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
	if err := repo.Create(context.Background(), &db.Image{
		ID: "img-p", ItemID: "item-img", Type: "primary",
		Path: "/images/poster.jpg", IsPrimary: true, AddedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

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

func TestImageRepository_GetByID(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	itemRepo := db.NewItemRepository(database)
	repo := db.NewImageRepository(database)
	seedItemForImages(t, libRepo, itemRepo)

	now := time.Now()
	if err := repo.Create(context.Background(), &db.Image{
		ID: "img-get", ItemID: "item-img", Type: "primary",
		Path: "/images/get.jpg", Width: 640, Height: 960,
		Blurhash: "LEHV6nWB2yk8", Provider: "tmdb",
		IsPrimary: true, AddedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetByID(context.Background(), "img-get")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "img-get" {
		t.Errorf("expected ID img-get, got %q", got.ID)
	}
	if got.Width != 640 || got.Height != 960 {
		t.Errorf("expected 640x960, got %dx%d", got.Width, got.Height)
	}
	if got.Blurhash != "LEHV6nWB2yk8" {
		t.Errorf("expected blurhash LEHV6nWB2yk8, got %q", got.Blurhash)
	}
	if got.Provider != "tmdb" {
		t.Errorf("expected provider tmdb, got %q", got.Provider)
	}
}

func TestImageRepository_GetByID_NotFound(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewImageRepository(database)

	_, err := repo.GetByID(context.Background(), "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestImageRepository_DeleteByID(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	itemRepo := db.NewItemRepository(database)
	repo := db.NewImageRepository(database)
	seedItemForImages(t, libRepo, itemRepo)

	now := time.Now()
	if err := repo.Create(context.Background(), &db.Image{
		ID: "img-del1", ItemID: "item-img", Type: "primary",
		Path: "/images/del1.jpg", AddedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(context.Background(), &db.Image{
		ID: "img-del2", ItemID: "item-img", Type: "backdrop",
		Path: "/images/del2.jpg", AddedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	if err := repo.DeleteByID(context.Background(), "img-del1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only img-del2 should remain.
	got, _ := repo.ListByItem(context.Background(), "item-img")
	if len(got) != 1 {
		t.Fatalf("expected 1 image after delete, got %d", len(got))
	}
	if got[0].ID != "img-del2" {
		t.Errorf("expected img-del2 to remain, got %q", got[0].ID)
	}
}

func TestImageRepository_SetPrimary(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	itemRepo := db.NewItemRepository(database)
	repo := db.NewImageRepository(database)
	seedItemForImages(t, libRepo, itemRepo)

	now := time.Now()
	if err := repo.Create(context.Background(), &db.Image{
		ID: "img-sp1", ItemID: "item-img", Type: "primary",
		Path: "/images/sp1.jpg", IsPrimary: true, AddedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(context.Background(), &db.Image{
		ID: "img-sp2", ItemID: "item-img", Type: "primary",
		Path: "/images/sp2.jpg", IsPrimary: false, AddedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	// Switch primary to img-sp2.
	if err := repo.SetPrimary(context.Background(), "item-img", "primary", "img-sp2"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify img-sp2 is now primary.
	got, err := repo.GetPrimary(context.Background(), "item-img", "primary")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "img-sp2" {
		t.Errorf("expected img-sp2 as primary, got %q", got.ID)
	}

	// Verify img-sp1 is no longer primary.
	img1, err := repo.GetByID(context.Background(), "img-sp1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if img1.IsPrimary {
		t.Error("expected img-sp1 to no longer be primary")
	}
}

func TestImageRepository_DeleteByItem(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	itemRepo := db.NewItemRepository(database)
	repo := db.NewImageRepository(database)
	seedItemForImages(t, libRepo, itemRepo)

	now := time.Now()
	if err := repo.Create(context.Background(), &db.Image{
		ID: "img-d", ItemID: "item-img", Type: "primary",
		Path: "/images/del.jpg", AddedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	if err := repo.DeleteByItem(context.Background(), "item-img"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := repo.ListByItem(context.Background(), "item-img")
	if len(got) != 0 {
		t.Errorf("expected 0 images after delete, got %d", len(got))
	}
}
