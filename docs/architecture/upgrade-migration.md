# Upgrade & Migration Strategy

Cómo se actualizan las versiones de HubPlay, cómo se manejan las migraciones de base de datos, qué pasa con los breaking changes, y cómo hacer rollback si algo sale mal.

**Principio**: actualizar HubPlay debe ser tan fácil como `docker compose pull && docker compose up -d`. Sin pasos manuales, sin downtime perceptible.

---

## 1. Versioning — Semantic Versioning

```
v{MAJOR}.{MINOR}.{PATCH}

MAJOR  → Breaking changes (API, config format, DB incompatible)
MINOR  → New features, backwards-compatible
PATCH  → Bug fixes, security patches
```

### Versión en el binario

```go
// cmd/hubplay/main.go
var (
    version = "dev"      // Set by -ldflags at build time
    commit  = "none"
    date    = "unknown"
)

// $ hubplay --version
// hubplay 1.3.0 (commit abc1234, built 2026-03-13T10:00:00Z)
```

### Release cadence (estimado)

| Tipo | Frecuencia | Ejemplo |
|---|---|---|
| Patch | Según necesidad (security fixes) | v1.3.1 → v1.3.2 |
| Minor | Cada 4–8 semanas | v1.3.0 → v1.4.0 |
| Major | Raramente (breaking changes significativos) | v1.x → v2.0.0 |

---

## 2. Database Migrations

### Herramienta: goose

Migraciones como archivos SQL raw en `migrations/sqlite/` y `migrations/postgres/`.

```
migrations/sqlite/
├── 001_initial_schema.sql
├── 002_fts_search.sql
├── 003_iptv_channels.sql
├── 004_federation.sql
├── 005_plugins_webhooks.sql
├── 006_add_content_rating_index.sql     ← nueva migración
└── ...
```

### Formato de cada migración

```sql
-- +goose Up
CREATE INDEX idx_items_content_rating ON items(content_rating);

-- +goose Down
DROP INDEX IF EXISTS idx_items_content_rating;
```

### Ejecución automática

Las migraciones se ejecutan **automáticamente al arrancar** HubPlay, antes de iniciar el servidor HTTP:

```go
// main.go — Phase 2: Database
database, err := db.Open(cfg.Database, logger)
err = db.Migrate(database, cfg.Database.Driver)  // ← auto-migrate
```

```
Startup log:
  [INFO] Running database migrations...
  [INFO] Applied migration 006_add_content_rating_index.sql (12ms)
  [INFO] Database at version 6 (up to date)
```

### Reglas para escribir migraciones

| Regla | Por qué |
|---|---|
| Cada migración es idempotente | `CREATE TABLE IF NOT EXISTS`, `CREATE INDEX IF NOT EXISTS` |
| Siempre incluir `Down` | Para rollback (aunque no siempre es posible al 100%) |
| Nunca modificar una migración ya aplicada | Crear una nueva en su lugar |
| Mantener compatibilidad con la versión anterior | Para zero-downtime upgrades (ver sección 4) |
| Test de migración | `make test-migrations` ejecuta Up + Down + Up para verificar |
| Separar SQLite y PostgreSQL | Sintaxis diferente (FTS5 vs tsvector, AUTOINCREMENT vs SERIAL) |

### Migraciones destructivas (data loss)

Si una migración necesita eliminar o transformar datos:

1. **Fase 1** (versión N): Añadir nueva columna/tabla, copiar datos
2. **Fase 2** (versión N+1): Eliminar columna/tabla antigua

Esto permite rollback seguro entre versión N y N+1.

```sql
-- Migration 007 (v1.4.0): Add new_column, copy data
-- +goose Up
ALTER TABLE items ADD COLUMN sort_title_v2 TEXT;
UPDATE items SET sort_title_v2 = lower(replace(sort_title, 'The ', ''));

-- Migration 008 (v1.5.0): Drop old column
-- +goose Up
ALTER TABLE items DROP COLUMN sort_title;
ALTER TABLE items RENAME COLUMN sort_title_v2 TO sort_title;
```

---

## 3. Upgrade Process

### Docker (Recommended)

```bash
# 1. Pull new image
docker compose pull

# 2. Restart (auto-migrates DB)
docker compose up -d

# That's it. Migrations run automatically.
```

### Verificación post-upgrade

```bash
# Check version
docker compose exec hubplay hubplay --version

# Check health
curl -s http://localhost:8096/api/v1/health | jq .

# Check migration status
docker compose exec hubplay hubplay migrate status
```

### Native Binary

```bash
# 1. Backup (always before upgrade)
/usr/local/bin/hubplay-backup.sh

# 2. Stop
systemctl stop hubplay

# 3. Replace binary
curl -L https://github.com/hubplay/hubplay/releases/latest/download/hubplay-linux-amd64 \
  -o /usr/local/bin/hubplay
chmod +x /usr/local/bin/hubplay

# 4. Start (auto-migrates)
systemctl start hubplay

# 5. Verify
hubplay --version
journalctl -u hubplay --since "1 min ago"
```

---

## 4. Zero-Downtime Upgrades

Para upgrades menores que no rompen la API:

### Estrategia: migrate-first

```
1. Nueva versión arranca
2. Ejecuta migraciones (schema compatible con versión anterior)
3. Inicia servidor HTTP
4. Vieja versión se detiene
```

Esto funciona porque:
- Las migraciones son **aditivas** (añadir columna, crear tabla, crear index)
- Nunca eliminamos columnas en la misma versión que las deprecamos
- El código nuevo maneja tanto el schema viejo como el nuevo

### Docker rolling update

```yaml
# docker-compose.yml
services:
  hubplay:
    deploy:
      update_config:
        order: start-first     # Start new container before stopping old
        failure_action: rollback
    healthcheck:
      test: ["CMD", "hubplay", "health"]
      interval: 10s
      start_period: 30s
```

---

## 5. Rollback

### Cuándo hacer rollback

- La nueva versión no arranca (crash loop)
- Migración falla (raro, pero posible)
- Bug crítico descubierto post-upgrade
- Performance degradation

### Rollback — Docker

```bash
# 1. Rollback DB migration (if needed)
docker compose exec hubplay hubplay migrate down --steps 1

# 2. Use previous image version
# In docker-compose.yml, change:
#   image: hubplay/hubplay:latest
# To:
#   image: hubplay/hubplay:1.3.0

# 3. Restart
docker compose up -d
```

### Rollback — Native binary

```bash
# 1. Stop
systemctl stop hubplay

# 2. Rollback migration
hubplay migrate down --steps 1 --config /etc/hubplay/hubplay.yaml

# 3. Restore previous binary
cp /usr/local/bin/hubplay.bak /usr/local/bin/hubplay

# 4. Start
systemctl start hubplay
```

### Rollback — Database restore (nuclear option)

Si el rollback de migración no funciona, restaurar desde backup:

```bash
# 1. Stop
systemctl stop hubplay

# 2. Restore DB from backup
cp /backups/hubplay/hubplay_2026-03-12.db /config/hubplay.db

# 3. Restore previous binary
cp /usr/local/bin/hubplay.bak /usr/local/bin/hubplay

# 4. Start
systemctl start hubplay
```

---

## 6. Breaking Changes Policy

### What counts as a breaking change

| Category | Example | Requires MAJOR bump |
|---|---|---|
| API response shape change | Removing a field from JSON response | Yes |
| API endpoint removal | Removing `GET /api/v1/items/{id}/similar` | Yes |
| Config key rename | `transcoding.preset` → `transcoding.default_preset` | Yes |
| DB migration with data loss | Dropping a table with user data | Yes |
| Minimum system requirement change | Requiring FFmpeg 6.0+ (was 5.0+) | Yes |

### What is NOT a breaking change

| Category | Example |
|---|---|
| Adding a new API field | New `hdr_type` field in item response |
| Adding a new endpoint | New `GET /api/v1/epg/search` |
| Adding a new config key | New `iptv.health_check_interval` with default |
| Adding a new DB column (nullable) | `ALTER TABLE items ADD COLUMN tagline TEXT` |
| Performance improvement | Faster scans, smaller memory usage |

### Deprecation process

1. **Version N**: Mark feature as deprecated in docs + API response headers (`Deprecation: true`)
2. **Version N+1 or N+2**: Remove feature (MAJOR version bump if removing API)
3. **Changelog**: Always document breaking changes prominently

---

## 7. SQLite → PostgreSQL Migration

Para usuarios que empezaron con SQLite y quieren migrar a PostgreSQL:

```bash
# 1. Export from SQLite
hubplay db export --format sql --output hubplay_export.sql

# 2. Update config
# database:
#   driver: "postgres"
#   dsn: "host=localhost dbname=hubplay user=hubplay password=secret sslmode=disable"

# 3. Start HubPlay (creates PostgreSQL schema via migrations)
hubplay --config hubplay.yaml

# 4. Import data
hubplay db import --input hubplay_export.sql

# 5. Verify
hubplay db status
```

El comando `db export` genera SQL estándar compatible con ambos drivers. El `db import` adapta tipos automáticamente (ej: `DATETIME` SQLite → `TIMESTAMP` PostgreSQL).

---

## 8. Version Compatibility Matrix

| HubPlay Version | Min Go | Min Node | Min FFmpeg | SQLite Schema | PostgreSQL Schema |
|---|---|---|---|---|---|
| v1.0.x | 1.22 | 20 | 5.0 | v1–v5 | v1–v5 |
| v1.1.x | 1.22 | 20 | 5.0 | v1–v6 | v1–v6 |
| v1.2.x | 1.22 | 20 | 5.0 | v1–v7 | v1–v7 |
| v2.0.x | 1.23+ | 22 | 6.0 | v1–v10 (breaking) | v1–v10 (breaking) |

---

## 9. Changelog & Release Notes

Cada release incluye:

```markdown
## v1.4.0 (2026-XX-XX)

### New Features
- Live TV: EPG search (#123)
- Federation: download content from peers (#145)

### Improvements
- Scanner 40% faster with parallel FFprobe (#150)
- Reduced memory usage for large libraries (#152)

### Bug Fixes
- Fix subtitle burn-in for PGS tracks (#148)
- Fix WebSocket reconnection on mobile (#149)

### Breaking Changes
- None

### Migration Notes
- Database migration 008 adds EPG search index (automatic, ~2s)
- New config option: `iptv.health_check_interval` (default: 30m)
```

### Dónde se publica

| Canal | Contenido |
|---|---|
| GitHub Releases | Changelog completo + binarios + checksums |
| Docker Hub | Tag `latest` + tag versionado (`1.4.0`) |
| `/api/v1/system/info` | Versión actual del servidor |
| `/api/v1/system/updates` | Check si hay nueva versión disponible (opt-in, no phone-home por defecto) |

---

## 10. CLI Commands for Migration Management

```bash
# Check current migration status
hubplay migrate status
# Output:
#   Applied: 001_initial_schema.sql
#   Applied: 002_fts_search.sql
#   ...
#   Pending: 008_epg_search_index.sql

# Apply all pending migrations
hubplay migrate up

# Apply N migrations
hubplay migrate up --steps 2

# Rollback last migration
hubplay migrate down --steps 1

# Rollback to specific version
hubplay migrate down --to 5

# Create new migration file
hubplay migrate create add_tagline_column
# Creates: migrations/sqlite/009_add_tagline_column.sql
#          migrations/postgres/009_add_tagline_column.sql

# Export database (for SQLite → PostgreSQL migration)
hubplay db export --format sql --output backup.sql

# Import database
hubplay db import --input backup.sql

# Database status
hubplay db status
# Output:
#   Driver: sqlite
#   Path: /config/hubplay.db
#   Size: 45.2 MB
#   Schema version: 7
#   Tables: 27
#   Items: 12,345
#   Users: 3
```
