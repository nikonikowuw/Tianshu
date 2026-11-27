-- name: GetUserStats :one
SELECT
    count(*) FILTER (WHERE deleted_at IS NULL)::bigint AS total_users,
    count(*) FILTER (WHERE deleted_at IS NULL AND status = 'active')::bigint AS active_users,
    count(*) FILTER (WHERE deleted_at IS NULL AND status = 'disabled')::bigint AS disabled_users,
    count(*) FILTER (WHERE deleted_at IS NULL AND created_at >= @since::timestamptz)::bigint AS new_users_last_7_days
FROM users;

-- name: GetRoleStats :one
SELECT
    count(*)::bigint AS total_roles,
    count(*) FILTER (WHERE is_system = true)::bigint AS system_roles,
    count(*) FILTER (WHERE is_system = false)::bigint AS custom_roles
FROM roles;

-- name: GetFileStats :one
SELECT
    count(*)::bigint AS total_files,
    coalesce(sum(size_bytes), 0)::bigint AS total_size_bytes,
    count(*) FILTER (WHERE mime_type LIKE 'image/%')::bigint AS image_files,
    count(*) FILTER (WHERE mime_type NOT LIKE 'image/%')::bigint AS document_files
FROM files
WHERE status = 'ready';

-- name: CountActiveSessions :one
SELECT count(*)::bigint
FROM user_sessions AS us
INNER JOIN users AS u ON u.id = us.user_id
WHERE us.revoked_at IS NULL
  AND us.expires_at > @now::timestamptz
  AND u.deleted_at IS NULL
  AND u.status = 'active';

-- name: ListDailyAuditActivity :many
SELECT
    to_char((created_at AT TIME ZONE 'UTC')::date, 'YYYY-MM-DD')::text AS day,
    count(*) FILTER (WHERE action = 'auth.login' AND status = 'success')::bigint AS logins,
    count(*) FILTER (WHERE action <> 'auth.login' AND status = 'success')::bigint AS operations,
    count(*) FILTER (WHERE status = 'failure')::bigint AS failures
FROM audit_logs
WHERE created_at >= @start_at::timestamptz
  AND created_at < @end_at::timestamptz
GROUP BY (created_at AT TIME ZONE 'UTC')::date
ORDER BY (created_at AT TIME ZONE 'UTC')::date ASC;

-- name: ListRecentAuditActivity :many
SELECT
    id,
    actor_username,
    action,
    resource,
    resource_id,
    status,
    ip_address,
    created_at
FROM audit_logs
ORDER BY created_at DESC, id DESC
LIMIT @row_limit;
