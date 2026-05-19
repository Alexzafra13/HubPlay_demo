package user_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/db"
	"hubplay/internal/domain"
	"hubplay/internal/testutil"
	"hubplay/internal/user"
)

// helpers --------------------------------------------------------------------

func newServiceWithUser(t *testing.T) (*user.Service, *db.UserRepository, string) {
	t.Helper()
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(testutil.Driver(), database)
	svc := user.NewService(repo, slog.Default(), "")

	u := &authmodel.User{
		ID:           "u-test",
		Username:     "tester",
		DisplayName:  "Tester",
		PasswordHash: "$2a$10$fake",
		Role:         "user",
		IsActive:     true,
		CreatedAt:    time.Now(),
	}
	if err := repo.Create(context.Background(), u); err != nil {
		t.Fatalf("creating test user: %v", err)
	}
	return svc, repo, u.ID
}

// SetCanUpload ---------------------------------------------------------------

func TestService_SetCanUpload(t *testing.T) {
	svc, repo, id := newServiceWithUser(t)
	ctx := context.Background()

	if err := svc.SetCanUpload(ctx, id, true); err != nil {
		t.Fatalf("set true: %v", err)
	}
	got, _ := repo.GetByID(ctx, id)
	if !got.CanUpload {
		t.Error("expected CanUpload=true after service call")
	}
}

// SetUploadQuota -------------------------------------------------------------

func TestService_SetUploadQuota_HappyPath(t *testing.T) {
	svc, repo, id := newServiceWithUser(t)
	ctx := context.Background()

	if err := svc.SetUploadQuota(ctx, id, 5*1024*1024*1024); err != nil {
		t.Fatalf("set quota: %v", err)
	}
	got, _ := repo.GetByID(ctx, id)
	if got.UploadQuotaBytes != 5*1024*1024*1024 {
		t.Errorf("got %d", got.UploadQuotaBytes)
	}
}

func TestService_SetUploadQuota_RejectsNegative(t *testing.T) {
	svc, _, id := newServiceWithUser(t)
	err := svc.SetUploadQuota(context.Background(), id, -1)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("want ErrValidation, got %v", err)
	}
}

// TestService_SetUploadQuota_RejectsBelowUsed pin la invariante: si el
// usuario ya tiene 700 bytes ocupados, no podemos bajar la cuota a 500
// — quedaría inconsistente. El service detecta y devuelve Conflict.
func TestService_SetUploadQuota_RejectsBelowUsed(t *testing.T) {
	svc, repo, id := newServiceWithUser(t)
	ctx := context.Background()

	// Prep: dar permiso, fijar cuota holgada, reservar 700.
	if err := repo.SetCanUpload(ctx, id, true); err != nil {
		t.Fatalf("set can upload: %v", err)
	}
	if err := repo.SetUploadQuota(ctx, id, 10_000); err != nil {
		t.Fatalf("set initial quota: %v", err)
	}
	if err := repo.ReserveUploadBytes(ctx, id, 700); err != nil {
		t.Fatalf("reserve: %v", err)
	}

	err := svc.SetUploadQuota(ctx, id, 500)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}

	// El valor en disco no debe haber cambiado tras el rechazo.
	got, _ := repo.GetByID(ctx, id)
	if got.UploadQuotaBytes != 10_000 {
		t.Errorf("quota leaked: got %d", got.UploadQuotaBytes)
	}
}

// TestService_SetUploadQuota_ZeroIsValid: ajustar a 0 es legal cuando
// no hay used_bytes — sirve para "congelar" subidas futuras sin tocar
// can_upload.
func TestService_SetUploadQuota_ZeroIsValid(t *testing.T) {
	svc, repo, id := newServiceWithUser(t)
	ctx := context.Background()

	_ = repo.SetCanUpload(ctx, id, true)
	if err := svc.SetUploadQuota(ctx, id, 0); err != nil {
		t.Fatalf("set quota 0: %v", err)
	}
	got, _ := repo.GetByID(ctx, id)
	if got.UploadQuotaBytes != 0 {
		t.Errorf("got %d", got.UploadQuotaBytes)
	}
	if !got.CanUpload {
		t.Error("CanUpload should remain untouched by quota change")
	}
}
