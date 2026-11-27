-- +goose Up
-- Extend files.status to support compensating delete cleanup and add query/reference indexes.

ALTER TABLE files DROP CONSTRAINT IF EXISTS files_status_check;
ALTER TABLE files DROP CONSTRAINT IF EXISTS ck_files_status;
ALTER TABLE files
    ADD CONSTRAINT ck_files_status
    CHECK (status IN ('pending', 'ready', 'failed', 'deleting'));

CREATE INDEX IF NOT EXISTS idx_files_status_created_at
    ON files (status, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_users_avatar_file_id
    ON users (avatar_file_id)
    WHERE avatar_file_id IS NOT NULL;

-- +goose Down
-- Preserve rows while making the older constraint satisfiable; the janitor will
-- continue cleaning them up as failed rows after rollback.
UPDATE files SET status = 'failed', updated_at = now() WHERE status = 'deleting';

DROP INDEX IF EXISTS idx_users_avatar_file_id;
DROP INDEX IF EXISTS idx_files_status_created_at;
ALTER TABLE files DROP CONSTRAINT IF EXISTS ck_files_status;
ALTER TABLE files
    ADD CONSTRAINT files_status_check
    CHECK (status IN ('pending', 'ready', 'failed'));
