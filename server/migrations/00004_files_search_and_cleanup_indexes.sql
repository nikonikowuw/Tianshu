-- +goose Up
-- Serialize extension creation: integration tests run migrations in parallel
-- schemas and pg_trgm is database-scoped.
SELECT pg_advisory_xact_lock(hashtext('tianshu:files:pg_trgm'));

-- +goose StatementBegin
DO $$
DECLARE
    trgm_schema text;
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_trgm') THEN
        -- Prefer public so the extension survives test-schema teardown and is
        -- visible to every module schema.
        IF EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = 'public') THEN
            EXECUTE 'CREATE EXTENSION pg_trgm WITH SCHEMA public';
        ELSE
            EXECUTE 'CREATE EXTENSION pg_trgm';
        END IF;
    END IF;

    SELECT n.nspname
      INTO trgm_schema
      FROM pg_extension e
      JOIN pg_namespace n ON n.oid = e.extnamespace
     WHERE e.extname = 'pg_trgm';

    EXECUTE format(
        'CREATE INDEX IF NOT EXISTS idx_files_original_name_trgm ON files USING gin (original_name %I.gin_trgm_ops)',
        trgm_schema
    );
END
$$;
-- +goose StatementEnd

CREATE INDEX IF NOT EXISTS idx_files_cleanup_scan
    ON files (storage_driver, status, updated_at, id)
    WHERE status IN ('pending', 'failed', 'deleting');

-- +goose Down
DROP INDEX IF EXISTS idx_files_cleanup_scan;
DROP INDEX IF EXISTS idx_files_original_name_trgm;
-- pg_trgm is intentionally kept: other deployments/tables may share the extension.
