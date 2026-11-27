package identity

import (
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/authctx"
	"tianshu/internal/shared/pagination"
)

const (
	maxSearchQueryRunes    = 100
	maxMultiFilterItems    = 20
	maxMultiFilterRawItems = 100
)

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

type updateProfileRequest struct {
	Email        *string                `json:"email" binding:"omitempty,email,max=255"`
	DisplayName  *string                `json:"displayName" binding:"omitempty,max=100"`
	AvatarFileID optionalNullableString `json:"avatarFileId"`
}

type changePasswordRequest struct {
	CurrentPassword     string `json:"currentPassword" binding:"required,max=128"`
	NewPassword         string `json:"newPassword" binding:"required,min=8,max=128"`
	RevokeOtherSessions *bool  `json:"revokeOtherSessions"`
}

func bindListUsersRequest(c *gin.Context) (UserListFilter, error) {
	var fieldErrs []apperr.FieldError

	page, pageOK := parseOptionalInt(c.Query("page"), pagination.DefaultPage)
	if !pageOK || page < pagination.DefaultPage {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "page",
			Message: ErrCodeInvalidPage,
		})
	}

	pageSize, pageSizeOK := parseOptionalInt(c.Query("pageSize"), pagination.DefaultPageSize)
	if !pageSizeOK || pageSize < pagination.MinPageSize || pageSize > pagination.MaxPageSize {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "pageSize",
			Message: ErrCodeInvalidPageSize,
		})
	}

	q := strings.TrimSpace(c.Query("q"))
	if !isValidText(q) {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "q",
			Message: "VALIDATION_DEFAULT",
		})
	} else if utf8.RuneCountInString(q) > maxSearchQueryRunes {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "q",
			Message: ErrCodeQueryTooLong,
		})
	}

	statuses, statusErr := parseStatusQuery(c.QueryArray("status"))
	if statusErr != nil {
		fieldErrs = append(fieldErrs, *statusErr)
	}

	roleIDs, roleErr := parseRoleIDQuery(c.QueryArray("roleId"))
	if roleErr != nil {
		fieldErrs = append(fieldErrs, *roleErr)
	}

	sortBy := strings.TrimSpace(c.Query("sort"))
	if sortBy != "" && !isAllowedUserSort(sortBy) {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "sort",
			Message: "VALIDATION_ONE_OF",
		})
	}

	order := strings.ToLower(strings.TrimSpace(c.Query("order")))
	if order != "" && order != "asc" && order != "desc" {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "order",
			Message: "VALIDATION_ONE_OF",
		})
	}

	if len(fieldErrs) > 0 {
		return UserListFilter{}, apperr.Validation(fieldErrs)
	}

	return UserListFilter{
		Pagination: pagination.Normalize(page, pageSize),
		Q:          q,
		Statuses:   statuses,
		RoleIDs:    roleIDs,
		Sort:       sortBy,
		Order:      order,
	}, nil
}

func parseOptionalInt(raw string, fallback int) (int, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback, true
	}
	v, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, false
	}
	return v, true
}

func parseStatusQuery(raw []string) ([]string, *apperr.FieldError) {
	if len(raw) > maxMultiFilterRawItems {
		return nil, &apperr.FieldError{Field: "status", Message: ErrCodeInvalidStatus}
	}
	seen := make(map[string]struct{}, min(len(raw), maxMultiFilterItems))
	statuses := make([]string, 0, min(len(raw), maxMultiFilterItems))
	for _, item := range raw {
		st := strings.TrimSpace(item)
		if st == "" {
			continue
		}
		if st != StatusActive && st != StatusDisabled {
			return nil, &apperr.FieldError{Field: "status", Message: ErrCodeInvalidStatus}
		}
		if _, exists := seen[st]; exists {
			continue
		}
		seen[st] = struct{}{}
		statuses = append(statuses, st)
	}
	return statuses, nil
}

func parseRoleIDQuery(raw []string) ([]uuid.UUID, *apperr.FieldError) {
	if len(raw) > maxMultiFilterRawItems {
		return nil, &apperr.FieldError{Field: "roleId", Message: ErrCodeTooManyRoleFilters}
	}
	seen := make(map[uuid.UUID]struct{}, min(len(raw), maxMultiFilterItems))
	roleIDs := make([]uuid.UUID, 0, min(len(raw), maxMultiFilterItems))
	for _, item := range raw {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		parsed, err := uuid.Parse(trimmed)
		if err != nil || parsed == uuid.Nil {
			return nil, &apperr.FieldError{Field: "roleId", Message: "VALIDATION_UUID"}
		}
		if _, exists := seen[parsed]; exists {
			continue
		}
		if len(roleIDs) >= maxMultiFilterItems {
			return nil, &apperr.FieldError{Field: "roleId", Message: ErrCodeTooManyRoleFilters}
		}
		seen[parsed] = struct{}{}
		roleIDs = append(roleIDs, parsed)
	}
	return roleIDs, nil
}

func parseIDParam(c *gin.Context) (uuid.UUID, error) {
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

func extractAuthIdentityAndMeta(c *gin.Context) (authctx.Identity, RequestMeta, bool) {
	identity, ok := authctx.IdentityFromContext(c.Request.Context())
	return identity, RequestMeta{
		IPAddress: c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	}, ok
}

func parseOptionalAvatarFileID(opt optionalNullableString) (*uuid.UUID, bool, error) {
	if !opt.Set {
		return nil, false, nil
	}
	if opt.Value == nil || strings.TrimSpace(*opt.Value) == "" {
		return nil, true, nil
	}
	parsed, err := uuid.Parse(strings.TrimSpace(*opt.Value))
	if err != nil || parsed == uuid.Nil {
		return nil, false, apperr.Validation([]apperr.FieldError{{
			Field:   "avatarFileId",
			Message: ErrCodeProfileInvalidAvatar,
		}})
	}
	return &parsed, false, nil
}
