-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: CreateUser :one
INSERT INTO users (email, password_hash, display_name)
VALUES ($1, $2, $3)
RETURNING *;

-- name: UpdateUser :one
UPDATE users
SET display_name = $2, avatar_url = $3, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeactivateUser :exec
UPDATE users SET is_active = false, updated_at = now() WHERE id = $1;

-- name: ListUsers :many
SELECT * FROM users WHERE is_active = true ORDER BY created_at DESC LIMIT $1 OFFSET $2;
