-- name: InsertPendingFile :one
INSERT INTO files (
    id,
    uploader_id,
    storage_driver,
    object_key,
    original_name,
    mime_type,
    size_bytes,
    sha256,
    status,
    created_at,
    updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'pending', now(), now())
RETURNING id, uploader_id, storage_driver, object_key, original_name, mime_type, size_bytes, sha256, status, created_at, updated_at;

-- name: MarkFileReady :one
UPDATE files
SET status = 'ready',
    updated_at = now()
WHERE id = $1
  AND status = 'pending'
RETURNING id, uploader_id, storage_driver, object_key, original_name, mime_type, size_bytes, sha256, status, created_at, updated_at;

-- name: MarkFileStatus :exec
UPDATE files
SET status = $2,
    updated_at = now()
WHERE id = $1;

-- name: GetReadyFileByID :one
SELECT id, uploader_id, storage_driver, object_key, original_name, mime_type, size_bytes, sha256, status, created_at, updated_at
FROM files
WHERE id = $1
  AND status = 'ready';

-- name: GetFileByIDForUpdate :one
SELECT id, uploader_id, storage_driver, object_key, original_name, mime_type, size_bytes, sha256, status, created_at, updated_at
FROM files
WHERE id = $1
FOR UPDATE;

-- name: ClaimExpiredPendingFile :execrows
UPDATE files
SET status = 'deleting',
    updated_at = now()
WHERE id = $1
  AND status = 'pending'
  AND updated_at < @pending_cutoff::timestamptz;

-- name: TouchCleanupCandidate :exec
UPDATE files
SET updated_at = now()
WHERE id = $1
  AND status IN ('failed', 'deleting');

-- name: DeleteFileRow :exec
DELETE FROM files
WHERE id = $1
  AND status IN ('failed', 'deleting');

-- name: CountReadyFiles :one
SELECT count(*)::bigint
FROM files
WHERE status = 'ready'
  AND (@q::text = '' OR original_name ILIKE '%' || @q::text || '%' ESCAPE '\')
  AND (cardinality(@mime_types::text[]) = 0 OR mime_type = ANY(@mime_types::text[]))
  AND (cardinality(@drivers::text[]) = 0 OR storage_driver = ANY(@drivers::text[]))
  AND (
      cardinality(@categories::text[]) = 0
      OR ('image' = ANY(@categories::text[]) AND mime_type LIKE 'image/%')
      OR ('document' = ANY(@categories::text[]) AND mime_type NOT LIKE 'image/%')
  );

-- name: ListReadyFiles :many
SELECT id, uploader_id, storage_driver, object_key, original_name, mime_type, size_bytes, sha256, status, created_at, updated_at
FROM files
WHERE status = 'ready'
  AND (@q::text = '' OR original_name ILIKE '%' || @q::text || '%' ESCAPE '\')
  AND (cardinality(@mime_types::text[]) = 0 OR mime_type = ANY(@mime_types::text[]))
  AND (cardinality(@drivers::text[]) = 0 OR storage_driver = ANY(@drivers::text[]))
  AND (
      cardinality(@categories::text[]) = 0
      OR ('image' = ANY(@categories::text[]) AND mime_type LIKE 'image/%')
      OR ('document' = ANY(@categories::text[]) AND mime_type NOT LIKE 'image/%')
  )
ORDER BY
    CASE WHEN @sort_by::text = 'created_at' AND @sort_order::text = 'asc' THEN created_at END ASC,
    CASE WHEN @sort_by::text = 'created_at' AND @sort_order::text = 'desc' THEN created_at END DESC,
    CASE WHEN @sort_by::text = 'original_name' AND @sort_order::text = 'asc' THEN original_name END ASC,
    CASE WHEN @sort_by::text = 'original_name' AND @sort_order::text = 'desc' THEN original_name END DESC,
    CASE WHEN @sort_by::text = 'size_bytes' AND @sort_order::text = 'asc' THEN size_bytes END ASC,
    CASE WHEN @sort_by::text = 'size_bytes' AND @sort_order::text = 'desc' THEN size_bytes END DESC,
    created_at DESC,
    CASE WHEN @sort_order::text = 'asc' THEN id END ASC,
    id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: ListCleanupCandidates :many
SELECT id, uploader_id, storage_driver, object_key, original_name, mime_type, size_bytes, sha256, status, created_at, updated_at
FROM files
WHERE storage_driver = ANY(@drivers::text[])
  AND (
      status IN ('failed', 'deleting')
      OR (status = 'pending' AND updated_at < @pending_cutoff::timestamptz)
  )
ORDER BY updated_at ASC, id ASC
LIMIT @batch_limit;
