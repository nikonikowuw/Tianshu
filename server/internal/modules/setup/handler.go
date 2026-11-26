package setup

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Handler exposes HTTP endpoints for system initialization.
type Handler struct {
	service *Service
}

// NewHandler creates a setup Handler backed by service.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) status(c *gin.Context) {
	st, err := h.service.Status(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toStatusResponse(st))
}

func (h *Handler) init(c *gin.Context) {
	var req initRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(err)
		return
	}

	admin, err := h.service.Init(c.Request.Context(), InitInput{
		SetupToken:  req.SetupToken,
		Username:    req.Username,
		Email:       req.Email,
		DisplayName: req.DisplayName,
		Password:    req.Password,
		ClientIP:    c.ClientIP(),
	})
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, toInitResponse(admin))
}
