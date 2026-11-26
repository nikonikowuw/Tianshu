package identity

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"tianshu/internal/platform/i18n"
)

// Handler exposes HTTP endpoints for user management.
type Handler struct {
	service *Service
	i18n    *i18n.Bundle
}

// NewHandler creates an identity Handler backed by service and i18n bundle.
func NewHandler(service *Service, bundle *i18n.Bundle) *Handler {
	return &Handler{
		service: service,
		i18n:    bundle,
	}
}

func (h *Handler) listUsers(c *gin.Context) {
	filter, err := bindListUsersRequest(c)
	if err != nil {
		_ = c.Error(err)
		return
	}

	result, err := h.service.ListUsers(c.Request.Context(), filter)
	if err != nil {
		_ = c.Error(err)
		return
	}

	locale := i18n.LocaleFromContext(c.Request.Context())
	c.JSON(http.StatusOK, toListUsersResponse(result, locale, h.i18n))
}

func (h *Handler) getUser(c *gin.Context) {
	userID, err := parseUserIDParam(c)
	if err != nil {
		_ = c.Error(err)
		return
	}

	user, err := h.service.GetUser(c.Request.Context(), userID)
	if err != nil {
		_ = c.Error(err)
		return
	}

	locale := i18n.LocaleFromContext(c.Request.Context())
	c.JSON(http.StatusOK, toUserResponse(user, locale, h.i18n))
}

func (h *Handler) createUser(c *gin.Context) {
	var req createUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(err)
		return
	}

	roleIDs, err := parseRoleIDStrings(req.RoleIDs)
	if err != nil {
		_ = c.Error(err)
		return
	}

	actorID, meta := extractActorAndMeta(c)
	user, err := h.service.CreateUser(c.Request.Context(), CreateUserInput{
		Username:    req.Username,
		Email:       req.Email,
		DisplayName: req.DisplayName,
		Password:    req.Password,
		Status:      req.Status,
		RoleIDs:     roleIDs,
		ActorID:     actorID,
		Meta:        meta,
	})
	if err != nil {
		_ = c.Error(err)
		return
	}

	locale := i18n.LocaleFromContext(c.Request.Context())
	c.JSON(http.StatusCreated, toUserResponse(user, locale, h.i18n))
}

func (h *Handler) updateUser(c *gin.Context) {
	userID, err := parseUserIDParam(c)
	if err != nil {
		_ = c.Error(err)
		return
	}

	var req updateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(err)
		return
	}

	var roleIDsPtr *[]uuid.UUID
	if req.RoleIDs != nil {
		parsed, err := parseRoleIDStrings(*req.RoleIDs)
		if err != nil {
			_ = c.Error(err)
			return
		}
		roleIDsPtr = &parsed
	}

	actorID, meta := extractActorAndMeta(c)
	user, err := h.service.UpdateUser(c.Request.Context(), UpdateUserInput{
		UserID:      userID,
		Email:       req.Email,
		DisplayName: req.DisplayName,
		Status:      req.Status,
		Password:    req.Password,
		RoleIDs:     roleIDsPtr,
		ActorID:     actorID,
		Meta:        meta,
	})
	if err != nil {
		_ = c.Error(err)
		return
	}

	locale := i18n.LocaleFromContext(c.Request.Context())
	c.JSON(http.StatusOK, toUserResponse(user, locale, h.i18n))
}

func (h *Handler) deleteUser(c *gin.Context) {
	userID, err := parseUserIDParam(c)
	if err != nil {
		_ = c.Error(err)
		return
	}

	actorID, meta := extractActorAndMeta(c)
	if err := h.service.DeleteUser(c.Request.Context(), DeleteUserInput{
		UserID:  userID,
		ActorID: actorID,
		Meta:    meta,
	}); err != nil {
		_ = c.Error(err)
		return
	}

	c.Status(http.StatusNoContent)
}
