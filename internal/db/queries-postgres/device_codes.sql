-- name: InsertDeviceCode :exec
INSERT INTO device_codes (
    device_code, user_code, device_name, expires_at, created_at
) VALUES (
    $1, $2, $3, $4, $5
);

-- name: GetDeviceCodeByDeviceCode :one
SELECT *
FROM   device_codes
WHERE  device_code = $1;

-- name: GetDeviceCodeByUserCode :one
SELECT *
FROM   device_codes
WHERE  user_code = $1;

-- name: ApproveDeviceCode :exec
UPDATE device_codes
SET    user_id = $1,
       approved_at = $2
WHERE  user_code = $3
  AND  user_id IS NULL
  AND  expires_at > $4;

-- name: ConsumeDeviceCode :exec
UPDATE device_codes
SET    consumed_at = $1
WHERE  device_code = $2
  AND  consumed_at IS NULL;

-- name: TouchDeviceCodePollAt :exec
UPDATE device_codes
SET    last_polled_at = $1
WHERE  device_code = $2;

-- name: DeleteExpiredDeviceCodes :exec
DELETE FROM device_codes
WHERE expires_at < $1
   OR consumed_at IS NOT NULL;
