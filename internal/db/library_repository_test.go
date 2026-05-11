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

// TestLibraryRepository_Access pins el modelo strict post-migración
// 040: necesita grant explícito; no hay fallback "público por
// defecto". Los profiles (parent_user_id != NULL) heredan acceso del
// parent — se cubre en TestLibraryRepository_Access_ProfileInherits.
func TestLibraryRepository_Access(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	userRepo := db.NewUserRepository(database)

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

	// Strict: sin grants, ListForUser devuelve 0.
	libs, err := libRepo.ListForUser(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(libs) != 0 {
		t.Errorf("strict mode: expected 0 libraries without grants, got %d", len(libs))
	}
	if has, _ := libRepo.UserHasAccess(context.Background(), "user-1", "lib-1"); has {
		t.Error("UserHasAccess must be false without an explicit grant")
	}

	// Grant a lib-1: ListForUser devuelve sólo lib-1.
	if err := libRepo.GrantAccess(context.Background(), "user-1", "lib-1"); err != nil {
		t.Fatalf("grant access: %v", err)
	}
	libs, err = libRepo.ListForUser(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(libs) != 1 || libs[0].ID != "lib-1" {
		t.Errorf("expected only lib-1, got %v", libs)
	}
	if has, _ := libRepo.UserHasAccess(context.Background(), "user-1", "lib-1"); !has {
		t.Error("UserHasAccess must be true after grant")
	}
	if has, _ := libRepo.UserHasAccess(context.Background(), "user-1", "lib-2"); has {
		t.Error("UserHasAccess for ungranted library must remain false")
	}

	// Revoke: vuelve a 0.
	if err := libRepo.RevokeAccess(context.Background(), "user-1", "lib-1"); err != nil {
		t.Fatalf("revoke access: %v", err)
	}
	libs, _ = libRepo.ListForUser(context.Background(), "user-1")
	if len(libs) != 0 {
		t.Errorf("post-revoke: expected 0 libraries, got %d", len(libs))
	}
}

// TestLibraryRepository_Access_ProfileInherits valida que un profile
// (parent_user_id != NULL) ve exactamente lo mismo que su parent
// porque el predicate consulta COALESCE(parent_user_id, id). Los
// "miembros del hogar" son profiles bajo el top-level user.
func TestLibraryRepository_Access_ProfileInherits(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(database)
	userRepo := db.NewUserRepository(database)
	ctx := context.Background()

	now := time.Now()
	parent := &db.User{
		ID: "u-parent", Username: "juanito", DisplayName: "Juanito",
		PasswordHash: "$2a$10$fakehash", Role: "user", IsActive: true,
		CreatedAt: now,
	}
	if err := userRepo.Create(ctx, parent); err != nil {
		t.Fatal(err)
	}
	// Crear profile directo via SQL — el repo expone CreateProfile en
	// user_repository.go pero para mantener este test minimal lo
	// hacemos inline.
	if _, err := database.ExecContext(ctx, `
		INSERT INTO users (id, username, display_name, password_hash, role,
		                   is_active, created_at, parent_user_id)
		VALUES (?, ?, ?, ?, 'user', 1, ?, ?)
	`, "u-child", "maria", "María", "$2a$10$fakehash", now, "u-parent"); err != nil {
		t.Fatal(err)
	}

	lib := newTestLibrary("lib-tv", "Canales Juanito")
	lib.ContentType = "livetv"
	if err := libRepo.Create(ctx, lib); err != nil {
		t.Fatal(err)
	}

	// Grant SOLO al parent. El profile NUNCA aparece en library_access.
	if err := libRepo.GrantAccess(ctx, "u-parent", "lib-tv"); err != nil {
		t.Fatal(err)
	}

	// Tanto el parent como el profile deben tener acceso.
	for _, uid := range []string{"u-parent", "u-child"} {
		has, err := libRepo.UserHasAccess(ctx, uid, "lib-tv")
		if err != nil {
			t.Fatalf("UserHasAccess(%s): %v", uid, err)
		}
		if !has {
			t.Errorf("%s must inherit access to lib-tv via parent grant", uid)
		}
		libs, err := libRepo.ListForUser(ctx, uid)
		if err != nil {
			t.Fatalf("ListForUser(%s): %v", uid, err)
		}
		if len(libs) != 1 || libs[0].ID != "lib-tv" {
			t.Errorf("%s ListForUser: expected [lib-tv], got %v", uid, libs)
		}
	}

	// Revoke al parent → el profile también pierde acceso al instante.
	if err := libRepo.RevokeAccess(ctx, "u-parent", "lib-tv"); err != nil {
		t.Fatal(err)
	}
	for _, uid := range []string{"u-parent", "u-child"} {
		has, _ := libRepo.UserHasAccess(ctx, uid, "lib-tv")
		if has {
			t.Errorf("%s should lose access when parent grant is revoked", uid)
		}
	}
}
