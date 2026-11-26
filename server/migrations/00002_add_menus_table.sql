-- +goose Up
-- Dynamic menu tree table owned by the access module.

CREATE TABLE IF NOT EXISTS menus (
    id              uuid        PRIMARY KEY,
    parent_id       uuid        NULL REFERENCES menus(id) ON DELETE RESTRICT,
    type            text        NOT NULL CHECK (type IN ('directory', 'menu', 'button')),
    code            text        NOT NULL UNIQUE,
    title           text        NOT NULL DEFAULT '',
    title_key       text        NULL,
    path            text        NOT NULL DEFAULT '',
    icon            text        NOT NULL DEFAULT '',
    permission_code text        NULL REFERENCES permissions(code) ON DELETE SET NULL,
    sort_order      integer     NOT NULL DEFAULT 0,
    visible         boolean     NOT NULL DEFAULT true,
    status          text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    is_system       boolean     NOT NULL DEFAULT false,
    created_by      uuid        NULL REFERENCES users(id) ON DELETE SET NULL,
    updated_by      uuid        NULL REFERENCES users(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_menus_parent_id_sort
    ON menus (parent_id, sort_order ASC, created_at ASC);

CREATE INDEX IF NOT EXISTS idx_menus_permission_code
    ON menus (permission_code);

-- +goose Down
DROP TABLE IF EXISTS menus;
