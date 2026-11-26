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
