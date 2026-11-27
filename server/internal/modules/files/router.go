package files

import (
	"time"

	"github.com/gin-gonic/gin"

	"tianshu/internal/platform/httpserver"
	"tianshu/internal/shared/authctx"
)

const uploadRateLimitPerMinute = 20

// RoutePermissions bundles per-action permission middleware for the files HTTP routes.
type RoutePermissions struct {
	RequireRead   gin.HandlerFunc
	RequireUpload gin.HandlerFunc
	RequireDelete gin.HandlerFunc
}

// RegisterPublicRoutes mounts HMAC-signed content delivery endpoints under rg (no Bearer token required).
func (h *Handler) RegisterPublicRoutes(rg *gin.RouterGroup) {
	rg.GET("/files/signed/:token", h.signedContent)
}

// RegisterRoutes mounts authenticated file management endpoints under rg.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, perms RoutePermissions) {
	uploadRateLimit := httpserver.RateLimitMiddleware(httpserver.RateLimitConfig{
		Limit:  uploadRateLimitPerMinute,
		Window: time.Minute,
		KeyFunc: func(c *gin.Context) string {
			if id, ok := authctx.IdentityFromContext(c.Request.Context()); ok {
				return "upload:" + id.UserID.String()
			}
			return "upload-ip:" + c.ClientIP()
		},
	})

	g := rg.Group("/files")
	g.GET("/config", h.uploadPolicy)
	g.GET("", perms.RequireRead, h.list)
	g.POST("", perms.RequireUpload, uploadRateLimit, h.upload)
	g.POST("/avatar", uploadRateLimit, h.uploadAvatar)
	g.GET("/:id", perms.RequireRead, h.get)
	g.GET("/:id/content", perms.RequireRead, h.content)
	g.POST("/:id/signed-url", perms.RequireRead, h.createSignedURL)
	g.DELETE("/:id", perms.RequireDelete, h.deleteFile)
}
