-- +goose Up
-- Core schema baseline for Tianshu (PostgreSQL 17, UUIDv7 primary keys).

CREATE TABLE IF NOT EXISTS users (
    id              uuid        PRIMARY KEY,
    username        text        NOT NULL,
    email           text        NOT NULL,
    password_hash   text        NOT NULL,
    display_name    text        NOT NULL DEFAULT '',
    avatar_file_id  uuid        NULL,
    status          text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    last_login_at   timestamptz NULL,
    deleted_at      timestamptz NULL,
    created_by      uuid        NULL REFERENCES users(id) ON DELETE SET NULL,
    updated_by      uuid        NULL REFERENCES users(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_users_username_lower
    ON users (lower(username))
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_users_email_lower
    ON users (lower(email))
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_users_status ON users (status);
CREATE INDEX IF NOT EXISTS idx_users_created_at ON users (created_at DESC);

CREATE TABLE IF NOT EXISTS files (
    id              uuid        PRIMARY KEY,
    uploader_id     uuid        NULL REFERENCES users(id) ON DELETE SET NULL,
    storage_driver  text        NOT NULL,
    object_key      text        NOT NULL UNIQUE,
    original_name   text        NOT NULL,
    mime_type       text        NOT NULL,
    size_bytes      bigint      NOT NULL CHECK (size_bytes >= 0),
    sha256          text        NOT NULL,
    status          text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'ready', 'failed')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_files_uploader_id_created_at ON files (uploader_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_files_sha256 ON files (sha256);
CREATE INDEX IF NOT EXISTS idx_files_status ON files (status);

ALTER TABLE users
    ADD CONSTRAINT fk_users_avatar_file_id
    FOREIGN KEY (avatar_file_id) REFERENCES files(id) ON DELETE SET NULL;

CREATE TABLE IF NOT EXISTS roles (
    id              uuid        PRIMARY KEY,
    code            text        NOT NULL UNIQUE,
    name            text        NOT NULL DEFAULT '',
    description     text        NOT NULL DEFAULT '',
    name_key        text        NULL,
    description_key text        NULL,
    is_system       boolean     NOT NULL DEFAULT false,
    created_by      uuid        NULL REFERENCES users(id) ON DELETE SET NULL,
    updated_by      uuid        NULL REFERENCES users(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_roles_is_system ON roles (is_system);

CREATE TABLE IF NOT EXISTS permissions (
    code            text        PRIMARY KEY,
    module          text        NOT NULL,
    name_key        text        NOT NULL,
    description_key text        NOT NULL,
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS user_roles (
    user_id         uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id         uuid        NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, role_id)
);

CREATE INDEX IF NOT EXISTS idx_user_roles_role_id ON user_roles (role_id);

CREATE TABLE IF NOT EXISTS role_permissions (
    role_id         uuid        NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_code text        NOT NULL REFERENCES permissions(code) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_code)
);

CREATE INDEX IF NOT EXISTS idx_role_permissions_permission_code ON role_permissions (permission_code);

CREATE TABLE IF NOT EXISTS user_sessions (
    id                 uuid        PRIMARY KEY,
    user_id            uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    refresh_token_hash text        NOT NULL UNIQUE,
    user_agent         text        NOT NULL DEFAULT '',
    ip_address         text        NOT NULL DEFAULT '',
    expires_at         timestamptz NOT NULL,
    revoked_at         timestamptz NULL,
    last_used_at       timestamptz NOT NULL DEFAULT now(),
    created_at         timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_user_sessions_user_id ON user_sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_user_sessions_expires_at ON user_sessions (expires_at);
CREATE INDEX IF NOT EXISTS idx_user_sessions_revoked_at ON user_sessions (revoked_at);

CREATE TABLE IF NOT EXISTS refresh_token_history (
    token_hash  text        PRIMARY KEY,
    session_id  uuid        NOT NULL REFERENCES user_sessions(id) ON DELETE CASCADE,
    replaced_at timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_refresh_token_history_session_id ON refresh_token_history (session_id);
CREATE INDEX IF NOT EXISTS idx_refresh_token_history_expires_at ON refresh_token_history (expires_at);

CREATE TABLE IF NOT EXISTS audit_logs (
    id             uuid        PRIMARY KEY,
    actor_id       uuid        NULL REFERENCES users(id) ON DELETE SET NULL,
    actor_username text        NOT NULL DEFAULT '',
    action         text        NOT NULL,
    resource       text        NOT NULL,
    resource_id    text        NOT NULL DEFAULT '',
    status         text        NOT NULL CHECK (status IN ('success', 'failure')),
    ip_address     text        NOT NULL DEFAULT '',
    user_agent     text        NOT NULL DEFAULT '',
    request_id     text        NOT NULL DEFAULT '',
    metadata       jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_actor_id_created_at ON audit_logs (actor_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_action_created_at ON audit_logs (action, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_resource ON audit_logs (resource, resource_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_request_id ON audit_logs (request_id);

-- +goose Down
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS refresh_token_history;
DROP TABLE IF EXISTS user_sessions;
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS user_roles;
DROP TABLE IF EXISTS permissions;
DROP TABLE IF EXISTS roles;
ALTER TABLE IF EXISTS users DROP CONSTRAINT IF EXISTS fk_users_avatar_file_id;
DROP TABLE IF EXISTS files;
DROP TABLE IF EXISTS users;
