-- +goose Up
--
-- 034_user_profiles.sql — Netflix-style profile model on top of `users`.
--
-- A "profile" is just a row in `users` with `parent_user_id` set. The
-- parent row carries the credentials (password_hash) and is the only
-- thing that performs an actual auth handshake; child profiles share
-- the parent's account but each gets their own user_data, favourites,
-- preferences and activity history because every existing per-user
-- table is already keyed on users.id.
--
-- Adding the columns this way is intentionally additive — every
-- pre-existing row has parent_user_id NULL (= account owner) and the
-- new flags default off, so the migration is a no-op for any deploy
-- that hasn't yet created profiles.
--
-- Columns:
--   parent_user_id           NULL = top-level account, otherwise FK
--                            to the parent's users.id. Profiles cascade
--                            on parent delete so removing an account
--                            takes its profiles with it (one less
--                            cleanup path to forget about).
--   pin_hash                 bcrypt of the optional 4-digit PIN. NULL =
--                            "anyone can switch into this profile".
--                            Stored hashed using the same crypto path
--                            as password_hash so a DB leak doesn't
--                            give up child PINs.
--   max_content_rating       per-profile cap. NULL = "no restriction".
--                            Stored as the rating literal ("PG", "R",
--                            "TV-14", ...); the per-profile filter
--                            consults a hard-coded ranking table at
--                            query time so adding new rating systems
--                            doesn't require a schema change.
--   password_change_required When the admin creates a user with an
--                            auto-generated password, or resets one,
--                            this flag forces the next login flow to
--                            land on a ChangePassword screen before
--                            anything else.

ALTER TABLE users ADD COLUMN parent_user_id TEXT
    REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE users ADD COLUMN pin_hash TEXT;

ALTER TABLE users ADD COLUMN max_content_rating TEXT;

ALTER TABLE users ADD COLUMN password_change_required BOOLEAN NOT NULL DEFAULT 0;

-- Look up a parent's profiles (and the parent itself) in one ORed
-- query without a self-join. Covers `WHERE id = ? OR parent_user_id = ?`
-- which is what the login response and "Switch profile" flow run.
CREATE INDEX IF NOT EXISTS idx_users_parent_user_id
    ON users(parent_user_id);
