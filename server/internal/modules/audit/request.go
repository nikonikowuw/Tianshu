package audit

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/pagination"
)

const (
	maxSearchQueryLen      = 100
	maxActorQueryLen       = 100
	maxFilterTokenRunes    = 64
	maxStatusTokenRunes    = 32
	maxMultiFilterItems    = 20
	maxMultiFilterRawItems = 100
)

func bindListAuditLogsRequest(c *gin.Context) (ListFilter, error) {
	var fieldErrs []apperr.FieldError

	page, pageParseOK := parseOptionalInt(c.Query("page"), pagination.DefaultPage)
	if !pageParseOK {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "page", Message: ErrCodeInvalidNumber})
	} else if page < pagination.DefaultPage {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "page", Message: ErrCodeInvalidPage})
	}

	pageSize, pageSizeParseOK := parseOptionalInt(c.Query("pageSize"), pagination.DefaultPageSize)
	if !pageSizeParseOK {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "pageSize", Message: ErrCodeInvalidNumber})
	} else if pageSize < pagination.MinPageSize || pageSize > pagination.MaxPageSize {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "pageSize", Message: ErrCodeInvalidPageSize})
	}

	q := strings.TrimSpace(c.Query("q"))
	if utf8.RuneCountInString(q) > maxSearchQueryLen {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "q", Message: ErrCodeQueryTooLong})
	}

	actor := strings.TrimSpace(c.Query("actor"))
	if utf8.RuneCountInString(actor) > maxActorQueryLen {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "actor", Message: ErrCodeActorTooLong})
	}

	var actorUUID *uuid.UUID
	if trimmedID := strings.TrimSpace(c.Query("actorId")); trimmedID != "" {
		parsed, err := uuid.Parse(trimmedID)
		if err != nil || parsed == uuid.Nil {
			fieldErrs = append(fieldErrs, apperr.FieldError{
				Field:   "actorId",
				Message: "VALIDATION_UUID",
			})
		} else {
			actorUUID = &parsed
		}
	}

	actions, okAction := normalizeMultiValues(c.QueryArray("action"), maxFilterTokenRunes, maxMultiFilterItems)
	if !okAction {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "action",
			Message: ErrCodeInvalidAction,
		})
	}

	resources, okResource := normalizeMultiValues(c.QueryArray("resource"), maxFilterTokenRunes, maxMultiFilterItems)
	if !okResource {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "resource",
			Message: ErrCodeInvalidResource,
		})
	}

	statuses, okStatus := normalizeMultiValues(c.QueryArray("status"), maxStatusTokenRunes, maxMultiFilterItems)
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

	fromTime, fromOK := parseOptionalRFC3339(c.Query("from"))
	if !fromOK {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "from",
			Message: ErrCodeInvalidTimeFormat,
		})
	}

	toTime, toOK := parseOptionalRFC3339(c.Query("to"))
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

	sortBy := strings.TrimSpace(c.Query("sort"))
	if sortBy != "" && sortBy != SortCreatedAt && sortBy != SortAction && sortBy != SortResource && sortBy != SortStatus {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "sort",
			Message: "VALIDATION_ONE_OF",
		})
	}

	order := strings.ToLower(strings.TrimSpace(c.Query("order")))
	if order != "" && order != OrderAsc && order != OrderDesc {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "order",
			Message: "VALIDATION_ONE_OF",
		})
	}

	if len(fieldErrs) > 0 {
		return ListFilter{}, apperr.Validation(fieldErrs)
	}

	return ListFilter{
		Pagination: pagination.Normalize(page, pageSize),
		Q:          q,
		Actor:      actor,
		ActorID:    actorUUID,
		Actions:    actions,
		Resources:  resources,
		Statuses:   statuses,
		From:       fromTime,
		To:         toTime,
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

func normalizeMultiValues(raw []string, maxItemRunes, maxItems int) ([]string, bool) {
	if len(raw) == 0 {
		return nil, true
	}
	seen := make(map[string]struct{}, min(len(raw), maxItems))
	out := make([]string, 0, min(len(raw), maxItems))
	rawScanned := 0
	for _, part := range raw {
		for item := range strings.SplitSeq(part, ",") {
			rawScanned++
			if rawScanned > maxMultiFilterRawItems {
				return nil, false
			}
			trimmed := strings.TrimSpace(item)
			if trimmed == "" {
				continue
			}
			if utf8.RuneCountInString(trimmed) > maxItemRunes {
				return nil, false
			}
			if _, exists := seen[trimmed]; exists {
				continue
			}
			if len(out) >= maxItems {
				return nil, false
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
