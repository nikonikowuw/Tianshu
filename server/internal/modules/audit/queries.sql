-- name: InsertAuditLog :exec
INSERT INTO audit_logs (
    id,
    actor_id,
    actor_username,
    action,
    resource,
    resource_id,
    status,
    ip_address,
    user_agent,
    request_id,
    metadata,
    created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now());

-- name: CountAuditLogsAll :one
SELECT count(*)::bigint
FROM audit_logs;

-- name: CountAuditLogsByTimeRange :one
SELECT count(*)::bigint
FROM audit_logs
WHERE created_at >= @from_at::timestamptz
  AND created_at <= @to_at::timestamptz;

-- name: CountAuditLogs :one
SELECT count(*)::bigint
FROM audit_logs
WHERE (@q::text = '' OR (
        actor_username ILIKE '%' || @q::text || '%' ESCAPE '\' OR
        action ILIKE '%' || @q::text || '%' ESCAPE '\' OR
        resource ILIKE '%' || @q::text || '%' ESCAPE '\' OR
        resource_id ILIKE '%' || @q::text || '%' ESCAPE '\' OR
        request_id ILIKE '%' || @q::text || '%' ESCAPE '\' OR
        ip_address ILIKE '%' || @q::text || '%' ESCAPE '\'
    ))
  AND (@actor::text = '' OR actor_username ILIKE '%' || @actor::text || '%' ESCAPE '\')
  AND (@has_actor_id::boolean = false OR actor_id = @actor_id::uuid)
  AND (cardinality(@actions::text[]) = 0 OR action = ANY(@actions::text[]))
  AND (cardinality(@resources::text[]) = 0 OR resource = ANY(@resources::text[]))
  AND (cardinality(@statuses::text[]) = 0 OR status = ANY(@statuses::text[]))
  AND (@has_from::boolean = false OR created_at >= @from_at::timestamptz)
  AND (@has_to::boolean = false OR created_at <= @to_at::timestamptz);

-- name: ListAuditLogsRecent :many
SELECT
    id,
    actor_id,
    actor_username,
    action,
    resource,
    resource_id,
    status,
    ip_address,
    user_agent,
    request_id,
    metadata,
    created_at
FROM audit_logs
ORDER BY created_at DESC, id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: ListAuditLogsOldest :many
SELECT
    id,
    actor_id,
    actor_username,
    action,
    resource,
    resource_id,
    status,
    ip_address,
    user_agent,
    request_id,
    metadata,
    created_at
FROM audit_logs
ORDER BY created_at ASC, id ASC
LIMIT @page_limit OFFSET @page_offset;

-- name: ListAuditLogsByTimeRangeRecent :many
SELECT
    id,
    actor_id,
    actor_username,
    action,
    resource,
    resource_id,
    status,
    ip_address,
    user_agent,
    request_id,
    metadata,
    created_at
FROM audit_logs
WHERE created_at >= @from_at::timestamptz
  AND created_at <= @to_at::timestamptz
ORDER BY created_at DESC, id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: ListAuditLogsByTimeRangeOldest :many
SELECT
    id,
    actor_id,
    actor_username,
    action,
    resource,
    resource_id,
    status,
    ip_address,
    user_agent,
    request_id,
    metadata,
    created_at
FROM audit_logs
WHERE created_at >= @from_at::timestamptz
  AND created_at <= @to_at::timestamptz
ORDER BY created_at ASC, id ASC
LIMIT @page_limit OFFSET @page_offset;

-- name: ListAuditLogs :many
SELECT
    id,
    actor_id,
    actor_username,
    action,
    resource,
    resource_id,
    status,
    ip_address,
    user_agent,
    request_id,
    metadata,
    created_at
FROM audit_logs
WHERE (@q::text = '' OR (
        actor_username ILIKE '%' || @q::text || '%' ESCAPE '\' OR
        action ILIKE '%' || @q::text || '%' ESCAPE '\' OR
        resource ILIKE '%' || @q::text || '%' ESCAPE '\' OR
        resource_id ILIKE '%' || @q::text || '%' ESCAPE '\' OR
        request_id ILIKE '%' || @q::text || '%' ESCAPE '\' OR
        ip_address ILIKE '%' || @q::text || '%' ESCAPE '\'
    ))
  AND (@actor::text = '' OR actor_username ILIKE '%' || @actor::text || '%' ESCAPE '\')
  AND (@has_actor_id::boolean = false OR actor_id = @actor_id::uuid)
  AND (cardinality(@actions::text[]) = 0 OR action = ANY(@actions::text[]))
  AND (cardinality(@resources::text[]) = 0 OR resource = ANY(@resources::text[]))
  AND (cardinality(@statuses::text[]) = 0 OR status = ANY(@statuses::text[]))
  AND (@has_from::boolean = false OR created_at >= @from_at::timestamptz)
  AND (@has_to::boolean = false OR created_at <= @to_at::timestamptz)
ORDER BY
    CASE WHEN @sort_by::text = 'created_at' AND @sort_order::text = 'asc' THEN created_at END ASC,
    CASE WHEN @sort_by::text = 'created_at' AND @sort_order::text = 'desc' THEN created_at END DESC,
    CASE WHEN @sort_by::text = 'action' AND @sort_order::text = 'asc' THEN action END ASC,
    CASE WHEN @sort_by::text = 'action' AND @sort_order::text = 'desc' THEN action END DESC,
    CASE WHEN @sort_by::text = 'resource' AND @sort_order::text = 'asc' THEN resource END ASC,
    CASE WHEN @sort_by::text = 'resource' AND @sort_order::text = 'desc' THEN resource END DESC,
    CASE WHEN @sort_by::text = 'status' AND @sort_order::text = 'asc' THEN status END ASC,
    CASE WHEN @sort_by::text = 'status' AND @sort_order::text = 'desc' THEN status END DESC,
    CASE WHEN @sort_by::text = 'created_at' AND @sort_order::text = 'asc' THEN id END ASC,
    created_at DESC,
    id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: DeleteExpiredAuditLogsBatch :execrows
WITH batch AS (
    SELECT al.id
    FROM audit_logs AS al
    WHERE al.created_at < $1
    ORDER BY al.created_at ASC, al.id ASC
    LIMIT $2
    FOR UPDATE SKIP LOCKED
)
DELETE FROM audit_logs AS target
USING batch
WHERE target.id = batch.id;
