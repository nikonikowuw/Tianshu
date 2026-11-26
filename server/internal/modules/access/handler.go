package access

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"tianshu/internal/platform/i18n"
)

// Handler exposes HTTP endpoints for the permission catalog and role CRUD.
type Handler struct {
	service *Service
	i18n    *i18n.Bundle
}

// NewHandler creates an access Handler backed by service and i18n bundle.
func NewHandler(service *Service, bundle *i18n.Bundle) *Handler {
	return &Handler{
		service: service,
		i18n:    bundle,
	}
}

func (h *Handler) listPermissions(c *gin.Context) {
	locale := i18n.LocaleFromContext(c.Request.Context())
	groups := h.service.ListPermissionGroups()
	c.JSON(http.StatusOK, toListPermissionsResponse(groups, locale, h.i18n))
}

func (h *Handler) listRoles(c *gin.Context) {
	filter, err := bindListRolesRequest(c)
	if err != nil {
		_ = c.Error(err)
		return
	}

	result, err := h.service.ListRoles(c.Request.Context(), filter)
	if err != nil {
		_ = c.Error(err)
		return
	}

	locale := i18n.LocaleFromContext(c.Request.Context())
	c.JSON(http.StatusOK, toListRolesResponse(result, locale, h.i18n))
}

func (h *Handler) getRole(c *gin.Context) {
	roleID, err := parseRoleIDParam(c)
	if err != nil {
		_ = c.Error(err)
		return
	}

	role, err := h.service.GetRole(c.Request.Context(), roleID)
	if err != nil {
		_ = c.Error(err)
		return
	}

	locale := i18n.LocaleFromContext(c.Request.Context())
	c.JSON(http.StatusOK, toRoleResponse(role, locale, h.i18n))
}

func (h *Handler) createRole(c *gin.Context) {
	var req createRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(err)
		return
	}

	actorID, meta := extractActorAndMeta(c)
	role, err := h.service.CreateRole(c.Request.Context(), CreateRoleInput{
		Code:        req.Code,
		Name:        req.Name,
		Description: req.Description,
		Permissions: req.Permissions,
		ActorID:     actorID,
		Meta:        meta,
	})
	if err != nil {
		_ = c.Error(err)
		return
	}

	locale := i18n.LocaleFromContext(c.Request.Context())
	c.JSON(http.StatusCreated, toRoleResponse(role, locale, h.i18n))
}

func (h *Handler) updateRole(c *gin.Context) {
	roleID, err := parseRoleIDParam(c)
	if err != nil {
		_ = c.Error(err)
		return
	}

	var req updateRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(err)
		return
	}

	actorID, meta := extractActorAndMeta(c)
	role, err := h.service.UpdateRole(c.Request.Context(), UpdateRoleInput{
		RoleID:      roleID,
		Name:        req.Name,
		Description: req.Description,
		Permissions: req.Permissions,
		ActorID:     actorID,
		Meta:        meta,
	})
	if err != nil {
		_ = c.Error(err)
		return
	}

	locale := i18n.LocaleFromContext(c.Request.Context())
	c.JSON(http.StatusOK, toRoleResponse(role, locale, h.i18n))
}

func (h *Handler) deleteRole(c *gin.Context) {
	roleID, err := parseRoleIDParam(c)
	if err != nil {
		_ = c.Error(err)
		return
	}

	actorID, meta := extractActorAndMeta(c)
	if err := h.service.DeleteRole(c.Request.Context(), DeleteRoleInput{
		RoleID:  roleID,
		ActorID: actorID,
		Meta:    meta,
	}); err != nil {
		_ = c.Error(err)
		return
	}

	c.Status(http.StatusNoContent)
}
