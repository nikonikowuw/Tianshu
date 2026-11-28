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

	rawActions, actionRawOK := splitMultiValuesWithGuard(c.QueryArray("action"))
	if !actionRawOK {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "action",
			Message: ErrCodeInvalidAction,
		})
	}

	rawResources, resourceRawOK := splitMultiValuesWithGuard(c.QueryArray("resource"))
	if !resourceRawOK {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "resource",
			Message: ErrCodeInvalidResource,
		})
	}

	rawStatuses, statusRawOK := splitMultiValuesWithGuard(c.QueryArray("status"))
	if !statusRawOK {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "status",
			Message: ErrCodeInvalidStatus,
		})
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

	normalized, filterErrs := validateAndNormalizeFilterFields(ListFilter{
		Pagination: pagination.Normalize(page, pageSize),
		Q:          c.Query("q"),
		Actor:      c.Query("actor"),
		ActorID:    actorUUID,
		Actions:    rawActions,
		Resources:  rawResources,
		Statuses:   rawStatuses,
		From:       fromTime,
		To:         toTime,
		Sort:       c.Query("sort"),
		Order:      c.Query("order"),
	})
	fieldErrs = append(fieldErrs, filterErrs...)

	if len(fieldErrs) > 0 {
		return ListFilter{}, apperr.Validation(fieldErrs)
	}
	return normalized, nil
}

func validateAndNormalizeFilterFields(filter ListFilter) (ListFilter, []apperr.FieldError) {
	var fieldErrs []apperr.FieldError
	out := filter
	out.Pagination = pagination.Normalize(filter.Pagination.Page, filter.Pagination.PageSize)

	out.Q = strings.TrimSpace(filter.Q)
	if !isValidFilterText(out.Q) {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "q", Message: "VALIDATION_DEFAULT"})
	} else if utf8.RuneCountInString(out.Q) > maxSearchQueryLen {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "q", Message: ErrCodeQueryTooLong})
	}

	out.Actor = strings.TrimSpace(filter.Actor)
	if !isValidFilterText(out.Actor) {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "actor", Message: "VALIDATION_DEFAULT"})
	} else if utf8.RuneCountInString(out.Actor) > maxActorQueryLen {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "actor", Message: ErrCodeActorTooLong})
	}

	if out.ActorID != nil {
		if *out.ActorID == uuid.Nil {
			fieldErrs = append(fieldErrs, apperr.FieldError{
				Field:   "actorId",
				Message: "VALIDATION_UUID",
			})
		} else {
			copied := *out.ActorID
			out.ActorID = &copied
		}
	}

	actions, okAction := cleanAndValidateTokens(filter.Actions, maxFilterTokenRunes, maxMultiFilterItems)
	if !okAction {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "action",
			Message: ErrCodeInvalidAction,
		})
	}
	out.Actions = actions

	resources, okResource := cleanAndValidateTokens(filter.Resources, maxFilterTokenRunes, maxMultiFilterItems)
	if !okResource {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "resource",
			Message: ErrCodeInvalidResource,
		})
	}
	out.Resources = resources

	statuses, okStatus := cleanAndValidateTokens(filter.Statuses, maxStatusTokenRunes, maxMultiFilterItems)
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
	out.Statuses = statuses

	if out.From != nil {
		utc := out.From.UTC()
		out.From = &utc
	}
	if out.To != nil {
		utc := out.To.UTC()
		out.To = &utc
	}
	if out.From != nil && out.To != nil && out.From.After(*out.To) {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "from",
			Message: ErrCodeInvalidTimeRange,
		})
	}

	sortBy := strings.TrimSpace(filter.Sort)
	if sortBy == "" {
		sortBy = SortCreatedAt
	}
	switch sortBy {
	case SortCreatedAt, SortAction, SortResource, SortStatus:
		out.Sort = sortBy
	default:
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "sort",
			Message: "VALIDATION_ONE_OF",
		})
	}

	order := strings.ToLower(strings.TrimSpace(filter.Order))
	if order == "" {
		order = OrderDesc
	}
	switch order {
	case OrderAsc, OrderDesc:
		out.Order = order
	default:
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "order",
			Message: "VALIDATION_ONE_OF",
		})
	}

	return out, fieldErrs
}

func isValidFilterText(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0)
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

func splitMultiValuesWithGuard(raw []string) ([]string, bool) {
	if len(raw) == 0 {
		return nil, true
	}
	out := make([]string, 0, len(raw))
	for _, part := range raw {
		for item := range strings.SplitSeq(part, ",") {
			if len(out) >= maxMultiFilterRawItems {
				return nil, false
			}
			out = append(out, item)
		}
	}
	return out, true
}

func cleanAndValidateTokens(raw []string, maxItemRunes, maxItems int) ([]string, bool) {
	if len(raw) == 0 {
		return nil, true
	}
	seen := make(map[string]struct{}, min(len(raw), maxItems))
	out := make([]string, 0, min(len(raw), maxItems))
	for _, item := range raw {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if !isValidFilterText(trimmed) || utf8.RuneCountInString(trimmed) > maxItemRunes {
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
