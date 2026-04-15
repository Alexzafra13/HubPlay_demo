-- JWT signing keys for rotation with overlap.
--
-- Table schema lives in migrations/sqlite/004_jwt_signing_keys.sql.
-- The repository (signing_key_repository.go) is a thin adapter around
-- these queries. Rotation/overlap policy lives in internal/auth/keystore.go.

-- name: CreateSigningKey :exec
INSERT INTO jwt_signing_keys (id, secret, created_at, retired_at)
VALUES (?, ?, ?, ?);

-- name: GetSigningKey :one
SELECT id, secret, created_at, retired_at
FROM jwt_signing_keys
WHERE id = ?;

-- name: ListActiveSigningKeys :many
SELECT id, secret, created_at, retired_at
FROM jwt_signing_keys
WHERE retired_at IS NULL
ORDER BY created_at DESC;

-- name: ListSigningKeys :many
SELECT id, secret, created_at, retired_at
FROM jwt_signing_keys
ORDER BY created_at DESC;

-- name: SetSigningKeyRetiredAt :execrows
UPDATE jwt_signing_keys
SET retired_at = ?
WHERE id = ?;

-- name: DeleteRetiredSigningKeysBefore :execrows
DELETE FROM jwt_signing_keys
WHERE retired_at IS NOT NULL
  AND retired_at < ?;
