package access

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/authctx"
	"tianshu/internal/shared/pagination"
)

type listRolesRequest struct {
	Page     *int   `form:"page" binding:"omitempty,min=1"`
	PageSize *int   `form:"pageSize" binding:"omitempty,min=10,max=100"`
	Q        string `form:"q" binding:"omitempty,max=100"`
	IsSystem string `form:"isSystem"`
	Sort     string `form:"sort" binding:"omitempty,oneof=created_at updated_at code name"`
	Order    string `form:"order" binding:"omitempty,oneof=asc desc"`
}

type createRoleRequest struct {
	Code        string   `json:"code" binding:"required,min=2,max=64"`
	Name        string   `json:"name" binding:"required,min=1,max=100"`
	Description string   `json:"description" binding:"omitempty,max=500"`
	Permissions []string `json:"permissions"`
}

type updateRoleRequest struct {
	Name        *string   `json:"name" binding:"omitempty,min=1,max=100"`
	Description *string   `json:"description" binding:"omitempty,max=500"`
	Permissions *[]string `json:"permissions"`
}

type createMenuRequest struct {
	ParentID       *string `json:"parentId"`
	Type           string  `json:"type" binding:"required,oneof=directory menu button"`
	Code           string  `json:"code" binding:"required,min=2,max=64"`
	Title          string  `json:"title" binding:"required,min=1,max=100"`
	Path           string  `json:"path" binding:"omitempty,max=255"`
	Icon           string  `json:"icon" binding:"omitempty,max=64"`
	PermissionCode string  `json:"permissionCode" binding:"omitempty,max=100"`
	SortOrder      *int32  `json:"sortOrder"`
	Visible        *bool   `json:"visible"`
	Status         string  `json:"status" binding:"omitempty,oneof=active disabled"`
}

type optionalNullableString struct {
	Set   bool
	Value *string
}

func (o *optionalNullableString) UnmarshalJSON(data []byte) error {
	o.Set = true
	if string(data) == "null" {
		o.Value = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	o.Value = &s
	return nil
}

type updateMenuRequest struct {
	ParentID       optionalNullableString `json:"parentId"`
	Code           *string                `json:"code" binding:"omitempty,max=64"`
	Type           *string                `json:"type" binding:"omitempty,oneof=directory menu button"`
	Title          *string                `json:"title" binding:"omitempty,max=100"`
	Path           *string                `json:"path" binding:"omitempty,max=255"`
	Icon           *string                `json:"icon" binding:"omitempty,max=64"`
	PermissionCode *string                `json:"permissionCode" binding:"omitempty,max=100"`
	SortOrder      *int32                 `json:"sortOrder"`
	Visible        *bool                  `json:"visible"`
	Status         *string                `json:"status" binding:"omitempty,oneof=active disabled"`
}

type reorderMenuItemRequest struct {
	ID        string  `json:"id" binding:"required"`
	ParentID  *string `json:"parentId"`
	SortOrder int32   `json:"sortOrder"`
}

type reorderMenusRequest struct {
	Items []reorderMenuItemRequest `json:"items" binding:"required"`
}

func (r reorderMenusRequest) toInputItems() ([]ReorderMenuItem, error) {
	out := make([]ReorderMenuItem, 0, len(r.Items))
	for _, it := range r.Items {
		id, err := uuid.Parse(strings.TrimSpace(it.ID))
		if err != nil || id == uuid.Nil {
			return nil, apperr.Validation([]apperr.FieldError{{
				Field:   "items",
				Message: "VALIDATION_UUID",
			}})
		}
		parentID, err := parseOptionalUUID(it.ParentID, "items", ErrCodeMenuInvalidParent)
		if err != nil {
			return nil, err
		}
		out = append(out, ReorderMenuItem{
			ID:        id,
			ParentID:  parentID,
			SortOrder: it.SortOrder,
		})
	}
	return out, nil
}

func parseOptionalUUID(raw *string, field, errCode string) (*uuid.UUID, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	parsed, err := uuid.Parse(strings.TrimSpace(*raw))
	if err != nil || parsed == uuid.Nil {
		return nil, apperr.Validation([]apperr.FieldError{{
			Field:   field,
			Message: errCode,
		}})
	}
	return &parsed, nil
}

func bindListRolesRequest(c *gin.Context) (RoleListFilter, error) {
	isSystem, fieldErrs := parseRoleQueryExtras(c)
	if len(fieldErrs) > 0 {
		return RoleListFilter{}, apperr.Validation(sortAndDedupeFieldErrors(fieldErrs))
	}

	var req listRolesRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		return RoleListFilter{}, err
	}

	page := pagination.DefaultPage
	if req.Page != nil {
		page = *req.Page
	}
	pageSize := pagination.DefaultPageSize
	if req.PageSize != nil {
		pageSize = *req.PageSize
	}

	return RoleListFilter{
		Pagination: pagination.Normalize(page, pageSize),
		Q:          strings.TrimSpace(req.Q),
		IsSystem:   isSystem,
		Sort:       req.Sort,
		Order:      req.Order,
	}, nil
}

func parseUUIDParam(c *gin.Context) (uuid.UUID, error) {
	raw := strings.TrimSpace(c.Param("id"))
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apperr.Validation([]apperr.FieldError{{
			Field:   "id",
			Message: "VALIDATION_UUID",
		}})
	}
	return parsed, nil
}

func extractActorAndMeta(c *gin.Context) (*uuid.UUID, RequestMeta) {
	var actorID *uuid.UUID
	if identity, ok := authctx.IdentityFromContext(c.Request.Context()); ok {
		id := identity.UserID
		actorID = &id
	}
	return actorID, RequestMeta{
		IPAddress: c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	}
}

func parseRoleQueryExtras(c *gin.Context) (*bool, []apperr.FieldError) {
	var (
		isSystem *bool
		errs     []apperr.FieldError
	)
	for _, name := range []string{"page", "pageSize"} {
		if raw, exists := c.GetQuery(name); exists {
			if strings.TrimSpace(raw) == "" {
				continue
			}
			if _, err := strconv.Atoi(raw); err != nil {
				errs = append(errs, apperr.FieldError{
					Field:   name,
					Message: ErrCodeInvalidNumber,
				})
			}
		}
	}
	if raw, exists := c.GetQuery("isSystem"); exists {
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			parsed, err := parseStrictBool(trimmed)
			if err != nil {
				errs = append(errs, apperr.FieldError{
					Field:   "isSystem",
					Message: ErrCodeInvalidBool,
				})
			} else {
				isSystem = &parsed
			}
		}
	}
	return isSystem, errs
}

func parseStrictBool(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, strconv.ErrSyntax
	}
}
