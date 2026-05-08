-- +goose Up
--
-- 035_user_access_expires_at.sql — temporary-access window for users.
--
-- The admin can grant access "for 1 day / 3 days / 1 week / 1 month /
-- 3 months / 1 year / permanent". `access_expires_at` is the deadline
-- after which Login + the auth middleware reject the user; cleared
-- (NULL) means "no time limit", which is the default for everything
-- created before this migration.
--
-- Lazy expiry: we don't run a background job to flip the flag at the
-- precise moment of expiry. Login + middleware compare against
-- time.Now() on every request, and the JWT TTL (15 min default) is
-- the worst-case window during which an already-issued token can
-- outlive its own access. That trade-off is acceptable for the
-- household / small-deploy use case the field exists for.
--
-- The pre-existing `is_active` column stays the canonical "this user
-- is currently disabled" flag — the admin can flip it manually for
-- an indefinite suspension. `access_expires_at` is the time-bound
-- variant that NULL-coalesces to "permanent" when omitted.

ALTER TABLE users ADD COLUMN access_expires_at DATETIME;

-- The middleware filter is `access_expires_at IS NULL OR
-- access_expires_at > now()` — covered by an index so a 100-user
-- deployment doesn't full-scan the table on every request.
CREATE INDEX IF NOT EXISTS idx_users_access_expires_at
    ON users(access_expires_at)
    WHERE access_expires_at IS NOT NULL;
