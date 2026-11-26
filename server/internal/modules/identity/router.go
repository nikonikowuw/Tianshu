package identity

import "github.com/gin-gonic/gin"

// RoutePermissions bundles per-action permission middleware for the identity HTTP routes.
type RoutePermissions struct {
	RequireRead   gin.HandlerFunc
	RequireCreate gin.HandlerFunc
	RequireUpdate gin.HandlerFunc
	RequireDelete gin.HandlerFunc
}

// RegisterRoutes mounts user management endpoints under an authenticated route group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, perms RoutePermissions) {
	rg.GET("/users", perms.RequireRead, h.listUsers)
	rg.GET("/users/:id", perms.RequireRead, h.getUser)
	rg.POST("/users", perms.RequireCreate, h.createUser)
	rg.PATCH("/users/:id", perms.RequireUpdate, h.updateUser)
	rg.DELETE("/users/:id", perms.RequireDelete, h.deleteUser)
}
