# sqlc Integration — Design Document

## Overview

sqlc genera código Go type-safe a partir de queries SQL. Escribes SQL, sqlc genera structs y funciones Go. Sin reflexión, sin ORM, rendimiento near-raw `database/sql`.

---

## 1. Workflow

```
1. Escribir query SQL en queries/*.sql
2. Ejecutar `sqlc generate`
3. sqlc genera Go code en internal/db/sqlc/
4. Services usan el código generado via interfaces
```

```
migrations/sqlite/001_initial.sql     ← Schema (goose)
internal/db/queries/items.sql         ← Queries SQL con anotaciones sqlc
internal/db/sqlc/                     ← Código generado (NO editar)
    ├── models.go                     ← Structs para cada tabla
    ├── items.sql.go                  ← Funciones para queries de items
    ├── users.sql.go                  ← Funciones para queries de users
    ├── querier.go                    ← Interface con todos los métodos
    └── db.go                         ← Constructor
internal/db/repos.go                  ← Wrappers que adaptan sqlc → domain interfaces
```

---

## 2. sqlc Configuration

```yaml
# sqlc.yaml (raíz del proyecto)
version: "2"
sql:
  - engine: "sqlite"
    queries: "internal/db/queries/"
    schema: "migrations/sqlite/"
    gen:
      go:
        package: "sqlc"
        out: "internal/db/sqlc"
        emit_interface: true          # Genera interface Querier
        emit_json_tags: true
        emit_empty_slices: true       # []T en vez de nil para slices vacías
        overrides:
          - column: "*.id"
            go_type: "string"         # UUIDs como string (TEXT en SQLite)
          - column: "*.created_at"
            go_type: "time.Time"
          - column: "*.updated_at"
            go_type: "time.Time"
```

---

## 3. Queries SQL con Anotaciones

### Items

```sql
-- internal/db/queries/items.sql

-- name: GetItem :one
SELECT * FROM items WHERE id = ? LIMIT 1;

-- name: GetItemsByLibrary :many
SELECT * FROM items
WHERE library_id = ?
ORDER BY
    CASE WHEN @sort_by = 'title' THEN sort_title END ASC,
    CASE WHEN @sort_by = 'year' THEN year END DESC,
    CASE WHEN @sort_by = 'added' THEN added_at END DESC
LIMIT ? OFFSET ?;

-- name: CountItemsByLibrary :one
SELECT COUNT(*) FROM items WHERE library_id = ?;

-- name: GetItemByPath :one
SELECT * FROM items WHERE path = ? LIMIT 1;

-- name: GetItemsByParent :many
SELECT * FROM items WHERE parent_id = ? ORDER BY season_number, episode_number;

-- name: CreateItem :exec
INSERT INTO items (
    id, library_id, parent_id, type, title, sort_title,
    original_title, year, path, size, duration_ticks,
    container, fingerprint, season_number, episode_number,
    community_rating, content_rating, premiere_date, added_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateItem :exec
UPDATE items SET
    title = ?, sort_title = ?, original_title = ?, year = ?,
    path = ?, size = ?, duration_ticks = ?, container = ?,
    fingerprint = ?, community_rating = ?, content_rating = ?,
    premiere_date = ?, updated_at = ?, is_available = ?
WHERE id = ?;

-- name: DeleteItem :exec
DELETE FROM items WHERE id = ?;

-- name: MarkUnavailable :execrows
UPDATE items SET is_available = 0
WHERE library_id = ? AND path NOT IN (/*SLICE:active_paths*/?)
AND is_available = 1;

-- name: SearchItems :many
SELECT items.* FROM items
JOIN items_fts ON items.rowid = items_fts.rowid
WHERE items_fts MATCH ?
ORDER BY rank
LIMIT ? OFFSET ?;

-- name: CountSearchItems :one
SELECT COUNT(*) FROM items
JOIN items_fts ON items.rowid = items_fts.rowid
WHERE items_fts MATCH ?;
```

### Users

```sql
-- internal/db/queries/users.sql

-- name: GetUserByID :one
SELECT * FROM users WHERE id = ?;

-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = ?;

-- name: ListUsers :many
SELECT * FROM users ORDER BY username LIMIT ? OFFSET ?;

-- name: CreateUser :exec
INSERT INTO users (id, username, display_name, password_hash, role, created_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: UpdateUser :exec
UPDATE users SET display_name = ?, role = ?, is_active = ?, updated_at = ?
WHERE id = ?;

-- name: UpdateLastLogin :exec
UPDATE users SET last_login_at = ? WHERE id = ?;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = ?;
```

### Watch Progress

```sql
-- internal/db/queries/progress.sql

-- name: UpsertProgress :exec
INSERT INTO user_data (user_id, item_id, position_ticks, last_played_at, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(user_id, item_id) DO UPDATE SET
    position_ticks = excluded.position_ticks,
    last_played_at = excluded.last_played_at,
    updated_at = excluded.updated_at;

-- name: GetProgress :one
SELECT * FROM user_data WHERE user_id = ? AND item_id = ?;

-- name: GetContinueWatching :many
SELECT ud.*, i.title, i.type, i.year, i.duration_ticks
FROM user_data ud
JOIN items i ON i.id = ud.item_id
WHERE ud.user_id = ?
    AND ud.completed = 0
    AND ud.position_ticks > 0
    AND CAST(ud.position_ticks AS REAL) / NULLIF(i.duration_ticks, 0) BETWEEN 0.05 AND 0.90
ORDER BY ud.last_played_at DESC
LIMIT ?;

-- name: MarkCompleted :exec
UPDATE user_data SET completed = 1, play_count = play_count + 1, updated_at = ?
WHERE user_id = ? AND item_id = ?;
```

---

## 4. Código Generado (ejemplo)

sqlc genera esto automáticamente (NO editar):

```go
// internal/db/sqlc/models.go (generado)
type Item struct {
    ID              string
    LibraryID       string
    ParentID        sql.NullString
    Type            string
    Title           string
    SortTitle       string
    OriginalTitle   sql.NullString
    Year            sql.NullInt64
    Path            sql.NullString
    Size            int64
    DurationTicks   int64
    Container       sql.NullString
    Fingerprint     sql.NullString
    SeasonNumber    sql.NullInt64
    EpisodeNumber   sql.NullInt64
    CommunityRating sql.NullFloat64
    ContentRating   sql.NullString
    PremiereDate    sql.NullTime
    AddedAt         time.Time
    UpdatedAt       time.Time
    IsAvailable     bool
}

// internal/db/sqlc/items.sql.go (generado)
func (q *Queries) GetItem(ctx context.Context, id string) (Item, error) { ... }
func (q *Queries) GetItemsByLibrary(ctx context.Context, arg GetItemsByLibraryParams) ([]Item, error) { ... }
func (q *Queries) CreateItem(ctx context.Context, arg CreateItemParams) error { ... }
```

---

## 5. Repository Wrappers (Domain Adapters)

Los services NO usan el código sqlc directamente. Un wrapper adapta tipos sqlc → tipos de dominio:

```go
// internal/db/item_repository.go
type ItemRepository struct {
    q *sqlc.Queries
}

func NewItemRepository(db *sql.DB) *ItemRepository {
    return &ItemRepository{q: sqlc.New(db)}
}

func (r *ItemRepository) GetByID(ctx context.Context, id uuid.UUID) (*media.Item, error) {
    row, err := r.q.GetItem(ctx, id.String())
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return nil, fmt.Errorf("item %s: %w", id, domain.ErrNotFound)
        }
        return nil, fmt.Errorf("get item %s: %w", id, err)
    }
    return mapSqlcItemToDomain(row), nil
}

func (r *ItemRepository) GetByLibrary(ctx context.Context, libID uuid.UUID, opts ListOptions) ([]media.Item, int, error) {
    items, err := r.q.GetItemsByLibrary(ctx, sqlc.GetItemsByLibraryParams{
        LibraryID: libID.String(),
        SortBy:    opts.SortBy,
        Limit:     int64(opts.Limit),
        Offset:    int64(opts.Offset),
    })
    if err != nil {
        return nil, 0, fmt.Errorf("list items: %w", err)
    }

    total, err := r.q.CountItemsByLibrary(ctx, libID.String())
    if err != nil {
        return nil, 0, fmt.Errorf("count items: %w", err)
    }

    result := make([]media.Item, len(items))
    for i, item := range items {
        result[i] = *mapSqlcItemToDomain(item)
    }
    return result, int(total), nil
}

func (r *ItemRepository) Create(ctx context.Context, item *media.Item) error {
    return r.q.CreateItem(ctx, sqlc.CreateItemParams{
        ID:        item.ID.String(),
        LibraryID: item.LibraryID.String(),
        ParentID:  nullString(item.ParentID),
        Type:      string(item.Type),
        Title:     item.Title,
        SortTitle: item.SortTitle,
        // ... etc
    })
}

// ─── Mapper helpers ───

func mapSqlcItemToDomain(row sqlc.Item) *media.Item {
    return &media.Item{
        ID:        uuid.MustParse(row.ID),
        LibraryID: uuid.MustParse(row.LibraryID),
        ParentID:  parseNullUUID(row.ParentID),
        Type:      media.ItemType(row.Type),
        Title:     row.Title,
        SortTitle: row.SortTitle,
        Year:      int(row.Year.Int64),
        Path:      row.Path.String,
        Duration:  time.Duration(row.DurationTicks) * 100, // ticks → nanoseconds
        // ... etc
    }
}

func nullString(id *uuid.UUID) sql.NullString {
    if id == nil {
        return sql.NullString{}
    }
    return sql.NullString{String: id.String(), Valid: true}
}
```

---

## 6. Repositories Struct (wiring helper)

```go
// internal/db/repos.go
type Repositories struct {
    Items      *ItemRepository
    Libraries  *LibraryRepository
    Users      *UserRepository
    Sessions   *SessionRepository
    Metadata   *MetadataRepository
    Images     *ImageRepository
    ExternalIDs *ExternalIDRepository
    People     *PeopleRepository
    Channels   *ChannelRepository
    EPG        *EPGRepository
    Progress   *ProgressRepository
    Favorites  *FavoriteRepository
    Streams    *StreamRepository
    Trickplay  *TrickplayRepository
    Webhooks   *WebhookRepository
    WebhookLog *WebhookLogRepository
    Plugins    *PluginRepository
    Federation *FederationRepository
    Identity   *IdentityRepository
    Activity   *ActivityRepository
}

func NewRepositories(database *sql.DB) *Repositories {
    return &Repositories{
        Items:      NewItemRepository(database),
        Libraries:  NewLibraryRepository(database),
        Users:      NewUserRepository(database),
        Sessions:   NewSessionRepository(database),
        Metadata:   NewMetadataRepository(database),
        // ... etc — cada uno crea su propio sqlc.Queries
    }
}
```

---

## 7. Transacciones

Para operaciones que tocan múltiples tablas:

```go
// internal/db/tx.go
func WithTx(ctx context.Context, db *sql.DB, fn func(q *sqlc.Queries) error) error {
    tx, err := db.BeginTx(ctx, nil)
    if err != nil {
        return fmt.Errorf("begin tx: %w", err)
    }
    defer tx.Rollback()

    q := sqlc.New(tx)
    if err := fn(q); err != nil {
        return err
    }

    return tx.Commit()
}

// Uso en scanner (crear item + metadata + streams en una tx)
func (r *ItemRepository) CreateWithMetadata(ctx context.Context, item *media.Item, meta *media.Metadata) error {
    return WithTx(ctx, r.db, func(q *sqlc.Queries) error {
        if err := q.CreateItem(ctx, mapToCreateParams(item)); err != nil {
            return fmt.Errorf("create item: %w", err)
        }
        if err := q.UpsertMetadata(ctx, mapToMetadataParams(meta)); err != nil {
            return fmt.Errorf("upsert metadata: %w", err)
        }
        for _, stream := range item.MediaStreams {
            if err := q.CreateMediaStream(ctx, mapToStreamParams(item.ID, stream)); err != nil {
                return fmt.Errorf("create stream: %w", err)
            }
        }
        return nil
    })
}
```

---

## 8. PostgreSQL Support

sqlc soporta múltiples engines. Para PostgreSQL:

```yaml
# sqlc.yaml — sección adicional para postgres
sql:
  - engine: "sqlite"
    queries: "internal/db/queries/sqlite/"
    schema: "migrations/sqlite/"
    gen:
      go:
        package: "sqlitegen"
        out: "internal/db/sqlitegen"

  - engine: "postgresql"
    queries: "internal/db/queries/postgres/"
    schema: "migrations/postgres/"
    gen:
      go:
        package: "pggen"
        out: "internal/db/pggen"
```

El repository wrapper usa una interfaz común y selecciona la implementación según config:

```go
func NewRepositories(database *sql.DB, driver string) *Repositories {
    switch driver {
    case "postgres":
        return newPostgresRepos(database)
    default:
        return newSQLiteRepos(database)
    }
}
```

---

## 9. Build Integration

```makefile
# Makefile
sqlc:              ## Generate Go code from SQL queries
	sqlc generate

sqlc-check:        ## Verify queries are valid (CI)
	sqlc compile

sqlc-diff:         ## Show what would change
	sqlc diff
```

`sqlc compile` en CI verifica que los queries son válidos contra el schema sin generar código.

---

## 10. Directory Structure Final

```
internal/db/
├── queries/
│   ├── items.sql                # Queries para items
│   ├── users.sql                # Queries para users
│   ├── progress.sql             # Queries para watch progress
│   ├── channels.sql             # Queries para IPTV channels
│   ├── federation.sql           # Queries para federation
│   └── ...
├── sqlc/                        # ⚠️ GENERADO — no editar
│   ├── db.go
│   ├── models.go
│   ├── querier.go
│   ├── items.sql.go
│   ├── users.sql.go
│   └── ...
├── item_repository.go           # Wrapper: sqlc → domain
├── user_repository.go
├── progress_repository.go
├── channel_repository.go
├── federation_repository.go
├── repos.go                     # Repositories struct + constructor
├── tx.go                        # Transaction helper
├── sqlite.go                    # SQLite connection + config
└── postgres.go                  # PostgreSQL connection + config
```
