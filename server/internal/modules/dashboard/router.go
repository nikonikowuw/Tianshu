package dashboard

import "github.com/gin-gonic/gin"

// RegisterRoutes mounts dashboard endpoints under an authenticated, permission-protected route group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, requireRead gin.HandlerFunc) {
	rg.GET("/dashboard/summary", requireRead, h.summary)
}
