package main

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestMaxMigrationVersion(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/sqlite/001_initial.sql":     {Data: []byte("x")},
		"migrations/sqlite/002_fts.sql":         {Data: []byte("x")},
		"migrations/sqlite/057_audit_log.sql":   {Data: []byte("x")},
		"migrations/sqlite/README.md":           {Data: []byte("x")}, // sin prefijo → ignorado
		"migrations/sqlite/notanumber_foo.sql":  {Data: []byte("x")}, // ignorado
		"migrations/postgres/099_pg_only.sql":   {Data: []byte("x")}, // otro dir → ignorado
	}
	if got := maxMigrationVersion(fsys, "migrations/sqlite"); got != 57 {
		t.Errorf("maxMigrationVersion = %d, want 57", got)
	}
	if got := maxMigrationVersion(fsys, "migrations/nope"); got != 0 {
		t.Errorf("dir inexistente debería dar 0, got %d", got)
	}
}

func TestPrunePreMigrateBackups(t *testing.T) {
	dir := t.TempDir()
	// Nombres con timestamp creciente → orden lexicográfico = cronológico.
	names := []string{
		"hubplay-pre-migrate-v50-20260101-000000.db",
		"hubplay-pre-migrate-v51-20260102-000000.db",
		"hubplay-pre-migrate-v52-20260103-000000.db",
		"hubplay-pre-migrate-v53-20260104-000000.db",
		"hubplay-pre-migrate-v54-20260105-000000.db",
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Un fichero no-backup que NUNCA debe tocarse.
	other := filepath.Join(dir, "hubplay.db")
	if err := os.WriteFile(other, []byte("live"), 0o600); err != nil {
		t.Fatal(err)
	}

	prunePreMigrateBackups(dir, 3)

	remaining, _ := os.ReadDir(dir)
	got := map[string]bool{}
	for _, e := range remaining {
		got[e.Name()] = true
	}
	// Conserva los 3 más recientes (v52, v53, v54) + el live.
	for _, keep := range []string{
		"hubplay-pre-migrate-v52-20260103-000000.db",
		"hubplay-pre-migrate-v53-20260104-000000.db",
		"hubplay-pre-migrate-v54-20260105-000000.db",
		"hubplay.db",
	} {
		if !got[keep] {
			t.Errorf("debería conservar %q", keep)
		}
	}
	// Borra los 2 más viejos.
	for _, gone := range []string{
		"hubplay-pre-migrate-v50-20260101-000000.db",
		"hubplay-pre-migrate-v51-20260102-000000.db",
	} {
		if got[gone] {
			t.Errorf("debería haber borrado %q", gone)
		}
	}
}
