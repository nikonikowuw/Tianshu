-- name: CountUsers :one
SELECT count(*)::bigint
FROM users
WHERE deleted_at IS NULL;

-- name: CreateUser :one
INSERT INTO users (
    id,
    username,
    email,
    password_hash,
    display_name,
    status,
    created_by,
    updated_by,
    created_at,
    updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now(), now())
RETURNING id, username, email, display_name, status, created_at, updated_at;

-- name: GetUserForAuthByIdentifier :one
SELECT id, username, email, password_hash, display_name, status
FROM users
WHERE deleted_at IS NULL
  AND (lower(username) = $1 OR lower(email) = $1)
LIMIT 1;

-- name: GetUserByID :one
SELECT id, username, email, display_name, status, created_at, updated_at
FROM users
WHERE id = $1
  AND deleted_at IS NULL;

-- name: UpdateUserLastLogin :exec
UPDATE users
SET last_login_at = now(),
    updated_at = now()
WHERE id = $1
  AND deleted_at IS NULL;

-- name: UpdateUserPasswordHash :exec
UPDATE users
SET password_hash = $2,
    updated_at = now()
WHERE id = $1
  AND deleted_at IS NULL;
