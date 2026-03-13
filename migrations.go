package hubplay

import "embed"

//go:embed migrations/sqlite/*.sql
var SQLiteMigrations embed.FS
