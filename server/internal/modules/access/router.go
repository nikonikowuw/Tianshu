package access

import (
	"github.com/gin-gonic/gin"

	"tianshu/internal/shared/apperr"
)

// RoutePermissions bundles per-action permission middleware for the access HTTP routes.
type RoutePermissions struct {
	RequireRead       gin.HandlerFunc // roles:read
	RequireOptions    gin.HandlerFunc // users:read || roles:read
	RequireCreate     gin.HandlerFunc // roles:create
	RequireUpdate     gin.HandlerFunc // roles:update
	RequireDelete     gin.HandlerFunc // roles:delete
	RequireMenuRead   gin.HandlerFunc // menus:read || roles:read
	RequireMenuCreate gin.HandlerFunc // menus:create
	RequireMenuUpdate gin.HandlerFunc // menus:update
	RequireMenuDelete gin.HandlerFunc // menus:delete
}

// RegisterRoutes mounts permission catalog, role CRUD, and menu tree endpoints under an authenticated route group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, perms RoutePermissions) {
	rg.GET("/permissions", ensureGuard(perms.RequireRead), h.listPermissions)
	rg.GET("/roles", ensureGuard(perms.RequireRead), h.listRoles)
	rg.GET("/roles/options", ensureGuard(perms.RequireOptions), h.listRoleOptions)
	rg.GET("/roles/:id", ensureGuard(perms.RequireRead), h.getRole)
	rg.POST("/roles", ensureGuard(perms.RequireCreate), h.createRole)
	rg.PATCH("/roles/:id", ensureGuard(perms.RequireUpdate), h.updateRole)
	rg.DELETE("/roles/:id", ensureGuard(perms.RequireDelete), h.deleteRole)

	rg.GET("/menus/user", h.listUserMenus)
	rg.GET("/menus/tree", ensureGuard(perms.RequireMenuRead), h.listMenuTree)
	rg.POST("/menus", ensureGuard(perms.RequireMenuCreate), h.createMenu)
	rg.PUT("/menus/reorder", ensureGuard(perms.RequireMenuUpdate), h.reorderMenus)
	rg.PATCH("/menus/:id", ensureGuard(perms.RequireMenuUpdate), h.updateMenu)
	rg.DELETE("/menus/:id", ensureGuard(perms.RequireMenuDelete), h.deleteMenu)
}

func ensureGuard(fn gin.HandlerFunc) gin.HandlerFunc {
	if fn != nil {
		return fn
	}
	return func(c *gin.Context) {
		_ = c.Error(apperr.Forbidden(apperr.CodeForbidden))
		c.Abort()
	}
}
