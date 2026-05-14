-- +goose Up
-- Primary admin auto-grants on library_access. See SQLite sibling
-- for the full rationale (oldest top-level admin gets explicit grants
-- so the household-matrix UI doesn't show empty).
--
-- Postgres translation: INSERT OR IGNORE → INSERT … ON CONFLICT DO
-- NOTHING. The rest (CROSS JOIN with a single-row subquery, ORDER BY
-- created_at + LIMIT 1) is standard SQL.
INSERT INTO library_access (user_id, library_id)
SELECT primary_admin.id, l.id
FROM libraries l
CROSS JOIN (
    SELECT id FROM users
    WHERE role = 'admin' AND parent_user_id IS NULL
    ORDER BY created_at ASC
    LIMIT 1
) AS primary_admin
ON CONFLICT DO NOTHING;
