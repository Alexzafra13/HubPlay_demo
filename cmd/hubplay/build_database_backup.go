package main

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"hubplay/internal/db"

	"github.com/pressly/goose/v3"
)

// preMigrateBackupsToKeep — cuántos snapshots pre-migración conservar.
const preMigrateBackupsToKeep = 3

// backupBeforeMigrate toma un snapshot SQLite ANTES de aplicar
// migraciones pendientes. Las migraciones del proyecto son up-only (sin
// rollback), así que un fallo a mitad de un upgrade puede dejar la DB
// medio-migrada; este snapshot da una vuelta atrás manual.
//
// Best-effort por diseño: si el backup falla (disco lleno, permisos), se
// AVISA y se continúa con la migración. Bloquear el arranque del server
// por no poder hacer backup sería peor UX que arriesgar una migración
// (que casi siempre es segura). SQLite-only: en Postgres el operador usa
// pg_dump / su herramienta de backups gestionada.
func backupBeforeMigrate(database *sql.DB, cfg databaseConfig, migrationsFS fs.FS, logger *slog.Logger) {
	if cfg.Driver != db.DriverSQLite {
		return
	}
	if err := goose.SetDialect("sqlite3"); err != nil {
		return
	}
	current, err := goose.GetDBVersion(database)
	if err != nil || current == 0 {
		// DB fresca (sin tabla de versión o versión 0): no hay datos del
		// usuario que proteger todavía.
		return
	}
	target := maxMigrationVersion(migrationsFS, "migrations/sqlite")
	if target <= current {
		return // al día — nada que migrar, nada que respaldar
	}

	backupDir := filepath.Join(filepath.Dir(cfg.Path), "backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		logger.Warn("pre-migrate backup: no se pudo crear el dir de backups; se continúa sin red de seguridad",
			"error", err, "dir", backupDir)
		return
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	dest := filepath.Join(backupDir, fmt.Sprintf("hubplay-pre-migrate-v%d-%s.db", current, stamp))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := db.NewMaintenance(cfg.Driver, database).VacuumInto(ctx, dest); err != nil {
		logger.Warn("pre-migrate backup falló; se continúa con la migración igualmente",
			"error", err, "dest", dest)
		return
	}
	logger.Info("pre-migrate backup creado",
		"from_version", current, "to_version", target, "dest", dest)
	prunePreMigrateBackups(backupDir, preMigrateBackupsToKeep)
}

// maxMigrationVersion devuelve el prefijo numérico más alto entre los
// ficheros .sql de `dir` dentro de `fsys` (p.ej. 057_audit_log.sql → 57).
func maxMigrationVersion(fsys fs.FS, dir string) int64 {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return 0
	}
	var maxV int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		i := strings.IndexFunc(name, func(r rune) bool { return r < '0' || r > '9' })
		if i <= 0 {
			continue
		}
		if v, err := strconv.ParseInt(name[:i], 10, 64); err == nil && v > maxV {
			maxV = v
		}
	}
	return maxV
}

// prunePreMigrateBackups conserva los `keep` snapshots pre-migración más
// recientes (el timestamp va en el nombre, así que orden lexicográfico =
// cronológico) y borra el resto.
func prunePreMigrateBackups(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var backups []string
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && strings.HasPrefix(n, "hubplay-pre-migrate-") && strings.HasSuffix(n, ".db") {
			backups = append(backups, n)
		}
	}
	if len(backups) <= keep {
		return
	}
	sort.Strings(backups)
	for _, name := range backups[:len(backups)-keep] {
		_ = os.Remove(filepath.Join(dir, name))
	}
}
