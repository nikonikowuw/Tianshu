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
SELECT id, username, email, password_hash, display_name, avatar_file_id, status
FROM users
WHERE deleted_at IS NULL
  AND (lower(username) = $1 OR lower(email) = $1)
LIMIT 1;

-- name: GetUserByID :one
SELECT id, username, email, display_name, avatar_file_id, status, created_at, updated_at
FROM users
WHERE id = $1
  AND deleted_at IS NULL;

-- name: GetUserPasswordHashByID :one
SELECT id, username, status, password_hash
FROM users
WHERE id = $1
  AND deleted_at IS NULL;

-- name: GetUserPasswordHashByIDForUpdate :one
SELECT id, username, status, password_hash
FROM users
WHERE id = $1
  AND deleted_at IS NULL
FOR UPDATE;

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

-- name: CountUsersFiltered :one
SELECT count(*)::bigint
FROM users u
WHERE u.deleted_at IS NULL
  AND (@q::text = '' OR (
        u.username ILIKE '%' || @q::text || '%' ESCAPE '\' OR
        u.email ILIKE '%' || @q::text || '%' ESCAPE '\' OR
        u.display_name ILIKE '%' || @q::text || '%' ESCAPE '\'
    ))
  AND (cardinality(@statuses::text[]) = 0 OR u.status = ANY(@statuses::text[]))
  AND (NOT @filter_ids::boolean OR u.id = ANY(@ids::uuid[]));

-- name: ListUsers :many
SELECT
    u.id,
    u.username,
    u.email,
    u.display_name,
    u.status,
    u.last_login_at,
    u.created_by,
    cb.username AS created_by_username,
    u.updated_by,
    ub.username AS updated_by_username,
    u.created_at,
    u.updated_at
FROM users u
LEFT JOIN users cb ON cb.id = u.created_by
LEFT JOIN users ub ON ub.id = u.updated_by
WHERE u.deleted_at IS NULL
  AND (@q::text = '' OR (
        u.username ILIKE '%' || @q::text || '%' ESCAPE '\' OR
        u.email ILIKE '%' || @q::text || '%' ESCAPE '\' OR
        u.display_name ILIKE '%' || @q::text || '%' ESCAPE '\'
    ))
  AND (cardinality(@statuses::text[]) = 0 OR u.status = ANY(@statuses::text[]))
  AND (NOT @filter_ids::boolean OR u.id = ANY(@ids::uuid[]))
ORDER BY
    CASE WHEN @sort_by::text = 'created_at' AND @sort_order::text = 'asc' THEN u.created_at END ASC,
    CASE WHEN @sort_by::text = 'created_at' AND @sort_order::text = 'desc' THEN u.created_at END DESC,
    CASE WHEN @sort_by::text = 'updated_at' AND @sort_order::text = 'asc' THEN u.updated_at END ASC,
    CASE WHEN @sort_by::text = 'updated_at' AND @sort_order::text = 'desc' THEN u.updated_at END DESC,
    CASE WHEN @sort_by::text = 'username' AND @sort_order::text = 'asc' THEN u.username END ASC,
    CASE WHEN @sort_by::text = 'username' AND @sort_order::text = 'desc' THEN u.username END DESC,
    CASE WHEN @sort_by::text = 'last_login_at' AND @sort_order::text = 'asc' THEN u.last_login_at END ASC NULLS LAST,
    CASE WHEN @sort_by::text = 'last_login_at' AND @sort_order::text = 'desc' THEN u.last_login_at END DESC NULLS LAST,
    u.created_at DESC,
    CASE WHEN @sort_order::text = 'asc' THEN u.id END ASC,
    u.id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: GetUserDetailByID :one
SELECT
    u.id,
    u.username,
    u.email,
    u.display_name,
    u.avatar_file_id,
    u.status,
    u.last_login_at,
    u.created_by,
    cb.username AS created_by_username,
    u.updated_by,
    ub.username AS updated_by_username,
    u.created_at,
    u.updated_at
FROM users u
LEFT JOIN users cb ON cb.id = u.created_by
LEFT JOIN users ub ON ub.id = u.updated_by
WHERE u.id = $1
  AND u.deleted_at IS NULL;

-- name: GetUserByIDForUpdate :one
SELECT
    id,
    username,
    email,
    display_name,
    avatar_file_id,
    status,
    created_at,
    updated_at
FROM users
WHERE id = $1
  AND deleted_at IS NULL
FOR UPDATE;

-- name: UpdateUserProfile :one
UPDATE users
SET email = $2,
    display_name = $3,
    status = $4,
    updated_by = $5,
    updated_at = now()
WHERE id = $1
  AND deleted_at IS NULL
RETURNING id;

-- name: UpdateOwnProfile :one
UPDATE users
SET email = $2,
    display_name = $3,
    avatar_file_id = $4,
    updated_by = $5,
    updated_at = now()
WHERE id = $1
  AND deleted_at IS NULL
RETURNING id;

-- name: UpdateUserPasswordWithActor :exec
UPDATE users
SET password_hash = $2,
    updated_by = $3,
    updated_at = now()
WHERE id = $1
  AND deleted_at IS NULL;

-- name: SoftDeleteUser :exec
UPDATE users
SET deleted_at = now(),
    updated_by = $2,
    updated_at = now()
WHERE id = $1
  AND deleted_at IS NULL;

-- name: CountActiveUsersByIDs :one
SELECT count(*)::bigint
FROM users
WHERE deleted_at IS NULL
  AND status = 'active'
  AND id = ANY(@ids::uuid[]);

-- name: ListUsernamesByIDs :many
SELECT id, username
FROM users
WHERE id = ANY(@ids::uuid[]);

-- name: CountActiveUsersByAvatarFileID :one
SELECT count(*)::bigint
FROM users
WHERE deleted_at IS NULL
  AND avatar_file_id = @file_id::uuid;
