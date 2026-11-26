-- name: CreateSession :exec
INSERT INTO user_sessions (
    id,
    user_id,
    refresh_token_hash,
    user_agent,
    ip_address,
    expires_at,
    last_used_at,
    created_at
)
VALUES ($1, $2, $3, $4, $5, $6, now(), now());

-- name: GetSessionByRefreshHashForUpdate :one
SELECT id, user_id, refresh_token_hash, user_agent, ip_address, expires_at, revoked_at, last_used_at, created_at
FROM user_sessions
WHERE refresh_token_hash = $1
FOR UPDATE;

-- name: GetSessionByIDForUpdate :one
SELECT id, user_id, refresh_token_hash, user_agent, ip_address, expires_at, revoked_at, last_used_at, created_at
FROM user_sessions
WHERE id = $1
FOR UPDATE;

-- name: RotateSessionRefreshToken :exec
UPDATE user_sessions
SET refresh_token_hash = $2,
    user_agent = $3,
    ip_address = $4,
    expires_at = $5,
    last_used_at = now()
WHERE id = $1;

-- name: InsertRefreshTokenHistory :exec
INSERT INTO refresh_token_history (token_hash, session_id, replaced_at, expires_at)
VALUES ($1, $2, now(), $3)
ON CONFLICT (token_hash) DO NOTHING;

-- name: GetRefreshTokenHistoryByHash :one
SELECT token_hash, session_id, replaced_at, expires_at
FROM refresh_token_history
WHERE token_hash = $1;

-- name: RevokeSession :exec
UPDATE user_sessions
SET revoked_at = now()
WHERE id = $1
  AND revoked_at IS NULL;

-- name: RevokeSessionByRefreshHash :exec
UPDATE user_sessions
SET revoked_at = now()
WHERE refresh_token_hash = $1
  AND revoked_at IS NULL;

-- name: RevokeAllUserSessions :exec
UPDATE user_sessions
SET revoked_at = now()
WHERE user_id = $1
  AND revoked_at IS NULL;
