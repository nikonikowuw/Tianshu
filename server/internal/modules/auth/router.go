package auth

import (
	"time"

	"github.com/gin-gonic/gin"

	"tianshu/internal/platform/httpserver"
)

// RegisterPublicRoutes mounts public login, refresh, and logout endpoints under rg.
func (h *Handler) RegisterPublicRoutes(rg *gin.RouterGroup) {
	g := rg.Group("/auth")
	g.POST("/login",
		httpserver.RateLimitMiddleware(httpserver.RateLimitConfig{
			Limit:  5,
			Window: time.Minute,
		}),
		h.login,
	)
	g.POST("/refresh",
		RequireSameOrigin(h.allowedOrigins),
		httpserver.RateLimitMiddleware(httpserver.RateLimitConfig{
			Limit:  30,
			Window: time.Minute,
		}),
		h.refresh,
	)
	g.POST("/logout",
		RequireSameOrigin(h.allowedOrigins),
		h.logout,
	)
}

// RegisterRoutes mounts authenticated auth endpoints under an already-authenticated group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/auth/me", h.me)
}
