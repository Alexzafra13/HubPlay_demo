package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/db"
	"hubplay/internal/domain"
	"hubplay/internal/testutil"
)

func createTestUser(t *testing.T, repo *db.UserRepository, username string) *authmodel.User {
	t.Helper()
	u := &authmodel.User{
		ID:           "user-" + username,
		Username:     username,
		DisplayName:  username,
		PasswordHash: "$2a$10$fakehash",
		Role:         "user",
		IsActive:     true,
		CreatedAt:    time.Now(),
	}
	if err := repo.Create(context.Background(), u); err != nil {
		t.Fatalf("creating test user: %v", err)
	}
	return u
}

func TestUserRepository_Create_And_GetByID(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)

	u := createTestUser(t, repo, "alex")

	got, err := repo.GetByID(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Username != "alex" {
		t.Errorf("expected username 'alex', got %q", got.Username)
	}
	if got.Role != "user" {
		t.Errorf("expected role 'user', got %q", got.Role)
	}
	if !got.IsActive {
		t.Error("expected user to be active")
	}
}

func TestUserRepository_GetByID_NotFound(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)

	_, err := repo.GetByID(context.Background(), "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUserRepository_GetByUsername(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)

	createTestUser(t, repo, "maria")

	got, err := repo.GetByUsername(context.Background(), "maria")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Username != "maria" {
		t.Errorf("expected username 'maria', got %q", got.Username)
	}
}

func TestUserRepository_GetByUsername_NotFound(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)

	_, err := repo.GetByUsername(context.Background(), "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUserRepository_List(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)

	createTestUser(t, repo, "alice")
	createTestUser(t, repo, "bob")
	createTestUser(t, repo, "charlie")

	users, total, err := repo.List(context.Background(), 10, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 3 {
		t.Errorf("expected 3 total, got %d", total)
	}
	if len(users) != 3 {
		t.Errorf("expected 3 users, got %d", len(users))
	}

	// Should be sorted by username
	if users[0].Username != "alice" {
		t.Errorf("expected first user 'alice', got %q", users[0].Username)
	}
}

func TestUserRepository_List_Pagination(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)

	createTestUser(t, repo, "alice")
	createTestUser(t, repo, "bob")
	createTestUser(t, repo, "charlie")

	users, total, err := repo.List(context.Background(), 2, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 3 {
		t.Errorf("expected 3 total, got %d", total)
	}
	if len(users) != 2 {
		t.Errorf("expected 2 users in page, got %d", len(users))
	}

	users2, _, err := repo.List(context.Background(), 2, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(users2) != 1 {
		t.Errorf("expected 1 user in second page, got %d", len(users2))
	}
}

func TestUserRepository_Update(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)

	u := createTestUser(t, repo, "alex")
	u.DisplayName = "Alejandro"
	u.Role = "admin"

	if err := repo.Update(context.Background(), u); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := repo.GetByID(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.DisplayName != "Alejandro" {
		t.Errorf("expected display name 'Alejandro', got %q", got.DisplayName)
	}
	if got.Role != "admin" {
		t.Errorf("expected role 'admin', got %q", got.Role)
	}
}

func TestUserRepository_Delete(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)

	u := createTestUser(t, repo, "alex")

	if err := repo.Delete(context.Background(), u.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := repo.GetByID(context.Background(), u.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestUserRepository_Delete_NotFound(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)

	err := repo.Delete(context.Background(), "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUserRepository_Count(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)

	count, err := repo.Count(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 users, got %d", count)
	}

	createTestUser(t, repo, "alex")
	createTestUser(t, repo, "maria")

	count, err = repo.Count(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 users, got %d", count)
	}
}

// TestUserRepository_ListProfilesForOwner pins the regression that
// motivated the raw-SQL rewrite: sqlc 1.31.x truncated the generated
// query so this endpoint 500'd permanently with "near \"?\": syntax
// error" on every call. The hand-rolled implementation must (a)
// return both the parent and any profiles in one call and (b) order
// the parent first, then profiles alphabetically (case-insensitive).
func TestUserRepository_ListProfilesForOwner(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)
	ctx := context.Background()

	// Parent account.
	parent := &authmodel.User{
		ID: "parent-1", Username: "alex", DisplayName: "Alex",
		PasswordHash: "$2a$10$fake", Role: "admin", IsActive: true,
		CreatedAt: time.Now(),
	}
	if err := repo.Create(ctx, parent); err != nil {
		t.Fatalf("creating parent: %v", err)
	}

	// Three child profiles in non-alphabetical order so we can assert
	// the ORDER BY actually sorts them. Names mix case to exercise
	// LOWER()-based collation.
	for _, p := range []*authmodel.User{
		{ID: "p-charlie", Username: "alex:charlie", DisplayName: "charlie",
			PasswordHash: "", Role: "user", IsActive: true,
			ParentUserID: parent.ID, CreatedAt: time.Now()},
		{ID: "p-Bea", Username: "alex:bea", DisplayName: "Bea",
			PasswordHash: "", Role: "user", IsActive: true,
			ParentUserID: parent.ID, CreatedAt: time.Now()},
		{ID: "p-alma", Username: "alex:alma", DisplayName: "alma",
			PasswordHash: "", Role: "user", IsActive: true,
			ParentUserID: parent.ID, CreatedAt: time.Now()},
	} {
		if err := repo.Create(ctx, p); err != nil {
			t.Fatalf("creating profile %s: %v", p.DisplayName, err)
		}
	}

	got, err := repo.ListProfilesForOwner(ctx, parent.ID)
	if err != nil {
		t.Fatalf("ListProfilesForOwner: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("want 4 rows (parent + 3 profiles), got %d", len(got))
	}

	// Parent must be first regardless of name.
	if got[0].ID != parent.ID {
		t.Errorf("expected parent first, got %s", got[0].ID)
	}
	if got[0].ParentUserID != "" {
		t.Errorf("parent row leaked ParentUserID = %q", got[0].ParentUserID)
	}

	// Profiles must follow case-insensitively: alma, Bea, charlie.
	wantOrder := []string{"alma", "Bea", "charlie"}
	for i, want := range wantOrder {
		if got[i+1].DisplayName != want {
			t.Errorf("position %d: want %q, got %q", i+1, want, got[i+1].DisplayName)
		}
		if got[i+1].ParentUserID != parent.ID {
			t.Errorf("profile %s missing ParentUserID", got[i+1].DisplayName)
		}
	}
}

// TestUserRepository_ListProfilesForOwner_NoProfiles covers the
// degenerate but common case: a single-user install. The query must
// still return the owner row, not nil.
func TestUserRepository_ListProfilesForOwner_NoProfiles(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)
	ctx := context.Background()

	createTestUser(t, repo, "solo")

	got, err := repo.ListProfilesForOwner(ctx, "user-solo")
	if err != nil {
		t.Fatalf("ListProfilesForOwner: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 row (just the owner), got %d", len(got))
	}
	if got[0].Username != "solo" {
		t.Errorf("want 'solo', got %q", got[0].Username)
	}
}

func TestUserRepository_UpdateLastLogin(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)

	u := createTestUser(t, repo, "alex")

	now := time.Now().Truncate(time.Second)
	if err := repo.UpdateLastLogin(context.Background(), u.ID, now); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := repo.GetByID(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.LastLoginAt == nil {
		t.Fatal("expected last_login_at to be set")
	}
}

// ── Upload permission + quota (migration 053) ───────────────────────

// TestUserRepository_UploadDefaults pins el contrato post-053: un user
// recién creado tiene can_upload=false, quota=0, used=0 — el permiso
// se otorga explícitamente, nunca por defecto.
func TestUserRepository_UploadDefaults(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)
	ctx := context.Background()

	u := createTestUser(t, repo, "alex")
	got, err := repo.GetByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.CanUpload {
		t.Error("expected CanUpload=false by default")
	}
	if got.UploadQuotaBytes != 0 {
		t.Errorf("expected UploadQuotaBytes=0, got %d", got.UploadQuotaBytes)
	}
	if got.UploadUsedBytes != 0 {
		t.Errorf("expected UploadUsedBytes=0, got %d", got.UploadUsedBytes)
	}
}

func TestUserRepository_SetCanUpload(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)
	ctx := context.Background()

	u := createTestUser(t, repo, "alex")
	if err := repo.SetCanUpload(ctx, u.ID, true); err != nil {
		t.Fatalf("set true: %v", err)
	}
	got, _ := repo.GetByID(ctx, u.ID)
	if !got.CanUpload {
		t.Error("expected CanUpload=true after set")
	}

	if err := repo.SetCanUpload(ctx, u.ID, false); err != nil {
		t.Fatalf("set false: %v", err)
	}
	got, _ = repo.GetByID(ctx, u.ID)
	if got.CanUpload {
		t.Error("expected CanUpload=false after revoke")
	}
}

func TestUserRepository_SetUploadQuota(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)
	ctx := context.Background()

	u := createTestUser(t, repo, "alex")
	if err := repo.SetUploadQuota(ctx, u.ID, 10*1024*1024*1024); err != nil {
		t.Fatalf("set quota: %v", err)
	}
	got, _ := repo.GetByID(ctx, u.ID)
	if got.UploadQuotaBytes != 10*1024*1024*1024 {
		t.Errorf("got quota %d", got.UploadQuotaBytes)
	}
}

// TestUserRepository_ReserveUploadBytes_HappyPath: con can_upload=true
// y quota suficiente, la reserva incrementa used_bytes.
func TestUserRepository_ReserveUploadBytes_HappyPath(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)
	ctx := context.Background()

	u := createTestUser(t, repo, "alex")
	if err := repo.SetCanUpload(ctx, u.ID, true); err != nil {
		t.Fatalf("set can upload: %v", err)
	}
	if err := repo.SetUploadQuota(ctx, u.ID, 1000); err != nil {
		t.Fatalf("set quota: %v", err)
	}

	if err := repo.ReserveUploadBytes(ctx, u.ID, 400); err != nil {
		t.Fatalf("reserve 400: %v", err)
	}
	got, _ := repo.GetByID(ctx, u.ID)
	if got.UploadUsedBytes != 400 {
		t.Errorf("expected used=400, got %d", got.UploadUsedBytes)
	}

	// Una segunda reserva dentro de cuota debe acumularse.
	if err := repo.ReserveUploadBytes(ctx, u.ID, 300); err != nil {
		t.Fatalf("reserve 300: %v", err)
	}
	got, _ = repo.GetByID(ctx, u.ID)
	if got.UploadUsedBytes != 700 {
		t.Errorf("expected used=700, got %d", got.UploadUsedBytes)
	}
}

// TestUserRepository_ReserveUploadBytes_ExceedsQuota: cuando la
// reserva pasaría de la cuota, el repo devuelve ErrUploadQuotaExceeded
// y el contador NO se mueve.
func TestUserRepository_ReserveUploadBytes_ExceedsQuota(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)
	ctx := context.Background()

	u := createTestUser(t, repo, "alex")
	_ = repo.SetCanUpload(ctx, u.ID, true)
	_ = repo.SetUploadQuota(ctx, u.ID, 1000)
	_ = repo.ReserveUploadBytes(ctx, u.ID, 800)

	err := repo.ReserveUploadBytes(ctx, u.ID, 500)
	if !errors.Is(err, domain.ErrUploadQuotaExceeded) {
		t.Fatalf("want ErrUploadQuotaExceeded, got %v", err)
	}
	got, _ := repo.GetByID(ctx, u.ID)
	if got.UploadUsedBytes != 800 {
		t.Errorf("used_bytes leaked: got %d, want 800", got.UploadUsedBytes)
	}
}

// TestUserRepository_ReserveUploadBytes_PermissionRevoked: aunque la
// cuota daría, can_upload=false debe rechazar la reserva con el mismo
// sentinel. El cliente no distingue (ver doc en errors.go).
func TestUserRepository_ReserveUploadBytes_PermissionRevoked(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)
	ctx := context.Background()

	u := createTestUser(t, repo, "alex")
	// can_upload sigue en false (default).
	_ = repo.SetUploadQuota(ctx, u.ID, 1_000_000)

	err := repo.ReserveUploadBytes(ctx, u.ID, 100)
	if !errors.Is(err, domain.ErrUploadQuotaExceeded) {
		t.Fatalf("want ErrUploadQuotaExceeded, got %v", err)
	}
}

// TestUserRepository_ReleaseUploadBytes: devuelve bytes y, ante una
// llamada que llevaría el contador a negativo, lo deja en 0.
func TestUserRepository_ReleaseUploadBytes(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)
	ctx := context.Background()

	u := createTestUser(t, repo, "alex")
	_ = repo.SetCanUpload(ctx, u.ID, true)
	_ = repo.SetUploadQuota(ctx, u.ID, 1000)
	_ = repo.ReserveUploadBytes(ctx, u.ID, 500)

	if err := repo.ReleaseUploadBytes(ctx, u.ID, 200); err != nil {
		t.Fatalf("release 200: %v", err)
	}
	got, _ := repo.GetByID(ctx, u.ID)
	if got.UploadUsedBytes != 300 {
		t.Errorf("want 300, got %d", got.UploadUsedBytes)
	}

	// Liberar más de lo reservado: el cap en SQL evita negativos.
	if err := repo.ReleaseUploadBytes(ctx, u.ID, 9999); err != nil {
		t.Fatalf("release 9999: %v", err)
	}
	got, _ = repo.GetByID(ctx, u.ID)
	if got.UploadUsedBytes != 0 {
		t.Errorf("expected cap to 0, got %d", got.UploadUsedBytes)
	}

	// delta <= 0 es no-op por contrato — no toca la fila ni 500's.
	if err := repo.ReleaseUploadBytes(ctx, u.ID, 0); err != nil {
		t.Fatalf("release 0: %v", err)
	}
	if err := repo.ReleaseUploadBytes(ctx, u.ID, -5); err != nil {
		t.Fatalf("release negative: %v", err)
	}
}
