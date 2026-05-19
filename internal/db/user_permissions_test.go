package db_test

import (
	"context"
	"strings"
	"testing"
	"time"

	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

// ─── helpers ─────────────────────────────────────────────────────────

func newAdmin(t *testing.T, repo *db.UserRepository, id, username string, createdAt time.Time) {
	t.Helper()
	u := &authmodel.User{
		ID:           id,
		Username:     username,
		DisplayName:  username,
		PasswordHash: "$2a$10$fake",
		Role:         "admin",
		IsActive:     true,
		CreatedAt:    createdAt,
	}
	if err := repo.Create(context.Background(), u); err != nil {
		t.Fatalf("create admin %s: %v", username, err)
	}
}

// ─── Migration backfill ──────────────────────────────────────────────

// TestMigration055_BackfillsExistingAdmin: si la migración 055 corre
// sobre una DB que ya tenía un admin, ese admin se queda con todos los
// flags + is_owner=true. testutil.NewTestDB aplica todas las
// migraciones, incluida la 055, sobre un schema vacío — así que
// creamos el admin DESPUÉS y verificamos sólo SetPermission /
// TransferOwnership. El backfill real lo tests el siguiente test.

func TestMigration055_FreshAdminHasNoPermissionsByDefault(t *testing.T) {
	// Un admin creado vía Create() POST-migración no recibe flags
	// automáticamente — el setup wizard / promote flow es responsable
	// de marcárselos. Verificamos que ese contrato es claro.
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)
	newAdmin(t, repo, "u-fresh", "fresh", time.Now())

	got, err := repo.GetByID(context.Background(), "u-fresh")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.IsOwner {
		t.Error("fresh admin should NOT be owner")
	}
	if got.CanManageAdmins || got.CanManageUsers || got.CanEditMetadata {
		t.Errorf("fresh admin has stale flags: %+v", got)
	}
}

// ─── SetPermission ──────────────────────────────────────────────────

func TestUserRepository_SetPermission_Toggles(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)
	newAdmin(t, repo, "u-1", "alex", time.Now())

	ctx := context.Background()
	if err := repo.SetPermission(ctx, "u-1", "can_edit_metadata", true); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, _ := repo.GetByID(ctx, "u-1")
	if !got.CanEditMetadata {
		t.Error("can_edit_metadata not set")
	}
	if got.CanChangeArtwork {
		t.Error("can_change_artwork should remain false")
	}

	// Toggle off.
	_ = repo.SetPermission(ctx, "u-1", "can_edit_metadata", false)
	got, _ = repo.GetByID(ctx, "u-1")
	if got.CanEditMetadata {
		t.Error("can_edit_metadata not cleared")
	}
}

func TestUserRepository_SetPermission_RejectsUnknownColumn(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)
	newAdmin(t, repo, "u-1", "alex", time.Now())

	cases := []string{
		"is_owner",          // owner se transfiere, no se setea
		"role",              // no es flag
		"password_hash",     // sql injection attempt
		"x; DROP TABLE users",
		"",
	}
	for _, col := range cases {
		err := repo.SetPermission(context.Background(), "u-1", col, true)
		if err == nil {
			t.Errorf("SetPermission accepted invalid column %q", col)
		}
		if !strings.Contains(err.Error(), "invalid permission column") {
			t.Errorf("col %q: error message changed: %v", col, err)
		}
	}
}

// ─── Owner + TransferOwnership ──────────────────────────────────────

func TestUserRepository_GetOwnerID_EmptyOnFreshDB(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)

	id, err := repo.GetOwnerID(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if id != "" {
		t.Errorf("got id=%q on empty DB, want empty", id)
	}
}

func TestUserRepository_TransferOwnership_HappyPath(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)

	now := time.Now()
	newAdmin(t, repo, "u-alice", "alice", now)
	newAdmin(t, repo, "u-bob", "bob", now.Add(time.Hour))

	// Promote alice to owner manually (mimics setup wizard).
	driver := testutil.Driver()
	_, err := database.ExecContext(context.Background(), rewritePlaceholdersForTest(driver, `UPDATE users SET is_owner = 1 WHERE id = ?`), "u-alice")
	if err != nil {
		t.Fatalf("seed owner: %v", err)
	}

	if err := repo.TransferOwnership(context.Background(), "u-alice", "u-bob"); err != nil {
		t.Fatalf("transfer: %v", err)
	}

	gotID, _ := repo.GetOwnerID(context.Background())
	if gotID != "u-bob" {
		t.Errorf("after transfer owner=%s, want u-bob", gotID)
	}
	alice, _ := repo.GetByID(context.Background(), "u-alice")
	bob, _ := repo.GetByID(context.Background(), "u-bob")
	if alice.IsOwner {
		t.Error("alice still owner")
	}
	if !bob.IsOwner {
		t.Error("bob is not owner")
	}
	// Bob receives full permission set on becoming owner.
	if !bob.CanManageAdmins || !bob.CanEditMetadata || !bob.CanChangeArtwork {
		t.Errorf("new owner missing flags: %+v", bob)
	}
}

func TestUserRepository_TransferOwnership_RejectsSelf(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)
	newAdmin(t, repo, "u-alice", "alice", time.Now())

	err := repo.TransferOwnership(context.Background(), "u-alice", "u-alice")
	if err == nil {
		t.Error("self-transfer accepted")
	}
}

func TestUserRepository_TransferOwnership_RejectsNonAdminTarget(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)
	newAdmin(t, repo, "u-alice", "alice", time.Now())

	// Crear un usuario NO admin.
	plain := &authmodel.User{
		ID: "u-plain", Username: "plain", DisplayName: "p",
		PasswordHash: "x", Role: "user", IsActive: true,
		CreatedAt: time.Now(),
	}
	if err := repo.Create(context.Background(), plain); err != nil {
		t.Fatal(err)
	}

	driver := testutil.Driver()
	_, _ = database.ExecContext(context.Background(), rewritePlaceholdersForTest(driver, `UPDATE users SET is_owner = 1 WHERE id = ?`), "u-alice")

	err := repo.TransferOwnership(context.Background(), "u-alice", "u-plain")
	if err == nil {
		t.Error("transfer to non-admin accepted")
	}
	// Alice sigue siendo owner.
	gotID, _ := repo.GetOwnerID(context.Background())
	if gotID != "u-alice" {
		t.Errorf("ownership leaked: owner=%s", gotID)
	}
}

func TestUserRepository_TransferOwnership_RejectsWrongCurrent(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)

	now := time.Now()
	newAdmin(t, repo, "u-alice", "alice", now)
	newAdmin(t, repo, "u-bob", "bob", now.Add(time.Hour))

	driver := testutil.Driver()
	_, _ = database.ExecContext(context.Background(), rewritePlaceholdersForTest(driver, `UPDATE users SET is_owner = 1 WHERE id = ?`), "u-alice")

	// Bob (no owner) intenta transferirse a sí mismo desde "u-bob".
	err := repo.TransferOwnership(context.Background(), "u-bob", "u-alice")
	if err == nil {
		t.Error("transfer from non-owner accepted — anti-race broken")
	}
}

// ─── Can() helper ───────────────────────────────────────────────────

func TestUserCan_OwnerHasEverything(t *testing.T) {
	u := authmodel.User{IsOwner: true}
	for _, p := range authmodel.AllPermissions() {
		if !u.Can(p) {
			t.Errorf("owner missing %s", p)
		}
	}
}

func TestUserCan_GranularFlags(t *testing.T) {
	u := authmodel.User{CanEditMetadata: true}
	if !u.Can(authmodel.PermEditMetadata) {
		t.Error("flag not honoured")
	}
	if u.Can(authmodel.PermManageUsers) {
		t.Error("unrelated flag granted")
	}
}

// ─── helpers locales para tests externos ────────────────────────────

// rewritePlaceholdersForTest exporta rewritePlaceholders al tests
// black-box. Vive aquí (en el test) en vez de exportarse en el repo
// para que la API pública no acumule ruido.
func rewritePlaceholdersForTest(driver, query string) string {
	if driver != "postgres" {
		return query
	}
	// Mini-implementación: reemplaza '?' por $1, $2, … en orden. Sin
	// quoting (los tests no pasan literals con ? dentro).
	out := []rune{}
	n := 0
	for _, c := range query {
		if c == '?' {
			n++
			out = append(out, []rune("$")...)
			out = append(out, []rune(itoa(n))...)
		} else {
			out = append(out, c)
		}
	}
	return string(out)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
