-- name: InsertDeviceCode :exec
INSERT INTO device_codes (
    device_code, user_code, device_name, expires_at, created_at
) VALUES (
    ?, ?, ?, ?, ?
);

-- name: GetDeviceCodeByDeviceCode :one
SELECT *
FROM   device_codes
WHERE  device_code = ?;

-- name: GetDeviceCodeByUserCode :one
SELECT *
FROM   device_codes
WHERE  user_code = ?;

-- name: ApproveDeviceCode :exec
UPDATE device_codes
SET    user_id = ?,
       approved_at = ?
WHERE  user_code = ?
  AND  user_id IS NULL
  AND  expires_at > ?;

-- name: ConsumeDeviceCode :exec
UPDATE device_codes
SET    consumed_at = ?
WHERE  device_code = ?
  AND  consumed_at IS NULL;

-- name: TouchDeviceCodePollAt :exec
UPDATE device_codes
SET    last_polled_at = ?
WHERE  device_code = ?;

-- name: DeleteExpiredDeviceCodes :exec
DELETE FROM device_codes
WHERE expires_at < ?
   OR consumed_at IS NOT NULL;
