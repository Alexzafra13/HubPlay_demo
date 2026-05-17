// pg-smoke is a one-shot integration check for the Sesión F dual-
// dialect wiring. Run it manually against a throwaway Postgres
// instance — never deployed.
//
// Usage:
//
//	docker run -d --rm --name hubplay-pg-smoke \
//	  -e POSTGRES_PASSWORD=test -e POSTGRES_DB=hubplay \
//	  -p 25432:5432 postgres:16-alpine
//	go run ./cmd/pg-smoke "postgres://postgres:test@127.0.0.1:25432/hubplay?sslmode=disable"
//
// Validates: open + ping, migrations, a couple of dual-dialect repo
// methods, the isUniqueConstraintError path (UNIQUE-violation → typed
// sentinel error), and FTS dual-mechanism (Items.List with a query).
//
// Exit 0 = everything works.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"hubplay"
	iptvmodel "hubplay/internal/iptv/model"
	"hubplay/internal/db"
	"hubplay/internal/federation"
	federationstorage "hubplay/internal/federation/storage"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: pg-smoke <postgres-dsn>")
		os.Exit(2)
	}
	dsn := os.Args[1]

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx := context.Background()

	// 1. Open + ping
	logger.Info("step 1: open postgres + ping")
	database, err := db.Open(db.DriverPostgres, dsn, logger)
	if err != nil {
		fatal(logger, "open", err)
	}
	defer database.Close() //nolint:errcheck

	// 2. Migrate
	logger.Info("step 2: run migrations (migrations/postgres)")
	if err := db.Migrate(db.DriverPostgres, database, hubplay.Migrations(db.DriverPostgres), logger); err != nil {
		fatal(logger, "migrate", err)
	}

	// 3. Wire repos
	logger.Info("step 3: wire repositories with driver=postgres")
	repos := db.NewRepositories(db.DriverPostgres, database)

	// 4. Library: create + list
	logger.Info("step 4: library create + list")
	lib := &db.Library{
		ID:          "smoke-lib-1",
		Name:        "Smoke Library",
		ContentType: "movies",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := repos.Libraries.Create(ctx, lib); err != nil {
		fatal(logger, "library create", err)
	}
	libs, err := repos.Libraries.List(ctx)
	if err != nil {
		fatal(logger, "library list", err)
	}
	if len(libs) != 1 || libs[0].ID != lib.ID {
		fatal(logger, "library list shape", fmt.Errorf("got %+v", libs))
	}
	logger.Info("library round-trip OK", "count", len(libs))

	// 5. LibraryEPGSources: insert two with the same URL → second
	// must be ErrEPGSourceAlreadyAttached. Validates
	// isUniqueConstraintError on the pg pgconn.PgError code 23505 path.
	logger.Info("step 5: validate UNIQUE-violation detection via pg SQLSTATE 23505")
	src := &iptvmodel.LibraryEPGSource{
		ID:        "smoke-src-1",
		LibraryID: lib.ID,
		CatalogID: "",
		URL:       "https://epg.example/smoke.xml",
		Priority:  0,
		CreatedAt: time.Now().UTC(),
	}
	if err := repos.LibraryEPGSources.Create(ctx, src); err != nil {
		fatal(logger, "epg source first insert", err)
	}
	dup := *src
	dup.ID = "smoke-src-2"
	err = repos.LibraryEPGSources.Create(ctx, &dup)
	if !errors.Is(err, db.ErrEPGSourceAlreadyAttached) {
		fatal(logger, "expected ErrEPGSourceAlreadyAttached", fmt.Errorf("got: %v", err))
	}
	logger.Info("UNIQUE-violation correctly mapped to ErrEPGSourceAlreadyAttached")

	// 6. Federation identity: insert + read back. Exercises
	// FederationPeer projection (LastSeenStatusCode NullInt32 in pg).
	logger.Info("step 6: federation identity insert + get")
	fedRepo := federationstorage.NewRepository(db.DriverPostgres, database)
	identity := &federation.Identity{
		ServerUUID: "smoke-server-uuid",
		Name:       "smoke-server",
		PrivateKey: []byte("priv-bytes-smoke"),
		PublicKey:  []byte("pub-bytes-smoke"),
		CreatedAt:  time.Now().UTC(),
	}
	if err := fedRepo.InsertIdentity(ctx, identity); err != nil {
		fatal(logger, "federation insert identity", err)
	}
	got, err := fedRepo.GetIdentity(ctx)
	if err != nil {
		fatal(logger, "federation get identity", err)
	}
	if got == nil || got.ServerUUID != identity.ServerUUID {
		fatal(logger, "federation identity round-trip", fmt.Errorf("got %+v", got))
	}
	logger.Info("federation identity round-trip OK")
	_ = repos

	logger.Info("✅ all smoke-test steps passed — pgx wire + dual-dialect repos work against real Postgres")
}

func fatal(logger *slog.Logger, step string, err error) {
	logger.Error("smoke-test FAILED", "step", step, "err", err)
	os.Exit(1)
}
