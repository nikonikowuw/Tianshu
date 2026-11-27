package identity

import (
	"time"

	"github.com/gin-gonic/gin"

	"tianshu/internal/platform/httpserver"
	"tianshu/internal/shared/authctx"
)

const passwordChangeRateLimitPerMinute = 5

// RoutePermissions bundles per-action permission middleware for the identity HTTP routes.
type RoutePermissions struct {
	RequireRead   gin.HandlerFunc
	RequireCreate gin.HandlerFunc
	RequireUpdate gin.HandlerFunc
	RequireDelete gin.HandlerFunc
}

// RegisterRoutes mounts user management and self-service profile endpoints under an authenticated route group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, perms RoutePermissions) {
	rg.GET("/users", perms.RequireRead, h.listUsers)
	rg.GET("/users/:id", perms.RequireRead, h.getUser)
	rg.POST("/users", perms.RequireCreate, h.createUser)
	rg.PATCH("/users/:id", perms.RequireUpdate, h.updateUser)
	rg.DELETE("/users/:id", perms.RequireDelete, h.deleteUser)

	passwordRateLimit := httpserver.RateLimitMiddleware(httpserver.RateLimitConfig{
		Limit:  passwordChangeRateLimitPerMinute,
		Window: time.Minute,
		KeyFunc: func(c *gin.Context) string {
			if id, ok := authctx.IdentityFromContext(c.Request.Context()); ok {
				return "profile-pwd:" + id.UserID.String()
			}
			return "profile-pwd-ip:" + c.ClientIP()
		},
	})

	profile := rg.Group("/profile")
	profile.GET("", h.getProfile)
	profile.PATCH("", h.updateProfile)
	profile.PUT("/password", passwordRateLimit, h.changePassword)
	profile.GET("/sessions", h.listProfileSessions)
	profile.DELETE("/sessions", h.revokeOtherProfileSessions)
	profile.DELETE("/sessions/:id", h.revokeProfileSession)
}
