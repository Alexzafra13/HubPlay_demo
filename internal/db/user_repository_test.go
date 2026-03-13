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

func createTestUser(t *testing.T, repo *db.UserRepository, username string) *db.User {
	t.Helper()
	u := &db.User{
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
	repo := db.NewUserRepository(database)

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
	repo := db.NewUserRepository(database)

	_, err := repo.GetByID(context.Background(), "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUserRepository_GetByUsername(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(database)

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
	repo := db.NewUserRepository(database)

	_, err := repo.GetByUsername(context.Background(), "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUserRepository_List(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(database)

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
	repo := db.NewUserRepository(database)

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
	repo := db.NewUserRepository(database)

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
	repo := db.NewUserRepository(database)

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
	repo := db.NewUserRepository(database)

	err := repo.Delete(context.Background(), "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUserRepository_Count(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(database)

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

func TestUserRepository_UpdateLastLogin(t *testing.T) {
	database := testutil.NewTestDB(t)
	repo := db.NewUserRepository(database)

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
