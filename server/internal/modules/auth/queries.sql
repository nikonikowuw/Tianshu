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

-- name: RevokeSession :one
UPDATE user_sessions
SET revoked_at = now()
WHERE id = $1
  AND revoked_at IS NULL
RETURNING id, user_id;

-- name: RevokeSessionByRefreshHash :one
UPDATE user_sessions
SET revoked_at = now()
WHERE refresh_token_hash = $1
  AND revoked_at IS NULL
RETURNING id, user_id;

-- name: RevokeAllUserSessions :exec
UPDATE user_sessions
SET revoked_at = now()
WHERE user_id = $1
  AND revoked_at IS NULL;

-- name: ListActiveUserSessions :many
SELECT id, user_id, user_agent, ip_address, expires_at, last_used_at, created_at
FROM user_sessions
WHERE user_id = $1
  AND revoked_at IS NULL
  AND expires_at > now()
ORDER BY last_used_at DESC, created_at DESC, id DESC;

-- name: IsUserSessionActive :one
SELECT EXISTS (
    SELECT 1
    FROM user_sessions
    WHERE id = $1
      AND user_id = $2
      AND revoked_at IS NULL
      AND expires_at > now()
)::boolean;

-- name: LockActiveUserSessionForShare :one
SELECT id
FROM user_sessions
WHERE id = $1
  AND user_id = $2
  AND revoked_at IS NULL
  AND expires_at > now()
FOR SHARE;

-- name: RevokeUserSessionByID :execrows
UPDATE user_sessions
SET revoked_at = now()
WHERE id = $1
  AND user_id = $2
  AND revoked_at IS NULL
  AND expires_at > now();

-- name: RevokeOtherUserSessions :execrows
UPDATE user_sessions
SET revoked_at = now()
WHERE user_id = $1
  AND id <> $2
  AND revoked_at IS NULL
  AND expires_at > now();

-- name: DeleteExpiredRefreshTokenHistoryBatch :execrows
DELETE FROM refresh_token_history
WHERE token_hash IN (
    SELECT rth.token_hash
    FROM refresh_token_history AS rth
    WHERE rth.expires_at < $1
    ORDER BY rth.expires_at ASC, rth.token_hash ASC
    LIMIT $2
);

-- name: DeleteExpiredUserSessionsBatch :execrows
DELETE FROM user_sessions
WHERE id IN (
    SELECT us.id
    FROM user_sessions AS us
    WHERE us.expires_at < $1
    ORDER BY us.expires_at ASC, us.id ASC
    LIMIT $2
);
