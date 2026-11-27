-- +goose Up
-- Replace single-column created_at index with composite (created_at DESC, id DESC)
-- to serve zero-sort default/asc pagination and bounded retention purge scans.
--
-- Production large-table rollout note: to avoid holding a SHARE lock on audit_logs
-- during online upgrades, operators may pre-create the index outside Goose:
--   CREATE INDEX CONCURRENTLY idx_audit_logs_created_at_id
--       ON audit_logs (created_at DESC, id DESC);
-- Verify pg_index.indisvalid = true before running migrations; CREATE INDEX IF NOT EXISTS
-- will then succeed immediately and drop the redundant single-column index.
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at_id
    ON audit_logs (created_at DESC, id DESC);

DROP INDEX IF EXISTS idx_audit_logs_created_at;

-- +goose Down
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at
    ON audit_logs (created_at DESC);

DROP INDEX IF EXISTS idx_audit_logs_created_at_id;
