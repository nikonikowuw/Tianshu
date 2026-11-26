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
