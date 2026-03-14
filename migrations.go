package hubplay

import "embed"

//go:embed migrations/sqlite/*.sql
var SQLiteMigrations embed.FS

//go:embed all:web/dist
var WebAssets embed.FS
