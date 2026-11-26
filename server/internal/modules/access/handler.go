package access

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"tianshu/internal/platform/i18n"
	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/authctx"
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

func (h *Handler) listRoleOptions(c *gin.Context) {
	refs, err := h.service.ListRoleOptions(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}

	locale := i18n.LocaleFromContext(c.Request.Context())
	c.JSON(http.StatusOK, toListRoleOptionsResponse(refs, locale, h.i18n))
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
	roleID, err := parseUUIDParam(c)
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
	roleID, err := parseUUIDParam(c)
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
	roleID, err := parseUUIDParam(c)
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

func (h *Handler) listMenuTree(c *gin.Context) {
	items, err := h.service.ListMenuTree(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	locale := i18n.LocaleFromContext(c.Request.Context())
	c.JSON(http.StatusOK, toMenuTreeResponse(items, locale, h.i18n))
}

func (h *Handler) listUserMenus(c *gin.Context) {
	identity, ok := authctx.IdentityFromContext(c.Request.Context())
	if !ok {
		_ = c.Error(apperr.Unauthorized(apperr.CodeUnauthorized))
		return
	}
	items, err := h.service.ListUserMenuTree(c.Request.Context(), identity.UserID)
	if err != nil {
		_ = c.Error(err)
		return
	}
	locale := i18n.LocaleFromContext(c.Request.Context())
	c.JSON(http.StatusOK, toMenuTreeResponse(items, locale, h.i18n))
}

func (h *Handler) createMenu(c *gin.Context) {
	var req createMenuRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(err)
		return
	}
	parentID, err := parseOptionalUUID(req.ParentID, "parentId", ErrCodeMenuInvalidParent)
	if err != nil {
		_ = c.Error(err)
		return
	}
	visible := true
	if req.Visible != nil {
		visible = *req.Visible
	}
	var sortOrder int32
	if req.SortOrder != nil {
		sortOrder = *req.SortOrder
	}

	actorID, meta := extractActorAndMeta(c)
	menu, err := h.service.CreateMenu(c.Request.Context(), CreateMenuInput{
		ParentID:       parentID,
		Type:           req.Type,
		Code:           req.Code,
		Title:          req.Title,
		Path:           req.Path,
		Icon:           req.Icon,
		PermissionCode: req.PermissionCode,
		SortOrder:      sortOrder,
		Visible:        visible,
		Status:         req.Status,
		ActorID:        actorID,
		Meta:           meta,
	})
	if err != nil {
		_ = c.Error(err)
		return
	}
	locale := i18n.LocaleFromContext(c.Request.Context())
	c.JSON(http.StatusCreated, toMenuResponse(menu, locale, h.i18n))
}

func (h *Handler) updateMenu(c *gin.Context) {
	menuID, err := parseUUIDParam(c)
	if err != nil {
		_ = c.Error(err)
		return
	}
	var req updateMenuRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(err)
		return
	}

	var (
		parentID    *uuid.UUID
		clearParent bool
	)
	if req.ParentID.Set {
		if req.ParentID.Value == nil || strings.TrimSpace(*req.ParentID.Value) == "" {
			clearParent = true
		} else {
			parsed, pErr := parseOptionalUUID(req.ParentID.Value, "parentId", ErrCodeMenuInvalidParent)
			if pErr != nil {
				_ = c.Error(pErr)
				return
			}
			parentID = parsed
		}
	}

	actorID, meta := extractActorAndMeta(c)
	menu, err := h.service.UpdateMenu(c.Request.Context(), UpdateMenuInput{
		MenuID:         menuID,
		ParentID:       parentID,
		ClearParent:    clearParent,
		Type:           req.Type,
		Title:          req.Title,
		Path:           req.Path,
		Icon:           req.Icon,
		PermissionCode: req.PermissionCode,
		SortOrder:      req.SortOrder,
		Visible:        req.Visible,
		Status:         req.Status,
		ActorID:        actorID,
		Meta:           meta,
	})
	if err != nil {
		_ = c.Error(err)
		return
	}
	locale := i18n.LocaleFromContext(c.Request.Context())
	c.JSON(http.StatusOK, toMenuResponse(menu, locale, h.i18n))
}

func (h *Handler) reorderMenus(c *gin.Context) {
	var req reorderMenusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(err)
		return
	}
	items, err := req.toInputItems()
	if err != nil {
		_ = c.Error(err)
		return
	}
	actorID, meta := extractActorAndMeta(c)
	tree, err := h.service.ReorderMenus(c.Request.Context(), ReorderMenusInput{
		Items:   items,
		ActorID: actorID,
		Meta:    meta,
	})
	if err != nil {
		_ = c.Error(err)
		return
	}
	locale := i18n.LocaleFromContext(c.Request.Context())
	c.JSON(http.StatusOK, toMenuTreeResponse(tree, locale, h.i18n))
}

func (h *Handler) deleteMenu(c *gin.Context) {
	menuID, err := parseUUIDParam(c)
	if err != nil {
		_ = c.Error(err)
		return
	}
	actorID, meta := extractActorAndMeta(c)
	if err := h.service.DeleteMenu(c.Request.Context(), menuID, actorID, meta); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}
