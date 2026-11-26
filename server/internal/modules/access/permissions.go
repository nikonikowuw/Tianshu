package access

import "slices"

// System role codes built into Tianshu.
const (
	RoleCodeSuperAdmin = "super_admin"
)

// Declarative permission codes synchronized to the permissions table at startup.
const (
	PermDashboardRead = "dashboard:read"

	PermUsersRead   = "users:read"
	PermUsersCreate = "users:create"
	PermUsersUpdate = "users:update"
	PermUsersDelete = "users:delete"

	PermRolesRead   = "roles:read"
	PermRolesCreate = "roles:create"
	PermRolesUpdate = "roles:update"
	PermRolesDelete = "roles:delete"

	PermAuditRead = "audit:read"

	PermFilesRead   = "files:read"
	PermFilesUpload = "files:upload"
	PermFilesDelete = "files:delete"
)

var permissionCatalog = []PermissionDef{
	{
		Code:           PermDashboardRead,
		Module:         "dashboard",
		NameKey:        "ACCESS_PERM_DASHBOARD_READ_NAME",
		DescriptionKey: "ACCESS_PERM_DASHBOARD_READ_DESC",
	},
	{
		Code:           PermUsersRead,
		Module:         "identity",
		NameKey:        "ACCESS_PERM_USERS_READ_NAME",
		DescriptionKey: "ACCESS_PERM_USERS_READ_DESC",
	},
	{
		Code:           PermUsersCreate,
		Module:         "identity",
		NameKey:        "ACCESS_PERM_USERS_CREATE_NAME",
		DescriptionKey: "ACCESS_PERM_USERS_CREATE_DESC",
	},
	{
		Code:           PermUsersUpdate,
		Module:         "identity",
		NameKey:        "ACCESS_PERM_USERS_UPDATE_NAME",
		DescriptionKey: "ACCESS_PERM_USERS_UPDATE_DESC",
	},
	{
		Code:           PermUsersDelete,
		Module:         "identity",
		NameKey:        "ACCESS_PERM_USERS_DELETE_NAME",
		DescriptionKey: "ACCESS_PERM_USERS_DELETE_DESC",
	},
	{
		Code:           PermRolesRead,
		Module:         "access",
		NameKey:        "ACCESS_PERM_ROLES_READ_NAME",
		DescriptionKey: "ACCESS_PERM_ROLES_READ_DESC",
	},
	{
		Code:           PermRolesCreate,
		Module:         "access",
		NameKey:        "ACCESS_PERM_ROLES_CREATE_NAME",
		DescriptionKey: "ACCESS_PERM_ROLES_CREATE_DESC",
	},
	{
		Code:           PermRolesUpdate,
		Module:         "access",
		NameKey:        "ACCESS_PERM_ROLES_UPDATE_NAME",
		DescriptionKey: "ACCESS_PERM_ROLES_UPDATE_DESC",
	},
	{
		Code:           PermRolesDelete,
		Module:         "access",
		NameKey:        "ACCESS_PERM_ROLES_DELETE_NAME",
		DescriptionKey: "ACCESS_PERM_ROLES_DELETE_DESC",
	},
	{
		Code:           PermAuditRead,
		Module:         "audit",
		NameKey:        "ACCESS_PERM_AUDIT_READ_NAME",
		DescriptionKey: "ACCESS_PERM_AUDIT_READ_DESC",
	},
	{
		Code:           PermFilesRead,
		Module:         "files",
		NameKey:        "ACCESS_PERM_FILES_READ_NAME",
		DescriptionKey: "ACCESS_PERM_FILES_READ_DESC",
	},
	{
		Code:           PermFilesUpload,
		Module:         "files",
		NameKey:        "ACCESS_PERM_FILES_UPLOAD_NAME",
		DescriptionKey: "ACCESS_PERM_FILES_UPLOAD_DESC",
	},
	{
		Code:           PermFilesDelete,
		Module:         "files",
		NameKey:        "ACCESS_PERM_FILES_DELETE_NAME",
		DescriptionKey: "ACCESS_PERM_FILES_DELETE_DESC",
	},
}

var systemRoleCatalog = []SystemRoleDef{
	{
		Code:           RoleCodeSuperAdmin,
		NameKey:        "ACCESS_ROLE_SUPER_ADMIN_NAME",
		DescriptionKey: "ACCESS_ROLE_SUPER_ADMIN_DESC",
		AllPermissions: true,
	},
}

// Catalog returns a defensive copy of all declarative permissions.
func Catalog() []PermissionDef {
	return slices.Clone(permissionCatalog)
}

// SystemRoles returns a defensive copy of all built-in system roles.
func SystemRoles() []SystemRoleDef {
	return slices.Clone(systemRoleCatalog)
}
