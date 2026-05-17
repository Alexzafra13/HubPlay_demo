package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	authmodel "hubplay/internal/auth/model"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/db"
	"hubplay/internal/domain"
	"hubplay/internal/testutil"
)

func newTestLibrary(id, name string) *librarymodel.Library {
	now := time.Now()
	return &librarymodel.Library{
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
	repo := db.NewLibraryRepository(testutil.Driver(), database)

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
	repo := db.NewLibraryRepository(testutil.Driver(), database)

	_, err := repo.GetByID(context.Background(), "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestLibraryRepository_List(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewLibraryRepository(testutil.Driver(), database)

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
	repo := db.NewLibraryRepository(testutil.Driver(), database)

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
	repo := db.NewLibraryRepository(testutil.Driver(), database)

	lib := newTestLibrary("nonexistent", "X")
	err := repo.Update(context.Background(), lib)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestLibraryRepository_Delete(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewLibraryRepository(testutil.Driver(), database)

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
	repo := db.NewLibraryRepository(testutil.Driver(), database)

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
	libRepo := db.NewLibraryRepository(testutil.Driver(), database)
	userRepo := db.NewUserRepository(testutil.Driver(), database)

	if err := userRepo.Create(context.Background(), &authmodel.User{
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
	libRepo := db.NewLibraryRepository(testutil.Driver(), database)
	userRepo := db.NewUserRepository(testutil.Driver(), database)
	ctx := context.Background()

	now := time.Now()
	parent := &authmodel.User{
		ID: "u-parent", Username: "juanito", DisplayName: "Juanito",
		PasswordHash: "$2a$10$fakehash", Role: "user", IsActive: true,
		CreatedAt: now,
	}
	if err := userRepo.Create(ctx, parent); err != nil {
		t.Fatal(err)
	}
	// Crear profile directo via SQL — el repo expone CreateProfile en
	// user_repository.go pero para mantener este test minimal lo
	// hacemos inline. testutil.Exec rewrites `?` → `$N` so the same
	// fixture works against either backend; `TRUE` is portable since
	// SQLite 3.23 + Postgres always supported it.
	testutil.Exec(t, database, `
		INSERT INTO users (id, username, display_name, password_hash, role,
		                   is_active, created_at, parent_user_id)
		VALUES (?, ?, ?, ?, 'user', TRUE, ?, ?)
	`, "u-child", "maria", "María", "$2a$10$fakehash", now, "u-parent")

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

// TestLibraryRepository_ListAccessByUser cubre el surface admin-only:
// devuelve los library_ids con grant explícito para el user. Bypassea
// el predicate (no resuelve profiles) porque el admin matrix tiene que
// pintar exactamente lo que hay en library_access.
func TestLibraryRepository_ListAccessByUser(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(testutil.Driver(), database)
	userRepo := db.NewUserRepository(testutil.Driver(), database)
	ctx := context.Background()

	now := time.Now()
	user := &authmodel.User{
		ID: "u-1", Username: "alice", DisplayName: "Alice",
		PasswordHash: "$2a$10$fakehash", Role: "user", IsActive: true,
		CreatedAt: now,
	}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"lib-z", "lib-a", "lib-m"} {
		if err := libRepo.Create(ctx, newTestLibrary(id, id)); err != nil {
			t.Fatal(err)
		}
	}

	// Sin grants → slice vacío.
	ids, err := libRepo.ListAccessByUser(ctx, "u-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("expected 0 ids, got %v", ids)
	}

	// Con grants → ordenados por library_id (predecible para tests).
	for _, id := range []string{"lib-z", "lib-a"} {
		if err := libRepo.GrantAccess(ctx, "u-1", id); err != nil {
			t.Fatal(err)
		}
	}
	ids, err = libRepo.ListAccessByUser(ctx, "u-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"lib-a", "lib-z"}
	if len(ids) != len(want) || ids[0] != want[0] || ids[1] != want[1] {
		t.Errorf("expected %v, got %v", want, ids)
	}

	// User sin filas en library_access (no existe siquiera): slice vacío,
	// no error. El admin matrix usa esto para "user nuevo, sin grants".
	ids, err = libRepo.ListAccessByUser(ctx, "u-ghost")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("unknown user: expected 0 ids, got %v", ids)
	}
}

// TestLibraryRepository_ReplaceAccess valida que el diff transaccional
// añade lo nuevo, borra lo sobrante y deja intactos los rows ya
// presentes. La operación es idempotente: pasar el mismo set dos veces
// no debería tocar nada.
func TestLibraryRepository_ReplaceAccess(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(testutil.Driver(), database)
	userRepo := db.NewUserRepository(testutil.Driver(), database)
	ctx := context.Background()

	now := time.Now()
	if err := userRepo.Create(ctx, &authmodel.User{
		ID: "u-1", Username: "alice", DisplayName: "Alice",
		PasswordHash: "$2a$10$fakehash", Role: "user", IsActive: true,
		CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"lib-a", "lib-b", "lib-c", "lib-d"} {
		if err := libRepo.Create(ctx, newTestLibrary(id, id)); err != nil {
			t.Fatal(err)
		}
	}

	// Estado inicial: grant a {a, b}.
	for _, id := range []string{"lib-a", "lib-b"} {
		if err := libRepo.GrantAccess(ctx, "u-1", id); err != nil {
			t.Fatal(err)
		}
	}

	// Replace con {b, c, d}: a sale, c/d entran, b queda.
	if err := libRepo.ReplaceAccess(ctx, "u-1", []string{"lib-b", "lib-c", "lib-d"}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	got, err := libRepo.ListAccessByUser(ctx, "u-1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"lib-b", "lib-c", "lib-d"}
	if !equalStringSets(got, want) {
		t.Errorf("after replace: expected %v, got %v", want, got)
	}

	// Idempotente: misma llamada no rompe nada.
	if err := libRepo.ReplaceAccess(ctx, "u-1", []string{"lib-b", "lib-c", "lib-d"}); err != nil {
		t.Fatalf("replace (idempotent): %v", err)
	}
	got, _ = libRepo.ListAccessByUser(ctx, "u-1")
	if !equalStringSets(got, want) {
		t.Errorf("idempotent replace: expected %v, got %v", want, got)
	}

	// Set vacío limpia todo.
	if err := libRepo.ReplaceAccess(ctx, "u-1", []string{}); err != nil {
		t.Fatalf("replace empty: %v", err)
	}
	got, _ = libRepo.ListAccessByUser(ctx, "u-1")
	if len(got) != 0 {
		t.Errorf("replace empty: expected 0 ids, got %v", got)
	}
}

// TestLibraryRepository_Create_GrantsPrimaryAdmin pins el invariante
// "el admin principal ve toda biblioteca creada después de
// migración 041". El runtime hook en Create otorga library_access
// dentro de la misma tx, así que la matriz UI se mantiene consistente
// con LIST y el grant queda persistido (multi-dispositivo).
func TestLibraryRepository_Create_GrantsPrimaryAdmin(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(testutil.Driver(), database)
	userRepo := db.NewUserRepository(testutil.Driver(), database)
	ctx := context.Background()

	now := time.Now()
	if err := userRepo.Create(ctx, &authmodel.User{
		ID: "admin-1", Username: "boss", DisplayName: "Boss",
		PasswordHash: "$2a$10$fakehash", Role: "admin", IsActive: true,
		CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	if err := libRepo.Create(ctx, newTestLibrary("lib-1", "Movies")); err != nil {
		t.Fatal(err)
	}

	ids, err := libRepo.ListAccessByUser(ctx, "admin-1")
	if err != nil {
		t.Fatalf("ListAccessByUser: %v", err)
	}
	if len(ids) != 1 || ids[0] != "lib-1" {
		t.Errorf("primary admin should have grant for lib-1, got %v", ids)
	}

	// Crear más libraries añade más grants sin tocar los previos.
	if err := libRepo.Create(ctx, newTestLibrary("lib-2", "Shows")); err != nil {
		t.Fatal(err)
	}
	ids, _ = libRepo.ListAccessByUser(ctx, "admin-1")
	if !equalStringSets(ids, []string{"lib-1", "lib-2"}) {
		t.Errorf("primary admin should have grants for both libs, got %v", ids)
	}
}

// TestLibraryRepository_Create_NoAdmin_NoOp valida que crear bibliotecas
// ANTES de que exista cualquier admin no rompe (LIMIT 1 sobre 0 rows =
// 0 inserts) y no deja grants huérfanos. Caso edge: setup wizard
// inicializa el operador admin DESPUÉS del primer arranque; si por
// alguna razón se hubiese inicializado una library antes, la
// migración 041 + el siguiente Create resincronizan el estado.
func TestLibraryRepository_Create_NoAdmin_NoOp(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(testutil.Driver(), database)
	ctx := context.Background()

	if err := libRepo.Create(ctx, newTestLibrary("lib-1", "Movies")); err != nil {
		t.Fatalf("Create without admin should not error: %v", err)
	}

	var count int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM library_access`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected library_access empty without admin, got %d rows", count)
	}
}

// TestLibraryRepository_Create_OnlyPrimaryAdmin valida que con varios
// admins sólo el más antiguo (PrimaryAdminID) recibe el grant
// automático. Los demás siguen viendo libraries por el bypass de LIST,
// pero su matriz UI refleja honestamente que no tienen grants
// explícitos (decisión consensuada — admins secundarios se gestionan
// manualmente).
func TestLibraryRepository_Create_OnlyPrimaryAdmin(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(testutil.Driver(), database)
	userRepo := db.NewUserRepository(testutil.Driver(), database)
	ctx := context.Background()

	older := time.Now()
	newer := older.Add(time.Hour)
	if err := userRepo.Create(ctx, &authmodel.User{
		ID: "admin-old", Username: "founder", DisplayName: "Founder",
		PasswordHash: "$2a$10$fakehash", Role: "admin", IsActive: true,
		CreatedAt: older,
	}); err != nil {
		t.Fatal(err)
	}
	if err := userRepo.Create(ctx, &authmodel.User{
		ID: "admin-new", Username: "second", DisplayName: "Second",
		PasswordHash: "$2a$10$fakehash", Role: "admin", IsActive: true,
		CreatedAt: newer,
	}); err != nil {
		t.Fatal(err)
	}

	if err := libRepo.Create(ctx, newTestLibrary("lib-1", "Movies")); err != nil {
		t.Fatal(err)
	}

	gotOld, _ := libRepo.ListAccessByUser(ctx, "admin-old")
	if len(gotOld) != 1 || gotOld[0] != "lib-1" {
		t.Errorf("oldest admin should be granted, got %v", gotOld)
	}
	gotNew, _ := libRepo.ListAccessByUser(ctx, "admin-new")
	if len(gotNew) != 0 {
		t.Errorf("secondary admin should NOT receive auto-grant, got %v", gotNew)
	}
}

// TestMigration041_BackfillsPrimaryAdmin simula el caso real que
// motivó la migración: admin + libraries pre-existentes en producción
// (creados antes de Phase B) sin grants en library_access. La
// migración aplica el INSERT OR IGNORE y restaura la invariante.
//
// NewTestDB ya aplica 041, así que reproducimos el estado "viejo"
// borrando library_access manualmente y volvemos a correr el SQL de
// la migración.
func TestMigration041_BackfillsPrimaryAdmin(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(testutil.Driver(), database)
	userRepo := db.NewUserRepository(testutil.Driver(), database)
	ctx := context.Background()

	// The two migration dialects do the same thing with different
	// "ignore-on-conflict" syntax. Mirroring both keeps this test
	// honest on the pg matrix.
	backfillSQL := `
		INSERT OR IGNORE INTO library_access (user_id, library_id)
		SELECT primary_admin.id, l.id
		FROM libraries l
		CROSS JOIN (
			SELECT id FROM users
			WHERE role = 'admin' AND parent_user_id IS NULL
			ORDER BY created_at ASC
			LIMIT 1
		) AS primary_admin
	`
	if db.IsPostgres(testutil.Driver()) {
		backfillSQL = `
			INSERT INTO library_access (user_id, library_id)
			SELECT primary_admin.id, l.id
			FROM libraries l
			CROSS JOIN (
				SELECT id FROM users
				WHERE role = 'admin' AND parent_user_id IS NULL
				ORDER BY created_at ASC
				LIMIT 1
			) AS primary_admin
			ON CONFLICT DO NOTHING
		`
	}

	now := time.Now()
	if err := userRepo.Create(ctx, &authmodel.User{
		ID: "admin-1", Username: "boss", DisplayName: "Boss",
		PasswordHash: "$2a$10$fakehash", Role: "admin", IsActive: true,
		CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"lib-a", "lib-b", "lib-c"} {
		if err := libRepo.Create(ctx, newTestLibrary(id, id)); err != nil {
			t.Fatal(err)
		}
	}

	// Estado "pre-041": el runtime hook ya insertó grants, los
	// borramos para simular un despliegue legacy.
	if _, err := database.ExecContext(ctx, `DELETE FROM library_access`); err != nil {
		t.Fatal(err)
	}
	pre, _ := libRepo.ListAccessByUser(ctx, "admin-1")
	if len(pre) != 0 {
		t.Fatalf("setup precondition: expected empty library_access, got %v", pre)
	}

	// Re-correr el SQL idéntico al de migrations/{sqlite,postgres}/041.
	if _, err := database.ExecContext(ctx, backfillSQL); err != nil {
		t.Fatalf("migration 041 SQL: %v", err)
	}

	post, err := libRepo.ListAccessByUser(ctx, "admin-1")
	if err != nil {
		t.Fatal(err)
	}
	if !equalStringSets(post, []string{"lib-a", "lib-b", "lib-c"}) {
		t.Errorf("after backfill: expected grants for all 3 libs, got %v", post)
	}

	// Idempotente: re-correr la migración no duplica filas.
	if _, err := database.ExecContext(ctx, backfillSQL); err != nil {
		t.Fatalf("migration 041 SQL (re-run): %v", err)
	}
	again, _ := libRepo.ListAccessByUser(ctx, "admin-1")
	if !equalStringSets(again, []string{"lib-a", "lib-b", "lib-c"}) {
		t.Errorf("re-run should be idempotent, got %v", again)
	}
}

// TestLibraryRepository_CreateWithGrant verifica que la creación
// atómica (lib + grant) deja al owner accediendo a la nueva
// biblioteca y que un usuario distinto sigue sin acceso. Cubre el
// path feliz del shortcut "lista IPTV personal" del admin.
func TestLibraryRepository_CreateWithGrant(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(testutil.Driver(), database)
	userRepo := db.NewUserRepository(testutil.Driver(), database)
	ctx := context.Background()

	now := time.Now()
	for _, id := range []string{"u-owner", "u-other"} {
		if err := userRepo.Create(ctx, &authmodel.User{
			ID: id, Username: id, DisplayName: id,
			PasswordHash: "$2a$10$fakehash", Role: "user", IsActive: true,
			CreatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}

	lib := newTestLibrary("lib-personal", "Lista de Owner")
	lib.ContentType = "livetv"
	lib.M3UURL = "https://example.com/owner.m3u"
	lib.Paths = nil

	if err := libRepo.CreateWithGrant(ctx, lib, "u-owner"); err != nil {
		t.Fatalf("CreateWithGrant: %v", err)
	}

	if has, _ := libRepo.UserHasAccess(ctx, "u-owner", "lib-personal"); !has {
		t.Error("owner must have access after CreateWithGrant")
	}
	if has, _ := libRepo.UserHasAccess(ctx, "u-other", "lib-personal"); has {
		t.Error("non-owner must NOT have access — the whole point of the shortcut")
	}

	libs, err := libRepo.ListForUser(ctx, "u-owner")
	if err != nil {
		t.Fatal(err)
	}
	if len(libs) != 1 || libs[0].ID != "lib-personal" {
		t.Errorf("owner's library list: %v", libs)
	}
	if libs[0].M3UURL != "https://example.com/owner.m3u" {
		t.Errorf("M3U URL roundtrip: %q", libs[0].M3UURL)
	}
}

// CreateWithGrant against an unknown user_id must fail with a FK
// violation and leave the libraries table clean. Otherwise the admin
// could end up with phantom libraries pointing at deleted accounts.
func TestLibraryRepository_CreateWithGrant_UnknownUser_Rollback(t *testing.T) {
	database := testutil.NewTestDB(t)
	libRepo := db.NewLibraryRepository(testutil.Driver(), database)
	ctx := context.Background()

	lib := newTestLibrary("lib-ghost", "Ghost")
	lib.ContentType = "livetv"
	lib.M3UURL = "https://example.com/x.m3u"
	lib.Paths = nil

	if err := libRepo.CreateWithGrant(ctx, lib, "u-does-not-exist"); err == nil {
		t.Fatal("expected FK violation, got nil")
	}

	if _, err := libRepo.GetByID(ctx, "lib-ghost"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected library rollback, got err=%v", err)
	}
}

func equalStringSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]struct{}, len(a))
	for _, s := range a {
		seen[s] = struct{}{}
	}
	for _, s := range b {
		if _, ok := seen[s]; !ok {
			return false
		}
	}
	return true
}
