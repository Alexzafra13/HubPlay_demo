-- People (cast + crew) and the join table that links them to items.
--
-- Schema: migrations/sqlite/001_initial_schema.sql
--   people(id, name, type, thumb_path)
--   item_people(item_id, person_id, role, character_name, sort_order)  PK on (item_id, person_id, role)
--
-- Deduplication: people are keyed in DB by uuid `id` but the scanner
-- needs a stable lookup from "the person TMDb just gave me". For now
-- we dedup by `name` (case-sensitive — TMDb is consistent) which is
-- good enough for the 95% case. A future migration can add a
-- person-level external_ids table for the edge cases (alias drift,
-- two actors with the same name).

-- name: GetPersonByName :one
SELECT id, name, COALESCE(type, '') AS type, COALESCE(thumb_path, '') AS thumb_path
FROM people
WHERE name = ?
LIMIT 1;

-- name: CreatePerson :exec
INSERT INTO people (id, name, type, thumb_path) VALUES (?, ?, ?, ?);

-- name: SetPersonThumbPath :exec
UPDATE people SET thumb_path = ? WHERE id = ?;

-- name: GetPersonByID :one
SELECT id, name, COALESCE(type, '') AS type, COALESCE(thumb_path, '') AS thumb_path
FROM people
WHERE id = ?;

-- Clear an item's existing cast/crew before re-inserting. Avoids
-- having to diff the new list against the old one — re-scans cost a
-- few writes per item rather than a comparison pass. The composite
-- PK on item_people means simple DELETE WHERE item_id = ? is the
-- whole story.
-- name: DeleteItemPeople :exec
DELETE FROM item_people WHERE item_id = ?;

-- name: InsertItemPerson :exec
INSERT INTO item_people (item_id, person_id, role, character_name, sort_order)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(item_id, person_id, role) DO UPDATE SET
    character_name = excluded.character_name,
    sort_order = excluded.sort_order;

-- name: ListItemPeople :many
-- Returns the cast/crew rows for an item joined with the person
-- record so the caller gets one struct per row. Ordered by
-- sort_order so TMDb's "billing position" ranking surfaces directly
-- to the UI without a client-side sort.
SELECT
    ip.person_id,
    ip.role,
    COALESCE(ip.character_name, '') AS character_name,
    COALESCE(ip.sort_order, 0) AS sort_order,
    p.name,
    COALESCE(p.type, '') AS person_type,
    COALESCE(p.thumb_path, '') AS thumb_path
FROM item_people ip
JOIN people p ON p.id = ip.person_id
WHERE ip.item_id = ?
ORDER BY ip.sort_order ASC, p.name ASC;
