package identity

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/authctx"
	"tianshu/internal/shared/pagination"
)

type listUsersRequest struct {
	Page     *int     `form:"page" binding:"omitempty,min=1"`
	PageSize *int     `form:"pageSize" binding:"omitempty,min=10,max=100"`
	Q        string   `form:"q" binding:"omitempty,max=100"`
	Status   []string `form:"status"`
	RoleID   []string `form:"roleId"`
	Sort     string   `form:"sort" binding:"omitempty,oneof=created_at updated_at username last_login_at"`
	Order    string   `form:"order" binding:"omitempty,oneof=asc desc"`
}

type createUserRequest struct {
	Username    string   `json:"username" binding:"required,min=3,max=64"`
	Email       string   `json:"email" binding:"required,email,max=255"`
	DisplayName string   `json:"displayName" binding:"omitempty,max=100"`
	Password    string   `json:"password" binding:"required,min=8,max=128"`
	Status      string   `json:"status" binding:"omitempty,oneof=active disabled"`
	RoleIDs     []string `json:"roleIds"`
}

type updateUserRequest struct {
	Email       *string   `json:"email" binding:"omitempty,email,max=255"`
	DisplayName *string   `json:"displayName" binding:"omitempty,max=100"`
	Status      *string   `json:"status" binding:"omitempty,oneof=active disabled"`
	Password    *string   `json:"password" binding:"omitempty,min=8,max=128"`
	RoleIDs     *[]string `json:"roleIds"`
}

func bindListUsersRequest(c *gin.Context) (UserListFilter, error) {
	var req listUsersRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		return UserListFilter{}, err
	}

	var fieldErrs []apperr.FieldError
	statuses := make([]string, 0, len(req.Status))
	for _, raw := range req.Status {
		st := strings.TrimSpace(raw)
		if st == "" {
			continue
		}
		if st != StatusActive && st != StatusDisabled {
			fieldErrs = append(fieldErrs, apperr.FieldError{
				Field:   "status",
				Message: ErrCodeInvalidStatus,
			})
			break
		}
		statuses = append(statuses, st)
	}

	roleIDs := make([]uuid.UUID, 0, len(req.RoleID))
	for _, raw := range req.RoleID {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		parsed, err := uuid.Parse(trimmed)
		if err != nil {
			fieldErrs = append(fieldErrs, apperr.FieldError{
				Field:   "roleId",
				Message: "VALIDATION_UUID",
			})
			break
		}
		roleIDs = append(roleIDs, parsed)
	}

	if len(fieldErrs) > 0 {
		return UserListFilter{}, apperr.Validation(sortAndDedupeFieldErrors(fieldErrs))
	}

	page := pagination.DefaultPage
	if req.Page != nil {
		page = *req.Page
	}
	pageSize := pagination.DefaultPageSize
	if req.PageSize != nil {
		pageSize = *req.PageSize
	}

	return UserListFilter{
		Pagination: pagination.Normalize(page, pageSize),
		Q:          strings.TrimSpace(req.Q),
		Statuses:   statuses,
		RoleIDs:    roleIDs,
		Sort:       req.Sort,
		Order:      req.Order,
	}, nil
}

func parseUserIDParam(c *gin.Context) (uuid.UUID, error) {
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

func parseRoleIDStrings(raw []string) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0, len(raw))
	for _, item := range raw {
		parsed, err := uuid.Parse(strings.TrimSpace(item))
		if err != nil || parsed == uuid.Nil {
			return nil, apperr.Validation([]apperr.FieldError{{
				Field:   "roleIds",
				Message: ErrCodeInvalidRole,
			}})
		}
		ids = append(ids, parsed)
	}
	return ids, nil
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
