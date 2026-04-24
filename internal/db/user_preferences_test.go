package db_test

import (
	"context"
	"testing"
	"time"

	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

func setupPreferencesTest(t *testing.T) (*db.Repositories, string) {
	t.Helper()
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(database)
	ctx := context.Background()

	user := &db.User{
		ID:           "user-prefs",
		Username:     "prefs",
		PasswordHash: "x",
		DisplayName:  "Prefs",
		Role:         "user",
		IsActive:     true,
		CreatedAt:    time.Now(),
	}
	if err := repos.Users.Create(ctx, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return repos, user.ID
}

func TestUserPreferences_SetIsUpsert(t *testing.T) {
	repos, uid := setupPreferencesTest(t)
	ctx := context.Background()

	first, err := repos.UserPreferences.Set(ctx, uid, "livetv.hero_mode", `"favorites"`)
	if err != nil {
		t.Fatalf("first set: %v", err)
	}
	if first.Value != `"favorites"` {
		t.Errorf("value = %q, want %q", first.Value, `"favorites"`)
	}

	second, err := repos.UserPreferences.Set(ctx, uid, "livetv.hero_mode", `"live-now"`)
	if err != nil {
		t.Fatalf("second set: %v", err)
	}
	if second.Value != `"live-now"` {
		t.Errorf("value after upsert = %q, want %q", second.Value, `"live-now"`)
	}

	all, err := repos.UserPreferences.ListByUser(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 preference row, got %d (upsert should not duplicate)", len(all))
	}
}

func TestUserPreferences_ListByUserIsolatesUsers(t *testing.T) {
	repos, uid := setupPreferencesTest(t)
	ctx := context.Background()

	other := &db.User{
		ID:           "user-other",
		Username:     "other",
		PasswordHash: "x",
		DisplayName:  "Other",
		Role:         "user",
		IsActive:     true,
		CreatedAt:    time.Now(),
	}
	if err := repos.Users.Create(ctx, other); err != nil {
		t.Fatal(err)
	}

	_, _ = repos.UserPreferences.Set(ctx, uid, "theme", `"dark"`)
	_, _ = repos.UserPreferences.Set(ctx, other.ID, "theme", `"light"`)

	mine, _ := repos.UserPreferences.ListByUser(ctx, uid)
	if len(mine) != 1 || mine[0].Value != `"dark"` {
		t.Errorf("user A got %v; expected single dark", mine)
	}
	hers, _ := repos.UserPreferences.ListByUser(ctx, other.ID)
	if len(hers) != 1 || hers[0].Value != `"light"` {
		t.Errorf("user B got %v; expected single light", hers)
	}
}

func TestUserPreferences_DeleteIsIdempotent(t *testing.T) {
	repos, uid := setupPreferencesTest(t)
	ctx := context.Background()

	if err := repos.UserPreferences.Delete(ctx, uid, "never.set"); err != nil {
		t.Fatalf("delete missing: %v", err)
	}

	_, _ = repos.UserPreferences.Set(ctx, uid, "k", "v")
	if err := repos.UserPreferences.Delete(ctx, uid, "k"); err != nil {
		t.Fatal(err)
	}
	rows, _ := repos.UserPreferences.ListByUser(ctx, uid)
	if len(rows) != 0 {
		t.Errorf("row still present after delete: %v", rows)
	}
}
