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

func newTestLibrary(id, name string) *db.Library {
	now := time.Now()
	return &db.Library{
		ID:           id,
		Name:         name,
		ContentType:  "movies",
		ScanMode:     "auto",
		ScanInterval: "6h",
		CreatedAt:    now,
		UpdatedAt:    now,
		Paths:        []string{"/media/movies"},
	}
}

func TestLibraryRepository_Create_And_GetByID(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewLibraryRepository(database)

	lib := newTestLibrary("lib-1", "Movies")
	lib.Paths = []string{"/media/movies", "/media/more-movies"}

	if err := repo.Create(context.Background(), lib); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := repo.GetByID(context.Background(), "lib-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Name != "Movies" {
		t.Errorf("expected name 'Movies', got %q", got.Name)
	}
	if got.ContentType != "movies" {
		t.Errorf("expected content_type 'movies', got %q", got.ContentType)
	}
	if len(got.Paths) != 2 {
		t.Errorf("expected 2 paths, got %d", len(got.Paths))
	}
}

func TestLibraryRepository_GetByID_NotFound(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewLibraryRepository(database)

	_, err := repo.GetByID(context.Background(), "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestLibraryRepository_List(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewLibraryRepository(database)

	if err := repo.Create(context.Background(), newTestLibrary("lib-a", "Anime")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(context.Background(), newTestLibrary("lib-m", "Movies")); err != nil {
		t.Fatal(err)
	}

	libs, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(libs) != 2 {
		t.Fatalf("expected 2 libraries, got %d", len(libs))
	}
	// Sorted by name
	if libs[0].Name != "Anime" {
		t.Errorf("expected first library 'Anime', got %q", libs[0].Name)
	}
}

func TestLibraryRepository_Update(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewLibraryRepository(database)

	lib := newTestLibrary("lib-1", "Movies")
	if err := repo.Create(context.Background(), lib); err != nil {
		t.Fatal(err)
	}

	lib.Name = "Updated Movies"
	lib.Paths = []string{"/new/path"}
	lib.UpdatedAt = time.Now()

	if err := repo.Update(context.Background(), lib); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := repo.GetByID(context.Background(), "lib-1")
	if got.Name != "Updated Movies" {
		t.Errorf("expected 'Updated Movies', got %q", got.Name)
	}
	if len(got.Paths) != 1 || got.Paths[0] != "/new/path" {
		t.Errorf("expected paths [/new/path], got %v", got.Paths)
	}
}

func TestLibraryRepository_Update_NotFound(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewLibraryRepository(database)

	lib := newTestLibrary("nonexistent", "X")
	err := repo.Update(context.Background(), lib)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestLibraryRepository_Delete(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewLibraryRepository(database)

	lib := newTestLibrary("lib-del", "Delete Me")
	if err := repo.Create(context.Background(), lib); err != nil {
		t.Fatal(err)
	}

	if err := repo.Delete(context.Background(), "lib-del"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := repo.GetByID(context.Background(), "lib-del")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Error("library should be deleted")
	}
}

func TestLibraryRepository_Delete_NotFound(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewLibraryRepository(database)

	err := repo.Delete(context.Background(), "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestLibraryRepository_Access(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	userRepo := db.NewUserRepository(database)

	// Seed a user
	if err := userRepo.Create(context.Background(), &db.User{
		ID: "user-1", Username: "alice", DisplayName: "Alice",
		PasswordHash: "$2a$10$fakehash", Role: "user", IsActive: true,
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	lib1 := newTestLibrary("lib-1", "Movies")
	lib2 := newTestLibrary("lib-2", "Shows")
	lib2.ContentType = "shows"
	if err := libRepo.Create(context.Background(), lib1); err != nil {
		t.Fatal(err)
	}
	if err := libRepo.Create(context.Background(), lib2); err != nil {
		t.Fatal(err)
	}

	// No access restrictions: user sees all libraries
	libs, err := libRepo.ListForUser(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(libs) != 2 {
		t.Errorf("expected 2 libraries with no restrictions, got %d", len(libs))
	}

	// Grant access to only lib-1
	if err := libRepo.GrantAccess(context.Background(), "user-1", "lib-1"); err != nil {
		t.Fatalf("grant access: %v", err)
	}

	libs, err = libRepo.ListForUser(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(libs) != 2 {
		// lib-2 has no access entries so it's open to all, lib-1 has explicit grant
		t.Errorf("expected 2 libraries, got %d", len(libs))
	}

	// Revoke access
	if err := libRepo.RevokeAccess(context.Background(), "user-1", "lib-1"); err != nil {
		t.Fatalf("revoke access: %v", err)
	}
}
