package access

import "github.com/gin-gonic/gin"

// RoutePermissions bundles per-action permission middleware for the access HTTP routes.
type RoutePermissions struct {
	RequireRead    gin.HandlerFunc
	RequireOptions gin.HandlerFunc
	RequireCreate  gin.HandlerFunc
	RequireUpdate  gin.HandlerFunc
	RequireDelete  gin.HandlerFunc
}

// RegisterRoutes mounts permission catalog and role CRUD endpoints under an authenticated route group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, perms RoutePermissions) {
	rg.GET("/permissions", perms.RequireRead, h.listPermissions)
	rg.GET("/roles", perms.RequireRead, h.listRoles)
	if perms.RequireOptions != nil {
		rg.GET("/roles/options", perms.RequireOptions, h.listRoleOptions)
	}
	rg.GET("/roles/:id", perms.RequireRead, h.getRole)
	rg.POST("/roles", perms.RequireCreate, h.createRole)
	rg.PATCH("/roles/:id", perms.RequireUpdate, h.updateRole)
	rg.DELETE("/roles/:id", perms.RequireDelete, h.deleteRole)
}
