package audit

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/pagination"
)

type listAuditLogsRequest struct {
	Page     *int     `form:"page" binding:"omitempty,min=1"`
	PageSize *int     `form:"pageSize" binding:"omitempty,min=10,max=100"`
	Q        string   `form:"q" binding:"omitempty,max=100"`
	Actor    string   `form:"actor" binding:"omitempty,max=100"`
	ActorID  string   `form:"actorId" binding:"omitempty,uuid"`
	Action   []string `form:"action"`
	Resource []string `form:"resource"`
	Status   []string `form:"status"`
	From     string   `form:"from"`
	To       string   `form:"to"`
	Sort     string   `form:"sort" binding:"omitempty,oneof=created_at action resource status"`
	Order    string   `form:"order" binding:"omitempty,oneof=asc desc"`
}

func bindListAuditLogsRequest(c *gin.Context) (ListFilter, error) {
	if fieldErrs := validateNumericQueryParams(c); len(fieldErrs) > 0 {
		return ListFilter{}, apperr.Validation(fieldErrs)
	}

	var req listAuditLogsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		return ListFilter{}, err
	}

	var fieldErrs []apperr.FieldError

	actions, okAction := normalizeMultiValues(req.Action, 64)
	if !okAction {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "action",
			Message: ErrCodeInvalidAction,
		})
	}

	resources, okResource := normalizeMultiValues(req.Resource, 64)
	if !okResource {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "resource",
			Message: ErrCodeInvalidResource,
		})
	}

	statuses, okStatus := normalizeMultiValues(req.Status, 32)
	if !okStatus {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "status",
			Message: ErrCodeInvalidStatus,
		})
	} else {
		for _, st := range statuses {
			if st != StatusSuccess && st != StatusFailure {
				fieldErrs = append(fieldErrs, apperr.FieldError{
					Field:   "status",
					Message: ErrCodeInvalidStatus,
				})
				break
			}
		}
	}

	var actorUUID *uuid.UUID
	if trimmedID := strings.TrimSpace(req.ActorID); trimmedID != "" {
		parsed, err := uuid.Parse(trimmedID)
		if err != nil {
			fieldErrs = append(fieldErrs, apperr.FieldError{
				Field:   "actorId",
				Message: "VALIDATION_UUID",
			})
		} else {
			actorUUID = &parsed
		}
	}

	fromTime, fromOK := parseOptionalRFC3339(req.From)
	if !fromOK {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "from",
			Message: ErrCodeInvalidTimeFormat,
		})
	}

	toTime, toOK := parseOptionalRFC3339(req.To)
	if !toOK {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "to",
			Message: ErrCodeInvalidTimeFormat,
		})
	}

	if fromTime != nil && toTime != nil && fromTime.After(*toTime) {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "from",
			Message: ErrCodeInvalidTimeRange,
		})
	}

	if len(fieldErrs) > 0 {
		return ListFilter{}, apperr.Validation(sortAndDedupeFieldErrors(fieldErrs))
	}

	page := pagination.DefaultPage
	if req.Page != nil {
		page = *req.Page
	}
	pageSize := pagination.DefaultPageSize
	if req.PageSize != nil {
		pageSize = *req.PageSize
	}

	return ListFilter{
		Pagination: pagination.Normalize(page, pageSize),
		Q:          strings.TrimSpace(req.Q),
		Actor:      strings.TrimSpace(req.Actor),
		ActorID:    actorUUID,
		Actions:    actions,
		Resources:  resources,
		Statuses:   statuses,
		From:       fromTime,
		To:         toTime,
		Sort:       req.Sort,
		Order:      req.Order,
	}, nil
}

func validateNumericQueryParams(c *gin.Context) []apperr.FieldError {
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
	return errs
}

func normalizeMultiValues(raw []string, maxItemLen int) ([]string, bool) {
	if len(raw) == 0 {
		return nil, true
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, part := range raw {
		for item := range strings.SplitSeq(part, ",") {
			trimmed := strings.TrimSpace(item)
			if trimmed == "" {
				continue
			}
			if len(trimmed) > maxItemLen {
				return nil, false
			}
			if _, exists := seen[trimmed]; exists {
				continue
			}
			seen[trimmed] = struct{}{}
			out = append(out, trimmed)
		}
	}
	return out, true
}

func parseOptionalRFC3339(raw string) (*time.Time, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, true
	}
	parsed, err := time.Parse(time.RFC3339Nano, trimmed)
	if err != nil {
		return nil, false
	}
	utc := parsed.UTC()
	return &utc, true
}

func sortAndDedupeFieldErrors(in []apperr.FieldError) []apperr.FieldError {
	byField := make(map[string]apperr.FieldError, len(in))
	for _, fe := range in {
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
