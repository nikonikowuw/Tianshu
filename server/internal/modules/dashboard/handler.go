package dashboard

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Handler exposes read-only HTTP endpoints for dashboard overview metrics.
type Handler struct {
	service *Service
}

// NewHandler creates a dashboard Handler backed by service.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) summary(c *gin.Context) {
	var q summaryQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		_ = c.Error(err)
		return
	}
	input, err := q.toInput()
	if err != nil {
		_ = c.Error(err)
		return
	}

	summary, err := h.service.Summary(c.Request.Context(), input)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toSummaryResponse(summary))
}
