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

	PermMenusRead   = "menus:read"
	PermMenusCreate = "menus:create"
	PermMenusUpdate = "menus:update"
	PermMenusDelete = "menus:delete"

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
		Code:           PermMenusRead,
		Module:         "access",
		NameKey:        "ACCESS_PERM_MENUS_READ_NAME",
		DescriptionKey: "ACCESS_PERM_MENUS_READ_DESC",
	},
	{
		Code:           PermMenusCreate,
		Module:         "access",
		NameKey:        "ACCESS_PERM_MENUS_CREATE_NAME",
		DescriptionKey: "ACCESS_PERM_MENUS_CREATE_DESC",
	},
	{
		Code:           PermMenusUpdate,
		Module:         "access",
		NameKey:        "ACCESS_PERM_MENUS_UPDATE_NAME",
		DescriptionKey: "ACCESS_PERM_MENUS_UPDATE_DESC",
	},
	{
		Code:           PermMenusDelete,
		Module:         "access",
		NameKey:        "ACCESS_PERM_MENUS_DELETE_NAME",
		DescriptionKey: "ACCESS_PERM_MENUS_DELETE_DESC",
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

var moduleLabelKeys = map[string]string{
	"dashboard": "ACCESS_MODULE_DASHBOARD",
	"identity":  "ACCESS_MODULE_IDENTITY",
	"access":    "ACCESS_MODULE_ACCESS",
	"audit":     "ACCESS_MODULE_AUDIT",
	"files":     "ACCESS_MODULE_FILES",
}

var permissionCodeSet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(permissionCatalog))
	for _, p := range permissionCatalog {
		set[p.Code] = struct{}{}
	}
	return set
}()

// IsValidPermissionCode reports whether code exists in the declarative permission catalog.
func IsValidPermissionCode(code string) bool {
	_, ok := permissionCodeSet[code]
	return ok
}

// PermissionGroups returns the declarative permission catalog grouped by module in declaration order.
func PermissionGroups() []PermissionGroupDef {
	indexByModule := make(map[string]int, len(moduleLabelKeys))
	groups := make([]PermissionGroupDef, 0, len(moduleLabelKeys))
	for _, p := range permissionCatalog {
		idx, exists := indexByModule[p.Module]
		if !exists {
			labelKey := moduleLabelKeys[p.Module]
			if labelKey == "" {
				labelKey = p.Module
			}
			indexByModule[p.Module] = len(groups)
			groups = append(groups, PermissionGroupDef{
				Module:      p.Module,
				LabelKey:    labelKey,
				Permissions: []PermissionDef{p},
			})
			continue
		}
		groups[idx].Permissions = append(groups[idx].Permissions, p)
	}
	return groups
}

// SystemRoles returns a defensive copy of all built-in system roles.
func SystemRoles() []SystemRoleDef {
	return slices.Clone(systemRoleCatalog)
}

var systemMenuCatalog = []SystemMenuDef{
	{
		Code:      "dir_general",
		Type:      MenuTypeDirectory,
		TitleKey:  "ACCESS_MENU_DIR_GENERAL",
		SortOrder: 10,
	},
	{
		ParentCode:     "dir_general",
		Code:           "menu_dashboard",
		Type:           MenuTypeMenu,
		TitleKey:       "ACCESS_MENU_DASHBOARD",
		Path:           "/",
		Icon:           "LayoutDashboard",
		PermissionCode: PermDashboardRead,
		SortOrder:      10,
	},
	{
		Code:      "dir_system",
		Type:      MenuTypeDirectory,
		TitleKey:  "ACCESS_MENU_DIR_SYSTEM",
		SortOrder: 20,
	},
	{
		ParentCode:     "dir_system",
		Code:           "menu_users",
		Type:           MenuTypeMenu,
		TitleKey:       "ACCESS_MENU_USERS",
		Path:           "/users",
		Icon:           "Users",
		PermissionCode: PermUsersRead,
		SortOrder:      10,
	},
	{
		ParentCode:     "menu_users",
		Code:           "btn_users_create",
		Type:           MenuTypeButton,
		TitleKey:       "ACCESS_PERM_USERS_CREATE_NAME",
		PermissionCode: PermUsersCreate,
		SortOrder:      10,
	},
	{
		ParentCode:     "menu_users",
		Code:           "btn_users_update",
		Type:           MenuTypeButton,
		TitleKey:       "ACCESS_PERM_USERS_UPDATE_NAME",
		PermissionCode: PermUsersUpdate,
		SortOrder:      20,
	},
	{
		ParentCode:     "menu_users",
		Code:           "btn_users_delete",
		Type:           MenuTypeButton,
		TitleKey:       "ACCESS_PERM_USERS_DELETE_NAME",
		PermissionCode: PermUsersDelete,
		SortOrder:      30,
	},
	{
		ParentCode:     "dir_system",
		Code:           "menu_roles",
		Type:           MenuTypeMenu,
		TitleKey:       "ACCESS_MENU_ROLES",
		Path:           "/roles",
		Icon:           "Shield",
		PermissionCode: PermRolesRead,
		SortOrder:      20,
	},
	{
		ParentCode:     "menu_roles",
		Code:           "btn_roles_create",
		Type:           MenuTypeButton,
		TitleKey:       "ACCESS_PERM_ROLES_CREATE_NAME",
		PermissionCode: PermRolesCreate,
		SortOrder:      10,
	},
	{
		ParentCode:     "menu_roles",
		Code:           "btn_roles_update",
		Type:           MenuTypeButton,
		TitleKey:       "ACCESS_PERM_ROLES_UPDATE_NAME",
		PermissionCode: PermRolesUpdate,
		SortOrder:      20,
	},
	{
		ParentCode:     "menu_roles",
		Code:           "btn_roles_delete",
		Type:           MenuTypeButton,
		TitleKey:       "ACCESS_PERM_ROLES_DELETE_NAME",
		PermissionCode: PermRolesDelete,
		SortOrder:      30,
	},
	{
		ParentCode:     "dir_system",
		Code:           "menu_menus",
		Type:           MenuTypeMenu,
		TitleKey:       "ACCESS_MENU_MENUS",
		Path:           "/menus",
		Icon:           "ListTree",
		PermissionCode: PermMenusRead,
		SortOrder:      30,
	},
	{
		ParentCode:     "menu_menus",
		Code:           "btn_menus_create",
		Type:           MenuTypeButton,
		TitleKey:       "ACCESS_PERM_MENUS_CREATE_NAME",
		PermissionCode: PermMenusCreate,
		SortOrder:      10,
	},
	{
		ParentCode:     "menu_menus",
		Code:           "btn_menus_update",
		Type:           MenuTypeButton,
		TitleKey:       "ACCESS_PERM_MENUS_UPDATE_NAME",
		PermissionCode: PermMenusUpdate,
		SortOrder:      20,
	},
	{
		ParentCode:     "menu_menus",
		Code:           "btn_menus_delete",
		Type:           MenuTypeButton,
		TitleKey:       "ACCESS_PERM_MENUS_DELETE_NAME",
		PermissionCode: PermMenusDelete,
		SortOrder:      30,
	},
	{
		ParentCode:     "dir_system",
		Code:           "menu_audit_logs",
		Type:           MenuTypeMenu,
		TitleKey:       "ACCESS_MENU_AUDIT_LOGS",
		Path:           "/audit-logs",
		Icon:           "ScrollText",
		PermissionCode: PermAuditRead,
		SortOrder:      40,
	},
}

// SystemMenus returns a defensive copy of the built-in navigation menu tree.
func SystemMenus() []SystemMenuDef {
	return slices.Clone(systemMenuCatalog)
}
