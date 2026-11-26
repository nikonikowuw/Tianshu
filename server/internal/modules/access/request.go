package access

import (
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

func bindListRolesRequest(c *gin.Context) (RoleListFilter, error) {
	if fieldErrs := validateRoleQueryParams(c); len(fieldErrs) > 0 {
		return RoleListFilter{}, apperr.Validation(sortAndDedupeFieldErrors(fieldErrs))
	}

	var req listRolesRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		return RoleListFilter{}, err
	}

	var isSystem *bool
	if raw := strings.TrimSpace(req.IsSystem); raw != "" {
		parsed, err := parseStrictBool(raw)
		if err != nil {
			return RoleListFilter{}, apperr.Validation([]apperr.FieldError{{
				Field:   "isSystem",
				Message: ErrCodeInvalidBool,
			}})
		}
		isSystem = &parsed
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

func parseRoleIDParam(c *gin.Context) (uuid.UUID, error) {
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

func validateRoleQueryParams(c *gin.Context) []apperr.FieldError {
	var errs []apperr.FieldError
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
			if _, err := parseStrictBool(trimmed); err != nil {
				errs = append(errs, apperr.FieldError{
					Field:   "isSystem",
					Message: ErrCodeInvalidBool,
				})
			}
		}
	}
	return errs
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
