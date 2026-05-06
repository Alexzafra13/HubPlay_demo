-- +goose Up
-- The external_ids table's PK is (item_id, provider), which makes
-- "what external ids does this item have?" fast but the reverse
-- lookup ("which local item carries this TMDb id?") a full scan.
-- The recommendations endpoint does up to 20 reverse lookups per
-- call (one per TMDb candidate) to mark which suggestions the user
-- already has in their library; without this index the cost grows
-- linearly with library size.
CREATE INDEX IF NOT EXISTS idx_external_ids_provider_id
    ON external_ids (provider, external_id);
