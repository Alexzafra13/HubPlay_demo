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

// TestSessionRepository_RotateRefreshToken pins the rotation
// invariant: the old hash MUST no longer resolve and the new hash
// MUST resolve to the same session row, with bumped last_active_at
// and expires_at. Without this the rotation in /auth/refresh would
// silently regress a future refactor.
func TestSessionRepository_RotateRefreshToken(t *testing.T) {
	database := testutil.NewTestDB(t)
	userRepo := db.NewUserRepository("sqlite", database)
	repo := db.NewSessionRepository("sqlite", database)
	seedUserForSessions(t, userRepo)

	created := time.Now().Add(-time.Hour).Truncate(time.Second)
	original := &db.Session{
		ID:               "session-1",
		UserID:           "user-1",
		DeviceName:       "Chrome",
		DeviceID:         "dev-1",
		RefreshTokenHash: "hash-old",
		CreatedAt:        created,
		LastActiveAt:     created,
		ExpiresAt:        created.Add(720 * time.Hour),
	}
	if err := repo.Create(context.Background(), original); err != nil {
		t.Fatalf("create: %v", err)
	}

	rotateAt := time.Now().Truncate(time.Second)
	newExp := rotateAt.Add(720 * time.Hour)
	if err := repo.RotateRefreshToken(context.Background(), original.ID, "hash-new", rotateAt, newExp); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	if _, err := repo.GetByRefreshTokenHash(context.Background(), "hash-old"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("old hash should be gone, got err=%v", err)
	}

	got, err := repo.GetByRefreshTokenHash(context.Background(), "hash-new")
	if err != nil {
		t.Fatalf("new hash lookup: %v", err)
	}
	if got.ID != original.ID {
		t.Errorf("new hash resolved to different session: %s", got.ID)
	}
	if !got.LastActiveAt.Equal(rotateAt) {
		t.Errorf("LastActiveAt: want %v, got %v", rotateAt, got.LastActiveAt)
	}
	if !got.ExpiresAt.Equal(newExp) {
		t.Errorf("ExpiresAt: want %v, got %v", newExp, got.ExpiresAt)
	}
	if !got.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt should not change: want %v, got %v", created, got.CreatedAt)
	}
	if got.PreviousRefreshTokenHash != "hash-old" {
		t.Errorf("previous hash: want hash-old (the just-rotated value), got %q", got.PreviousRefreshTokenHash)
	}
}

// TestSessionRepository_GetByPreviousRefreshTokenHash covers the
// reverse lookup that powers reuse detection in /auth/refresh: a
// refresh request that hits the previous (already-rotated) hash must
// still resolve to the session, so the service can revoke it.
func TestSessionRepository_GetByPreviousRefreshTokenHash(t *testing.T) {
	database := testutil.NewTestDB(t)
	userRepo := db.NewUserRepository("sqlite", database)
	repo := db.NewSessionRepository("sqlite", database)
	seedUserForSessions(t, userRepo)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	s := &db.Session{
		ID: "session-x", UserID: "user-1", DeviceName: "Chrome", DeviceID: "d",
		RefreshTokenHash: "hash-current", CreatedAt: now, LastActiveAt: now,
		ExpiresAt: now.Add(time.Hour),
	}
	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.RotateRefreshToken(ctx, s.ID, "hash-newer", now, now.Add(2*time.Hour)); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	got, err := repo.GetByPreviousRefreshTokenHash(ctx, "hash-current")
	if err != nil {
		t.Fatalf("lookup by previous hash: %v", err)
	}
	if got.ID != s.ID {
		t.Errorf("want session ID %q, got %q", s.ID, got.ID)
	}

	// An empty string must NOT match never-rotated rows — that
	// would turn every fresh session into a reuse signal.
	if _, err := repo.GetByPreviousRefreshTokenHash(ctx, ""); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("empty previous hash should not match: got err=%v", err)
	}

	// A genuine never-seen hash must also miss.
	if _, err := repo.GetByPreviousRefreshTokenHash(ctx, "hash-stranger"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown hash should miss: got err=%v", err)
	}
}

func seedUserForSessions(t *testing.T, database *db.UserRepository) {
	t.Helper()
	u := &db.User{
		ID:           "user-1",
		Username:     "testuser",
		DisplayName:  "Test",
		PasswordHash: "$2a$10$fakehash",
		Role:         "user",
		IsActive:     true,
		CreatedAt:    time.Now(),
	}
	if err := database.Create(context.Background(), u); err != nil {
		t.Fatalf("creating seed user: %v", err)
	}
}

func TestSessionRepository_Create_And_GetByHash(t *testing.T) {
	database := testutil.NewTestDB(t)
	userRepo := db.NewUserRepository("sqlite", database)
	repo := db.NewSessionRepository("sqlite", database)
	seedUserForSessions(t, userRepo)

	now := time.Now()
	s := &db.Session{
		ID:               "session-1",
		UserID:           "user-1",
		DeviceName:       "Chrome on Linux",
		DeviceID:         "device-abc",
		IPAddress:        "192.168.1.100",
		RefreshTokenHash: "hash-abc-123",
		CreatedAt:        now,
		LastActiveAt:     now,
		ExpiresAt:        now.Add(720 * time.Hour),
	}

	if err := repo.Create(context.Background(), s); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := repo.GetByRefreshTokenHash(context.Background(), "hash-abc-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.ID != "session-1" {
		t.Errorf("expected ID 'session-1', got %q", got.ID)
	}
	if got.UserID != "user-1" {
		t.Errorf("expected user ID 'user-1', got %q", got.UserID)
	}
	if got.DeviceName != "Chrome on Linux" {
		t.Errorf("expected device name 'Chrome on Linux', got %q", got.DeviceName)
	}
}

func TestSessionRepository_GetByHash_NotFound(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewSessionRepository("sqlite", database)

	_, err := repo.GetByRefreshTokenHash(context.Background(), "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestSessionRepository_DeleteByID(t *testing.T) {
	database := testutil.NewTestDB(t)
	userRepo := db.NewUserRepository("sqlite", database)
	repo := db.NewSessionRepository("sqlite", database)
	seedUserForSessions(t, userRepo)

	now := time.Now()
	s := &db.Session{
		ID: "session-del", UserID: "user-1", DeviceName: "Test", DeviceID: "dev",
		RefreshTokenHash: "hash-del", CreatedAt: now, LastActiveAt: now,
		ExpiresAt: now.Add(time.Hour),
	}
	if err := repo.Create(context.Background(), s); err != nil {
		t.Fatal(err)
	}

	if err := repo.DeleteByID(context.Background(), "session-del"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := repo.GetByRefreshTokenHash(context.Background(), "hash-del")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Error("session should be deleted")
	}
}

func TestSessionRepository_DeleteByRefreshTokenHash(t *testing.T) {
	database := testutil.NewTestDB(t)
	userRepo := db.NewUserRepository("sqlite", database)
	repo := db.NewSessionRepository("sqlite", database)
	seedUserForSessions(t, userRepo)

	now := time.Now()
	s := &db.Session{
		ID: "session-hash-del", UserID: "user-1", DeviceName: "Test", DeviceID: "dev",
		RefreshTokenHash: "hash-to-delete", CreatedAt: now, LastActiveAt: now,
		ExpiresAt: now.Add(time.Hour),
	}
	if err := repo.Create(context.Background(), s); err != nil {
		t.Fatal(err)
	}

	if err := repo.DeleteByRefreshTokenHash(context.Background(), "hash-to-delete"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := repo.GetByRefreshTokenHash(context.Background(), "hash-to-delete")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Error("session should be deleted by hash")
	}
}

func TestSessionRepository_ListByUser(t *testing.T) {
	database := testutil.NewTestDB(t)
	userRepo := db.NewUserRepository("sqlite", database)
	repo := db.NewSessionRepository("sqlite", database)
	seedUserForSessions(t, userRepo)

	now := time.Now()
	for i, name := range []string{"Chrome", "Firefox", "Safari"} {
		s := &db.Session{
			ID: name, UserID: "user-1", DeviceName: name, DeviceID: name,
			RefreshTokenHash: "hash-" + name, CreatedAt: now, LastActiveAt: now.Add(time.Duration(i) * time.Minute),
			ExpiresAt: now.Add(time.Hour),
		}
		if err := repo.Create(context.Background(), s); err != nil {
			t.Fatal(err)
		}
	}

	sessions, err := repo.ListByUser(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 3 {
		t.Errorf("expected 3 sessions, got %d", len(sessions))
	}

	// Should be sorted by last_active_at DESC
	if sessions[0].DeviceName != "Safari" {
		t.Errorf("expected most recently active first, got %q", sessions[0].DeviceName)
	}
}

func TestSessionRepository_CountByUser(t *testing.T) {
	database := testutil.NewTestDB(t)
	userRepo := db.NewUserRepository("sqlite", database)
	repo := db.NewSessionRepository("sqlite", database)
	seedUserForSessions(t, userRepo)

	count, err := repo.CountByUser(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 sessions, got %d", count)
	}

	now := time.Now()
	s := &db.Session{
		ID: "s1", UserID: "user-1", DeviceName: "Test", DeviceID: "dev",
		RefreshTokenHash: "hash1", CreatedAt: now, LastActiveAt: now,
		ExpiresAt: now.Add(time.Hour),
	}
	if err := repo.Create(context.Background(), s); err != nil {
		t.Fatal(err)
	}

	count, err = repo.CountByUser(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 session, got %d", count)
	}
}
