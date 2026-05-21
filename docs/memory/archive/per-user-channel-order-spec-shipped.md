# Per-user channel order + hide — SHIPPED

> Status: **DONE** (verificado 2026-05-21). Implementación end-to-end
> en main: migraciones `042_user_channel_order.sql` (sqlite + postgres)
> y `043_library_channel_order.sql` para el counterpart admin, repos
> en `internal/db/{user,library}_channel_order_repository.go`,
> servicio en `internal/iptv/service_channel_order.go`, handlers en
> `internal/api/handlers/iptv_personalisation.go` (rutas
> `/me/iptv/channels/order` PUT/DELETE y
> `/me/iptv/channels/{channelId}/visibility` PUT), SSE
> `publishOrderUpdated` para sync entre dispositivos, página user-side
> en `web/src/pages/LiveTvCustomize.tsx` (237 LoC + 232 LoC test) y
> panel admin `web/src/components/admin/AdminChannelOrderPanel.tsx`.
>
> El doc original (spec inicial) queda abajo como referencia
> arqueológica — la implementación final difiere en algunos detalles
> (rutas bajo `/me/iptv/*` en lugar de `/me/channels/*`, ReplaceAll en
> un transaction en lugar de bulk reorder, dos tablas overlay
> componibles user+admin en lugar de una sola).

---

# Per-user channel order + hide — pending feature (spec original)

> Status: **NOT IMPLEMENTED**. Specced 2026-05-13 during the Live TV
> polish sessions; left for a dedicated future session because it
> touches DB schema, service, API and both clients.

## Motivation

Today's channel order is **global**: the `channels.number` column is
either what the M3U declared (when `tvg-chno` / `channel-number` is
set) or an importer-assigned positional fallback. Every user sees the
exact same dial.

What we want: each authenticated user can **renumber** ("put channel
35 on slot 3") and **hide** channels from their personal list,
without affecting other users or being wiped by an M3U refresh.

Existing per-user surfaces in the codebase that work the same way and
serve as the pattern:

- `favorites/channels` — `(user_id, channel_id)` rows; library ACL
  applied at read-time.
- `me/channels/continue-watching` — joined against
  `channel_watch_history`, also keyed by `user_id`.

Existing **global** surface that should NOT be confused with this:

- `channel_overrides` (in `internal/iptv/service_overrides.go`):
  admin-only, library-scoped, persists `tvg_id` / `stream_url`
  overrides so manual edits survive a re-import. NOT per-user.

## Confirmed state of "channels with no signal"

Already handled, **no work needed here**. The user-facing list comes
from `db.ChannelRepository.ListHealthyByLibrary`, which SQL-filters
out:
- Inactive channels (`is_active = false`)
- Unhealthy channels (`consecutive_failures >= db.UnhealthyThreshold`)

Confirmed by:
- `db/channel_repository.go:339-343` doc comment and query
- `db/channel_health_test.go::TestChannel_ListHealthyByLibrary_HidesUnhealthyAndDisabled`

The HTTP handler `ListChannels` uses `activeOnly = !"false"` (default
true), so the default user-facing path is already healthy-only. Admin
callers pass `?active=false` to see the full list including dead /
disabled tiles.

`degraded` channels (1+ failures but below threshold) still surface —
intentional, because a one-off network blip shouldn't make a working
channel vanish. If we ever want a stricter "hide degraded too"
setting, that's a separate flag.

## Proposed shape

### DB

New table (SQLite + Postgres):

```sql
CREATE TABLE user_channel_preferences (
  user_id       TEXT NOT NULL,
  channel_id    TEXT NOT NULL,
  custom_number INTEGER,        -- NULL = use default channel.number
  is_hidden     BOOLEAN NOT NULL DEFAULT FALSE,
  updated_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (user_id, channel_id),
  FOREIGN KEY (user_id)    REFERENCES users(id)    ON DELETE CASCADE,
  FOREIGN KEY (channel_id) REFERENCES channels(id) ON DELETE CASCADE
);

CREATE INDEX idx_ucp_user_visible
  ON user_channel_preferences (user_id, is_hidden, custom_number);
```

Rationale:
- Composite PK (`user_id`, `channel_id`) means natural Upsert
  semantics with no surrogate id.
- `custom_number` is nullable so a user can hide a channel without
  having to renumber it, and vice versa.
- Cascade deletes mean removing a user or a channel cleans up its
  preferences automatically — no orphaned rows.

### Repository

```go
type UserChannelPreferencesRepository interface {
    ListForUser(ctx, userID) ([]*UserChannelPreference, error)
    Upsert(ctx, *UserChannelPreference) error
    Delete(ctx, userID, channelID) error
    // Atomic bulk reorder: write a slice of (channel_id, position)
    // as a single transaction so the user never sees a partial state.
    Reorder(ctx, userID, ordered []ChannelOrderEntry) error
}
```

### Service

Add a new method on `iptv.Service`:

```go
ListHealthyByLibraryForUser(ctx, libraryID, userID string) ([]*db.Channel, error)
```

Behaviour:
1. Start from `ListHealthyByLibrary(libraryID)`.
2. Left-join the user preferences in memory (or in SQL — TBD by
   profiler).
3. Filter out `is_hidden = true`.
4. Replace `channel.Number` with `custom_number` when set.
5. Sort by the effective number ascending.

The existing `ListHealthyByLibrary` keeps working for admin / cross-
user callers (e.g. the prober). Only the user-facing handler swaps
to the new method.

### Endpoints

All gated by JWT auth + library ACL:

| Verb   | Path                                    | Body                                 | Notes |
|--------|-----------------------------------------|--------------------------------------|-------|
| GET    | `/api/v1/me/channels/preferences`       | —                                    | Returns the user's full preference map |
| POST   | `/api/v1/me/channels/reorder`           | `{ "library_id": "...", "order": [channelId1, channelId2, ...] }` | Atomic bulk update; positions assigned by array index |
| PUT    | `/api/v1/me/channels/{channelId}/number`| `{ "number": 7 }`                    | Single-channel renumber |
| PUT    | `/api/v1/me/channels/{channelId}/hide`  | —                                    | Sets `is_hidden=true` |
| DELETE | `/api/v1/me/channels/{channelId}/hide`  | —                                    | Sets `is_hidden=false` |
| DELETE | `/api/v1/me/channels/{channelId}/preferences` | —                              | Reset to global defaults |

`ListChannels` (the existing `GET /libraries/{id}/channels`) needs
a tweak: when authenticated, dispatch through the new
`ListHealthyByLibraryForUser`. For unauthenticated / admin paths
(`?active=false`), keep the current behaviour.

### Clients

**Android TV (Kotlin)**:
- New sidebar entry "Editar canales" (only in Live TV section).
- Edit screen: same EPG row layout in read-only mode, but with a
  D-pad mode toggle (long-press OK on a row → enters "move mode";
  ↑/↓ reorder, OK confirms, BACK cancels).
- Optional: "Ocultar" toggle in the same context menu.

**Web (React)**:
- Drag-and-drop on the channels list (react-dnd or HTML5 DnD).
- "Hide" icon per row.
- "Reset to default" button at the top.

Both clients call the same endpoints. The reorder UI optimistically
updates local state then posts to `/me/channels/reorder`.

### Tests

- Repository: round-trip Upsert / Delete / Reorder, cascade behaviour.
- Service: `ListHealthyByLibraryForUser` honours custom_number,
  hides `is_hidden`, falls back to default when no preference exists.
- Handler: 401 without auth; library ACL enforced; reorder rejects
  channel IDs outside the requested library.

## Estimated effort

- **Mínimo viable** (~1 session): table + repo + service +
  `/me/channels/reorder` + minimal Android TV "move up/down by 1"
  affordance. **~600 lines Go + 200 lines Kotlin.**
- **Completo** (~2 sessions): adds hide/unhide, web drag-and-drop,
  per-category reordering. **~+400 lines Go + ~400 lines TS/Kotlin.**

## Open questions for the dedicated session

1. **Number collisions**: if user A puts the old channel 7 on slot 3,
   what happens to whatever was on slot 3 before? Options:
   - Bump everything below by 1 (rolling renumber).
   - Allow gaps / collisions and break ties by original channel.number.
   - Force a full reorder by sending the entire ordered list.
   The bulk `reorder` endpoint sidesteps this — only single-channel
   renumber has the ambiguity.
2. **Multi-library libraries**: a user with two IPTV libraries — do
   they get a global numbering plane or per-library? Per-library is
   simpler; global needs another column in the prefs.
3. **Reset semantics**: should "reset all" be a single DELETE on the
   user's whole prefs row set, or a per-channel action?
4. **Sync across devices**: SSE `/me/events` already exists; we'd
   emit a `PreferencesUpdated` event so a reorder on TV reflects
   instantly on the web.
