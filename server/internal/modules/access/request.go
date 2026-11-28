package access

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/authctx"
	"tianshu/internal/shared/pagination"
)

const maxRoleQueryRunes = 100

type createRoleRequest struct {
	Code        string   `json:"code" binding:"required,min=2,max=64"`
	Name        string   `json:"name" binding:"required,min=1,max=100"`
	Description string   `json:"description" binding:"omitempty,max=500"`
	Permissions []string `json:"permissions" binding:"omitempty,max=200,dive,max=100"`
}

type updateRoleRequest struct {
	Name        *string   `json:"name" binding:"omitempty,min=1,max=100"`
	Description *string   `json:"description" binding:"omitempty,max=500"`
	Permissions *[]string `json:"permissions" binding:"omitempty,max=200,dive,max=100"`
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
	ParentID optionalNullableString `json:"parentId"`
	// Code is immutable after creation; bound here only so client attempts to change it trigger a clear validation/conflict error.
	Code           *string `json:"code" binding:"omitempty,max=64"`
	Type           *string `json:"type" binding:"omitempty,oneof=directory menu button"`
	Title          *string `json:"title" binding:"omitempty,max=100"`
	Path           *string `json:"path" binding:"omitempty,max=255"`
	Icon           *string `json:"icon" binding:"omitempty,max=64"`
	PermissionCode *string `json:"permissionCode" binding:"omitempty,max=100"`
	SortOrder      *int32  `json:"sortOrder"`
	Visible        *bool   `json:"visible"`
	Status         *string `json:"status" binding:"omitempty,oneof=active disabled"`
}

type reorderMenuItemRequest struct {
	ID        string  `json:"id" binding:"required"`
	ParentID  *string `json:"parentId"`
	SortOrder int32   `json:"sortOrder"`
}

type reorderMenusRequest struct {
	Items []reorderMenuItemRequest `json:"items" binding:"required,min=1,max=500"`
}

func (r reorderMenusRequest) toInputItems() ([]ReorderMenuItem, error) {
	out := make([]ReorderMenuItem, 0, len(r.Items))
	var fieldErrs []apperr.FieldError
	for idx, it := range r.Items {
		id, err := uuid.Parse(strings.TrimSpace(it.ID))
		if err != nil || id == uuid.Nil {
			fieldErrs = append(fieldErrs, apperr.FieldError{
				Field:   fmt.Sprintf("items[%d].id", idx),
				Message: "VALIDATION_UUID",
			})
			continue
		}
		parentID, err := parseOptionalUUID(it.ParentID, fmt.Sprintf("items[%d].parentId", idx), ErrCodeMenuInvalidParent)
		if err != nil {
			if appErr, ok := apperr.As(err); ok {
				fieldErrs = append(fieldErrs, appErr.Fields...)
			}
			continue
		}
		out = append(out, ReorderMenuItem{
			ID:        id,
			ParentID:  parentID,
			SortOrder: it.SortOrder,
		})
	}
	if len(fieldErrs) > 0 {
		return nil, apperr.Validation(dedupeSortFieldErrors(fieldErrs))
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
	page, pageSize, isSystem, fieldErrs := parseRoleQueryParams(c)

	trimmedQ := strings.TrimSpace(c.Query("q"))
	if !isValidText(trimmedQ) || utf8.RuneCountInString(trimmedQ) > maxRoleQueryRunes {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "q",
			Message: "VALIDATION_DEFAULT",
		})
	}

	sortVal := strings.TrimSpace(c.Query("sort"))
	if sortVal != "" && normalizeRoleSort(sortVal) == "" {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "sort",
			Message: "VALIDATION_ONE_OF",
		})
	}

	orderVal := strings.ToLower(strings.TrimSpace(c.Query("order")))
	if orderVal != "" && orderVal != "asc" && orderVal != "desc" {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "order",
			Message: "VALIDATION_ONE_OF",
		})
	}

	if len(fieldErrs) > 0 {
		return RoleListFilter{}, apperr.Validation(dedupeSortFieldErrors(fieldErrs))
	}

	return RoleListFilter{
		Pagination: pagination.Normalize(page, pageSize),
		Q:          trimmedQ,
		IsSystem:   isSystem,
		Sort:       sortVal,
		Order:      orderVal,
	}, nil
}

func parseUUIDParam(c *gin.Context) (uuid.UUID, error) {
	raw := strings.TrimSpace(c.Param("id"))
	parsed, err := uuid.Parse(raw)
	if err != nil || parsed == uuid.Nil {
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

func parseRoleQueryParams(c *gin.Context) (int, int, *bool, []apperr.FieldError) {
	var (
		page     = pagination.DefaultPage
		pageSize = pagination.DefaultPageSize
		isSystem *bool
		errs     []apperr.FieldError
	)
	if raw, exists := c.GetQuery("page"); exists {
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			val, err := strconv.Atoi(trimmed)
			switch {
			case err != nil:
				errs = append(errs, apperr.FieldError{
					Field:   "page",
					Message: ErrCodeInvalidNumber,
				})
			case val < pagination.DefaultPage:
				errs = append(errs, apperr.FieldError{
					Field:   "page",
					Message: "VALIDATION_DEFAULT",
				})
			default:
				page = val
			}
		}
	}
	if raw, exists := c.GetQuery("pageSize"); exists {
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			val, err := strconv.Atoi(trimmed)
			switch {
			case err != nil:
				errs = append(errs, apperr.FieldError{
					Field:   "pageSize",
					Message: ErrCodeInvalidNumber,
				})
			case val < pagination.MinPageSize || val > pagination.MaxPageSize:
				errs = append(errs, apperr.FieldError{
					Field:   "pageSize",
					Message: "VALIDATION_DEFAULT",
				})
			default:
				pageSize = val
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
	return page, pageSize, isSystem, errs
}

func dedupeSortFieldErrors(errs []apperr.FieldError) []apperr.FieldError {
	byField := make(map[string]apperr.FieldError, len(errs))
	for _, fe := range errs {
		if _, exists := byField[fe.Field]; !exists {
			byField[fe.Field] = fe
		}
	}
	out := make([]apperr.FieldError, 0, len(byField))
	for _, fe := range byField {
		out = append(out, fe)
	}
	slices.SortFunc(out, func(a, b apperr.FieldError) int {
		return cmp.Compare(a.Field, b.Field)
	})
	return out
}

func isValidText(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0)
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
