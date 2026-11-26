package audit

import (
	"github.com/gin-gonic/gin"
)

// RegisterRoutes mounts audit log endpoints under an authenticated, permission-protected route group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, requireRead gin.HandlerFunc) {
	rg.GET("/audit-logs", requireRead, h.list)
}
