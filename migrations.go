package hubplay

import "embed"

//go:embed migrations/sqlite/*.sql
var SQLiteMigrations embed.FS

//go:embed migrations/postgres/*.sql
var PostgresMigrations embed.FS

//go:embed all:web/dist
var WebAssets embed.FS

// Migrations returns the migration filesystem matching `driver`. The
// binary embeds both trees so a single artefact can boot against
// either backend; the caller picks the right one based on
// `cfg.Database.Driver`.
func Migrations(driver string) embed.FS {
	if driver == "postgres" {
		return PostgresMigrations
	}
	return SQLiteMigrations
}
