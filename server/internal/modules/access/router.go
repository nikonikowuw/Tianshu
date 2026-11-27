package access

import "github.com/gin-gonic/gin"

// RoutePermissions bundles per-action permission middleware for the access HTTP routes.
type RoutePermissions struct {
	RequireRead       gin.HandlerFunc
	RequireOptions    gin.HandlerFunc
	RequireCreate     gin.HandlerFunc
	RequireUpdate     gin.HandlerFunc
	RequireDelete     gin.HandlerFunc
	RequireMenuRead   gin.HandlerFunc
	RequireMenuCreate gin.HandlerFunc
	RequireMenuUpdate gin.HandlerFunc
	RequireMenuDelete gin.HandlerFunc
}

// RegisterRoutes mounts permission catalog, role CRUD, and menu tree endpoints under an authenticated route group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, perms RoutePermissions) {
	rg.GET("/permissions", perms.RequireRead, h.listPermissions)
	rg.GET("/roles", perms.RequireRead, h.listRoles)
	rg.GET("/roles/options", perms.RequireOptions, h.listRoleOptions)
	rg.GET("/roles/:id", perms.RequireRead, h.getRole)
	rg.POST("/roles", perms.RequireCreate, h.createRole)
	rg.PATCH("/roles/:id", perms.RequireUpdate, h.updateRole)
	rg.DELETE("/roles/:id", perms.RequireDelete, h.deleteRole)

	rg.GET("/menus/user", h.listUserMenus)
	rg.GET("/menus/tree", perms.RequireMenuRead, h.listMenuTree)
	rg.POST("/menus", perms.RequireMenuCreate, h.createMenu)
	rg.PUT("/menus/reorder", perms.RequireMenuUpdate, h.reorderMenus)
	rg.PATCH("/menus/:id", perms.RequireMenuUpdate, h.updateMenu)
	rg.DELETE("/menus/:id", perms.RequireMenuDelete, h.deleteMenu)
}
