-- name: UpsertPermission :exec
INSERT INTO permissions (code, module, name_key, description_key, updated_at)
VALUES ($1, $2, $3, $4, now())
ON CONFLICT (code) DO UPDATE
SET module = EXCLUDED.module,
    name_key = EXCLUDED.name_key,
    description_key = EXCLUDED.description_key,
    updated_at = now();

-- name: EnsureSystemRole :one
INSERT INTO roles (id, code, name, description, name_key, description_key, is_system, created_at, updated_at)
VALUES ($1, $2, '', '', $3, $4, true, now(), now())
ON CONFLICT (code) DO UPDATE
SET name_key = EXCLUDED.name_key,
    description_key = EXCLUDED.description_key,
    is_system = true,
    updated_at = now()
RETURNING id;

-- name: GrantRolePermission :exec
INSERT INTO role_permissions (role_id, permission_code)
VALUES ($1, $2)
ON CONFLICT (role_id, permission_code) DO NOTHING;

-- name: GetRoleIDByCode :one
SELECT id
FROM roles
WHERE code = $1;

-- name: AssignUserRole :exec
INSERT INTO user_roles (user_id, role_id)
VALUES ($1, $2)
ON CONFLICT (user_id, role_id) DO NOTHING;

-- name: ListUserRoleCodes :many
SELECT r.code
FROM roles r
INNER JOIN user_roles ur ON ur.role_id = r.id
WHERE ur.user_id = $1
ORDER BY r.code ASC;

-- name: ListUserPermissions :many
SELECT DISTINCT rp.permission_code
FROM role_permissions rp
INNER JOIN user_roles ur ON ur.role_id = rp.role_id
WHERE ur.user_id = $1
ORDER BY rp.permission_code ASC;

-- name: ListRoleUserIDs :many
SELECT user_id
FROM user_roles
WHERE role_id = $1;

-- name: CountRoles :one
SELECT count(*)::bigint
FROM roles r
WHERE (@q::text = '' OR (
        r.code ILIKE '%' || @q::text || '%' ESCAPE '\' OR
        r.name ILIKE '%' || @q::text || '%' ESCAPE '\' OR
        r.description ILIKE '%' || @q::text || '%' ESCAPE '\' OR
        r.code = ANY(@matched_system_codes::text[])
    ))
  AND (@has_is_system::boolean = false OR r.is_system = @is_system::boolean);

-- name: ListRoles :many
SELECT
    r.id,
    r.code,
    r.name,
    r.description,
    r.name_key,
    r.description_key,
    r.is_system,
    r.created_by,
    r.updated_by,
    r.created_at,
    r.updated_at,
    (SELECT count(*)::bigint FROM user_roles ur WHERE ur.role_id = r.id) AS user_count,
    (SELECT count(*)::bigint FROM role_permissions rp WHERE rp.role_id = r.id) AS permission_count
FROM roles r
WHERE (@q::text = '' OR (
        r.code ILIKE '%' || @q::text || '%' ESCAPE '\' OR
        r.name ILIKE '%' || @q::text || '%' ESCAPE '\' OR
        r.description ILIKE '%' || @q::text || '%' ESCAPE '\' OR
        r.code = ANY(@matched_system_codes::text[])
    ))
  AND (@has_is_system::boolean = false OR r.is_system = @is_system::boolean)
ORDER BY
    CASE WHEN @sort_by::text = 'created_at' AND @sort_order::text = 'asc' THEN r.created_at END ASC,
    CASE WHEN @sort_by::text = 'created_at' AND @sort_order::text = 'desc' THEN r.created_at END DESC,
    CASE WHEN @sort_by::text = 'updated_at' AND @sort_order::text = 'asc' THEN r.updated_at END ASC,
    CASE WHEN @sort_by::text = 'updated_at' AND @sort_order::text = 'desc' THEN r.updated_at END DESC,
    CASE WHEN @sort_by::text = 'code' AND @sort_order::text = 'asc' THEN r.code END ASC,
    CASE WHEN @sort_by::text = 'code' AND @sort_order::text = 'desc' THEN r.code END DESC,
    CASE WHEN @sort_by::text = 'name' AND @sort_order::text = 'asc' THEN r.name END ASC,
    CASE WHEN @sort_by::text = 'name' AND @sort_order::text = 'desc' THEN r.name END DESC,
    CASE WHEN @sort_by::text = '' THEN r.is_system END DESC,
    r.created_at DESC,
    CASE WHEN @sort_order::text = 'asc' THEN r.id END ASC,
    r.id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: GetRoleByID :one
SELECT
    r.id,
    r.code,
    r.name,
    r.description,
    r.name_key,
    r.description_key,
    r.is_system,
    r.created_by,
    r.updated_by,
    r.created_at,
    r.updated_at,
    (SELECT count(*)::bigint FROM user_roles ur WHERE ur.role_id = r.id) AS user_count,
    (SELECT count(*)::bigint FROM role_permissions rp WHERE rp.role_id = r.id) AS permission_count
FROM roles r
WHERE r.id = $1;

-- name: GetRoleByIDForUpdate :one
SELECT
    id,
    code,
    name,
    description,
    name_key,
    description_key,
    is_system,
    created_by,
    updated_by,
    created_at,
    updated_at
FROM roles
WHERE id = $1
FOR UPDATE;

-- name: ListPermissionCodesByRoleIDs :many
SELECT role_id, permission_code
FROM role_permissions
WHERE role_id = ANY(@role_ids::uuid[])
ORDER BY role_id ASC, permission_code ASC;

-- name: CreateRole :one
INSERT INTO roles (
    id,
    code,
    name,
    description,
    name_key,
    description_key,
    is_system,
    created_by,
    updated_by,
    created_at,
    updated_at
)
VALUES ($1, $2, $3, $4, NULL, NULL, false, $5, $6, now(), now())
RETURNING
    id,
    code,
    name,
    description,
    name_key,
    description_key,
    is_system,
    created_by,
    updated_by,
    created_at,
    updated_at;

-- name: UpdateRole :one
UPDATE roles
SET name = $2,
    description = $3,
    updated_by = $4,
    updated_at = now()
WHERE id = $1
RETURNING
    id,
    code,
    name,
    description,
    name_key,
    description_key,
    is_system,
    created_by,
    updated_by,
    created_at,
    updated_at;

-- name: DeleteRolePermissions :exec
DELETE FROM role_permissions
WHERE role_id = $1;

-- name: CountRoleUsers :one
SELECT count(*)::bigint
FROM user_roles
WHERE role_id = $1;

-- name: DeleteRole :exec
DELETE FROM roles
WHERE id = $1;

-- name: ListRoleOptions :many
SELECT id, code, name, name_key, is_system
FROM roles
ORDER BY is_system DESC, code ASC;

-- name: ListRolesByIDs :many
SELECT id, code, name, name_key, is_system
FROM roles
WHERE id = ANY(@ids::uuid[]);

-- name: ListRolesByUserIDs :many
SELECT ur.user_id, r.id, r.code, r.name, r.name_key, r.is_system
FROM user_roles ur
INNER JOIN roles r ON r.id = ur.role_id
WHERE ur.user_id = ANY(@user_ids::uuid[])
ORDER BY ur.user_id ASC, r.is_system DESC, r.code ASC;

-- name: DeleteUserRoles :exec
DELETE FROM user_roles
WHERE user_id = $1;

-- name: ListUserIDsByRoleIDs :many
SELECT DISTINCT user_id
FROM user_roles
WHERE role_id = ANY(@role_ids::uuid[]);

-- name: ListUserIDsByRoleCode :many
SELECT ur.user_id
FROM user_roles ur
INNER JOIN roles r ON r.id = ur.role_id
WHERE r.code = $1;

-- name: EnsureSystemMenu :one
INSERT INTO menus (
    id,
    parent_id,
    type,
    code,
    title,
    title_key,
    path,
    icon,
    permission_code,
    sort_order,
    visible,
    status,
    is_system,
    created_at,
    updated_at
)
VALUES ($1, $2, $3, $4, '', $5, $6, $7, $8, $9, true, 'active', true, now(), now())
ON CONFLICT (code) DO UPDATE
SET title_key = EXCLUDED.title_key,
    is_system = true,
    updated_at = now()
RETURNING id;

-- name: ListMenus :many
SELECT
    id,
    parent_id,
    type,
    code,
    title,
    title_key,
    path,
    icon,
    permission_code,
    sort_order,
    visible,
    status,
    is_system,
    created_by,
    updated_by,
    created_at,
    updated_at
FROM menus
ORDER BY sort_order ASC, created_at ASC, id ASC;

-- name: GetMenuByID :one
SELECT
    id,
    parent_id,
    type,
    code,
    title,
    title_key,
    path,
    icon,
    permission_code,
    sort_order,
    visible,
    status,
    is_system,
    created_by,
    updated_by,
    created_at,
    updated_at
FROM menus
WHERE id = $1;

-- name: GetMenuByIDForUpdate :one
SELECT
    id,
    parent_id,
    type,
    code,
    title,
    title_key,
    path,
    icon,
    permission_code,
    sort_order,
    visible,
    status,
    is_system,
    created_by,
    updated_by,
    created_at,
    updated_at
FROM menus
WHERE id = $1
FOR UPDATE;

-- name: CreateMenu :one
INSERT INTO menus (
    id,
    parent_id,
    type,
    code,
    title,
    title_key,
    path,
    icon,
    permission_code,
    sort_order,
    visible,
    status,
    is_system,
    created_by,
    updated_by,
    created_at,
    updated_at
)
VALUES ($1, $2, $3, $4, $5, NULL, $6, $7, $8, $9, $10, $11, false, $12, $13, now(), now())
RETURNING
    id,
    parent_id,
    type,
    code,
    title,
    title_key,
    path,
    icon,
    permission_code,
    sort_order,
    visible,
    status,
    is_system,
    created_by,
    updated_by,
    created_at,
    updated_at;

-- name: UpdateMenu :one
UPDATE menus
SET parent_id = $2,
    type = $3,
    title = $4,
    path = $5,
    icon = $6,
    permission_code = $7,
    sort_order = $8,
    visible = $9,
    status = $10,
    updated_by = $11,
    updated_at = now()
WHERE id = $1
RETURNING
    id,
    parent_id,
    type,
    code,
    title,
    title_key,
    path,
    icon,
    permission_code,
    sort_order,
    visible,
    status,
    is_system,
    created_by,
    updated_by,
    created_at,
    updated_at;

-- name: UpdateMenuOrder :exec
UPDATE menus
SET parent_id = $2,
    sort_order = $3,
    updated_by = $4,
    updated_at = now()
WHERE id = $1;

-- name: CountMenuChildren :one
SELECT count(*)::bigint
FROM menus
WHERE parent_id = $1;

-- name: DeleteMenu :exec
DELETE FROM menus
WHERE id = $1;
