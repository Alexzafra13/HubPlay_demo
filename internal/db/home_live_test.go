package db_test

import (
	"context"
	"testing"
	"time"

	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/db"
	iptvmodel "hubplay/internal/iptv/model"
	librarymodel "hubplay/internal/library/model"
	"hubplay/internal/testutil"
)

// Regresión: una guía con dos programas solapados "en emisión" para el
// mismo canal duplicaba el canal en el rail "En directo ahora". La app
// de TV usa el id de canal como key del LazyRow y Compose aborta el
// proceso ante una key repetida.
func TestHomeRepository_LiveNow_OneRowPerChannelWithOverlappingEPG(t *testing.T) {
	database := testutil.NewTestDB(t)
	repos := db.NewRepositories(testutil.Driver(), database)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := repos.Libraries.Create(ctx, &librarymodel.Library{
		ID: "lib-tv", Name: "Live TV", ContentType: "livetv",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := repos.Users.Create(ctx, &authmodel.User{
		ID: "u-1", Username: "u1", DisplayName: "U1",
		PasswordHash: "x", Role: "admin", IsActive: true, CreatedAt: now,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repos.Libraries.GrantAccess(ctx, "u-1", "lib-tv"); err != nil {
		t.Fatalf("grant access: %v", err)
	}
	chRepo := db.NewChannelRepository(testutil.Driver(), database)
	for _, ch := range []struct{ id, name string }{{"ch-a", "Canal A"}, {"ch-b", "Canal B"}} {
		if err := chRepo.Create(ctx, &iptvmodel.Channel{
			ID: ch.id, LibraryID: "lib-tv", Name: ch.name, Number: 1,
			StreamURL: "http://stream.example.com/" + ch.id, TvgID: ch.id,
			IsActive: true, AddedAt: now,
		}); err != nil {
			t.Fatalf("create channel %s: %v", ch.id, err)
		}
	}

	insert := db.RewritePlaceholders(testutil.Driver(),
		`INSERT INTO epg_programs (id, channel_id, title, start_time, end_time) VALUES (?, ?, ?, ?, ?)`)
	programs := []struct {
		id, ch, title string
		start, end    time.Time
	}{
		// ch-a: dos programas solapados, ambos "en emisión" ahora.
		{"p-old", "ch-a", "Empezó antes", now.Add(-90 * time.Minute), now.Add(30 * time.Minute)},
		{"p-new", "ch-a", "Empezó después", now.Add(-10 * time.Minute), now.Add(50 * time.Minute)},
		// ch-b: sin programa actual (solo uno ya terminado).
		{"p-done", "ch-b", "Terminado", now.Add(-3 * time.Hour), now.Add(-2 * time.Hour)},
	}
	for _, p := range programs {
		if _, err := database.ExecContext(ctx, insert, p.id, p.ch, p.title, p.start, p.end); err != nil {
			t.Fatalf("seed program %s: %v", p.id, err)
		}
	}

	home := db.NewHomeRepository(testutil.Driver(), database)
	rows, err := home.LiveNow(ctx, "u-1", 10)
	if err != nil {
		t.Fatalf("LiveNow: %v", err)
	}

	seen := map[string]int{}
	for _, r := range rows {
		seen[r.ChannelID]++
	}
	if seen["ch-a"] != 1 || seen["ch-b"] != 1 || len(rows) != 2 {
		t.Fatalf("expected each channel exactly once, got %v (%d rows)", seen, len(rows))
	}
	for _, r := range rows {
		switch r.ChannelID {
		case "ch-a":
			if r.ProgramTitle != "Empezó después" {
				t.Errorf("ch-a program = %q, want the later-starting one", r.ProgramTitle)
			}
		case "ch-b":
			if r.ProgramTitle != "" || r.ProgramStart != nil {
				t.Errorf("ch-b should have no current program, got %q", r.ProgramTitle)
			}
		}
	}
	// El canal con programa va antes que el que no lo tiene.
	if rows[0].ChannelID != "ch-a" {
		t.Errorf("first row = %s, want ch-a (has_now DESC)", rows[0].ChannelID)
	}
}
