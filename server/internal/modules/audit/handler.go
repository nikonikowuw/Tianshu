package audit

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Handler exposes read-only HTTP endpoints for querying audit logs.
type Handler struct {
	service *Service
}

// NewHandler creates an audit Handler backed by service.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) list(c *gin.Context) {
	filter, err := bindListAuditLogsRequest(c)
	if err != nil {
		_ = c.Error(err)
		return
	}

	result, err := h.service.List(c.Request.Context(), filter)
	if err != nil {
		_ = c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toListAuditLogsResponse(result))
}
