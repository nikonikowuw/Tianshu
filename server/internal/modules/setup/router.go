package setup

import (
	"time"

	"github.com/gin-gonic/gin"

	"tianshu/internal/platform/httpserver"
)

// RegisterPublicRoutes mounts public setup endpoints under the provided API route group.
func (h *Handler) RegisterPublicRoutes(rg *gin.RouterGroup) {
	g := rg.Group("/setup")
	g.GET("/status", h.status)
	g.POST("/init",
		httpserver.RateLimitMiddleware(httpserver.RateLimitConfig{
			Limit:  5,
			Window: time.Minute,
		}),
		h.init,
	)
}
