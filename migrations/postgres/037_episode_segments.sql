-- +goose Up
-- Intro / outro / recap markers for skip-intro / skip-credits
-- affordance. See SQLite sibling for the full design rationale.
--
-- Postgres translation:
--   • INTEGER for start_ticks / end_ticks → BIGINT (ticks at
--     10M per second easily exceed 2^31 in a feature-length item).
--   • INTEGER for detected_at (unix epoch) → BIGINT for the same
--     reason (the original code stores time.Now().Unix() — already
--     fits in int32 today but BIGINT is safer + matches `bytes_out`
--     pattern in audit log).
--   • REAL for confidence → DOUBLE PRECISION
--   • CHECK constraints with IN / numeric comparisons identical.
CREATE TABLE episode_segments (
    item_id      TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    kind         TEXT NOT NULL CHECK (kind IN ('intro', 'outro', 'recap')),
    source       TEXT NOT NULL CHECK (source IN ('chapter', 'fingerprint', 'manual')),
    start_ticks  BIGINT NOT NULL CHECK (start_ticks >= 0),
    end_ticks    BIGINT NOT NULL CHECK (end_ticks > start_ticks),
    confidence   DOUBLE PRECISION NOT NULL DEFAULT 1.0
                  CHECK (confidence >= 0.0 AND confidence <= 1.0),
    detected_at  BIGINT NOT NULL,
    PRIMARY KEY (item_id, kind, source)
);

CREATE INDEX idx_episode_segments_item ON episode_segments(item_id);

-- +goose Down
DROP INDEX IF EXISTS idx_episode_segments_item;
DROP TABLE IF EXISTS episode_segments;
