-- +goose Up
-- Global "trending this week" rail index. See SQLite sibling for
-- design notes. Direct translation.
CREATE INDEX IF NOT EXISTS idx_user_data_last_played
    ON user_data(last_played_at DESC);
