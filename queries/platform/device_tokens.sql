-- name: GetDeviceTokens :many
SELECT * FROM device_tokens WHERE user_id = $1;

-- name: CreateDeviceToken :exec
INSERT INTO device_tokens (user_id, token, platform)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, token) DO NOTHING;

-- name: DeleteDeviceToken :exec
DELETE FROM device_tokens WHERE token = $1 AND user_id = $2;

-- name: DeleteAllUserDeviceTokens :exec
DELETE FROM device_tokens WHERE user_id = $1;
