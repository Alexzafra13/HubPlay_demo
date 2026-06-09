// Command hpseed populates a HubPlay SQLite database with synthetic
// catalogue data (movies + IPTV channels) and an admin account, so the
// query / load hot paths can be measured at realistic scale without a
// real media library on disk.
//
// It reuses the production repositories (no schema duplication), so the
// rows it writes are identical to what a real scan would produce.
//
//	go run ./cmd/hpseed -db ./data/hubplay.db -items 5000 -channels 5000
//
// For measuring the FULL app (transcoding, scanning real files, hwaccel,
// frontend) point HubPlay at a real media folder instead — see
// docs/perf-measurement.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	hubplay "hubplay"
	authmodel "hubplay/internal/auth/model"
	"hubplay/internal/db"
	iptvmodel "hubplay/internal/iptv/model"
	librarymodel "hubplay/internal/library/model"
)

func main() {
	dbPath := flag.String("db", "./hubplay.db", "path to the SQLite database file")
	nItems := flag.Int("items", 5000, "number of movie items to insert")
	nChannels := flag.Int("channels", 5000, "number of IPTV channels to insert")
	adminUser := flag.String("admin-user", "admin", "admin username to create")
	adminPass := flag.String("admin-pass", "hubplay123", "admin password")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ctx := context.Background()

	database, err := db.Open(db.DriverSQLite, *dbPath, logger)
	if err != nil {
		fatal("open db", err)
	}
	defer database.Close() //nolint:errcheck
	if err := db.Migrate(db.DriverSQLite, database, hubplay.Migrations(db.DriverSQLite), logger); err != nil {
		fatal("migrate", err)
	}

	users := db.NewUserRepository(db.DriverSQLite, database)
	libs := db.NewLibraryRepository(db.DriverSQLite, database)
	items := db.NewItemRepository(db.DriverSQLite, database)
	channels := db.NewChannelRepository(db.DriverSQLite, database)
	now := time.Now()

	// Admin (owner ⇒ every permission) so the load generator can hit
	// the whole authenticated surface.
	hash, err := bcrypt.GenerateFromPassword([]byte(*adminPass), 12)
	if err != nil {
		fatal("hash password", err)
	}
	admin := &authmodel.User{
		ID: uuid.NewString(), Username: *adminUser, DisplayName: "Seed Admin",
		PasswordHash: string(hash), Role: "admin", IsActive: true, IsOwner: true,
		CreatedAt: now,
	}
	if err := users.Create(ctx, admin); err != nil {
		logger.Warn("admin create failed (may already exist)", "error", err)
	} else {
		fmt.Printf("admin: %s / %s\n", *adminUser, *adminPass)
	}

	// Movies library + N items (each with a video + audio stream, via the
	// same transactional IngestItem the scanner uses).
	movieLib := &librarymodel.Library{
		ID: uuid.NewString(), Name: "Seed Movies", ContentType: "movies",
		ScanMode: "manual", ScanInterval: "6h", CreatedAt: now, UpdatedAt: now,
		Paths: []string{"/seed/movies"},
	}
	if err := libs.Create(ctx, movieLib); err != nil {
		fatal("create movie library", err)
	}
	for i := 0; i < *nItems; i++ {
		id := uuid.NewString()
		title := fmt.Sprintf("Seed Movie %05d", i)
		it := &librarymodel.Item{
			ID: id, LibraryID: movieLib.ID, Type: "movie",
			Title: title, SortTitle: title, Year: 1980 + i%45,
			Path:      fmt.Sprintf("/seed/movies/movie_%05d.mkv", i),
			Container: "matroska,webm", DurationTicks: 72_000_000_000,
			AddedAt: now, UpdatedAt: now, IsAvailable: true,
		}
		streams := []*librarymodel.MediaStream{
			{ItemID: id, StreamIndex: 0, StreamType: "video", Codec: "h264", Width: 1920, Height: 1080},
			{ItemID: id, StreamIndex: 1, StreamType: "audio", Codec: "aac", Channels: 6, Language: "eng"},
		}
		if err := items.IngestItem(ctx, it, streams, nil); err != nil {
			fatal("ingest item", err)
		}
		if i > 0 && i%1000 == 0 {
			fmt.Printf("  %d items...\n", i)
		}
	}
	fmt.Printf("inserted %d movie items\n", *nItems)

	// LiveTV library + M channels.
	tvLib := &librarymodel.Library{
		ID: uuid.NewString(), Name: "Seed LiveTV", ContentType: "livetv",
		ScanMode: "manual", ScanInterval: "6h", CreatedAt: now, UpdatedAt: now,
	}
	if err := libs.Create(ctx, tvLib); err != nil {
		fatal("create livetv library", err)
	}
	for i := 0; i < *nChannels; i++ {
		ch := &iptvmodel.Channel{
			ID: uuid.NewString(), LibraryID: tvLib.ID,
			Name: fmt.Sprintf("Seed Channel %05d", i), Number: i + 1,
			GroupName: fmt.Sprintf("Group %02d", i%50),
			StreamURL: fmt.Sprintf("http://example.invalid/stream/%05d.ts", i),
			IsActive:  true, AddedAt: now,
		}
		if err := channels.Create(ctx, ch); err != nil {
			fatal("create channel", err)
		}
		if i > 0 && i%1000 == 0 {
			fmt.Printf("  %d channels...\n", i)
		}
	}
	fmt.Printf("inserted %d channels\n", *nChannels)
	fmt.Println("seed complete:", *dbPath)
}

func fatal(msg string, err error) {
	fmt.Fprintf(os.Stderr, "hpseed: %s: %v\n", msg, err)
	os.Exit(1)
}
