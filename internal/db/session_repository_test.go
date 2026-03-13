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
	userRepo := db.NewUserRepository(database)
	repo := db.NewSessionRepository(database)
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
	repo := db.NewSessionRepository(database)

	_, err := repo.GetByRefreshTokenHash(context.Background(), "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestSessionRepository_DeleteByID(t *testing.T) {
	database := testutil.NewTestDB(t)
	userRepo := db.NewUserRepository(database)
	repo := db.NewSessionRepository(database)
	seedUserForSessions(t, userRepo)

	now := time.Now()
	s := &db.Session{
		ID: "session-del", UserID: "user-1", DeviceName: "Test", DeviceID: "dev",
		RefreshTokenHash: "hash-del", CreatedAt: now, LastActiveAt: now,
		ExpiresAt: now.Add(time.Hour),
	}
	repo.Create(context.Background(), s)

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
	userRepo := db.NewUserRepository(database)
	repo := db.NewSessionRepository(database)
	seedUserForSessions(t, userRepo)

	now := time.Now()
	s := &db.Session{
		ID: "session-hash-del", UserID: "user-1", DeviceName: "Test", DeviceID: "dev",
		RefreshTokenHash: "hash-to-delete", CreatedAt: now, LastActiveAt: now,
		ExpiresAt: now.Add(time.Hour),
	}
	repo.Create(context.Background(), s)

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
	userRepo := db.NewUserRepository(database)
	repo := db.NewSessionRepository(database)
	seedUserForSessions(t, userRepo)

	now := time.Now()
	for i, name := range []string{"Chrome", "Firefox", "Safari"} {
		s := &db.Session{
			ID: name, UserID: "user-1", DeviceName: name, DeviceID: name,
			RefreshTokenHash: "hash-" + name, CreatedAt: now, LastActiveAt: now.Add(time.Duration(i) * time.Minute),
			ExpiresAt: now.Add(time.Hour),
		}
		repo.Create(context.Background(), s)
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
	userRepo := db.NewUserRepository(database)
	repo := db.NewSessionRepository(database)
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
	repo.Create(context.Background(), s)

	count, err = repo.CountByUser(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 session, got %d", count)
	}
}
