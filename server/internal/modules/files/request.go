package files

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"tianshu/internal/platform/storage"
	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/pagination"
)

const (
	maxSearchQueryLen    = 100
	maxMultiFilterLen    = 20
	maxMIMETypeFilterLen = 128
)

type createSignedURLRequest struct {
	Disposition string `json:"disposition"`
}

func parseListFilter(c *gin.Context) (ListFilter, error) {
	var fieldErrs []apperr.FieldError

	page, pageOK := parseOptionalInt(c.Query("page"), pagination.DefaultPage)
	if !pageOK || page < 1 {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "page", Message: ErrCodeInvalidPage})
	}

	pageSize, pageSizeOK := parseOptionalInt(c.Query("pageSize"), pagination.DefaultPageSize)
	if !pageSizeOK || pageSize < pagination.MinPageSize || pageSize > pagination.MaxPageSize {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "pageSize", Message: ErrCodeInvalidPageSize})
	}

	raw := ListFilter{
		Pagination: pagination.Normalize(page, pageSize),
		Q:          c.Query("q"),
		Categories: splitMultiValues(c.QueryArray("category")),
		MIMETypes:  splitMultiValues(c.QueryArray("mimeType")),
		Drivers:    splitMultiValues(c.QueryArray("driver")),
		Sort:       c.Query("sort"),
		Order:      c.Query("order"),
	}

	normalized, filterErrs := validateAndNormalizeFilterFields(raw)
	fieldErrs = append(fieldErrs, filterErrs...)
	if len(fieldErrs) > 0 {
		return ListFilter{}, apperr.Validation(fieldErrs)
	}
	return normalized, nil
}

func validateAndNormalizeFilterFields(filter ListFilter) (ListFilter, []apperr.FieldError) {
	var fieldErrs []apperr.FieldError
	out := filter

	out.Q = strings.TrimSpace(filter.Q)
	if !isValidFilterText(out.Q) {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "q", Message: "VALIDATION_DEFAULT"})
	} else if utf8.RuneCountInString(out.Q) > maxSearchQueryLen {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "q", Message: ErrCodeQueryTooLong})
	}

	out.Categories = normalizeMultiValues(filter.Categories)
	for _, cat := range out.Categories {
		if cat != CategoryImage && cat != CategoryDocument {
			fieldErrs = append(fieldErrs, apperr.FieldError{Field: "category", Message: ErrCodeInvalidCategory})
			break
		}
	}

	out.Drivers = normalizeMultiValues(filter.Drivers)
	for _, drv := range out.Drivers {
		if drv != storage.DriverLocal && drv != storage.DriverS3 {
			fieldErrs = append(fieldErrs, apperr.FieldError{Field: "driver", Message: ErrCodeInvalidDriver})
			break
		}
	}

	out.MIMETypes = normalizeMultiValues(filter.MIMETypes)
	if len(out.MIMETypes) > maxMultiFilterLen {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "mimeType", Message: ErrCodeTooManyMIMEFilters})
	} else {
		for _, mt := range out.MIMETypes {
			if !isValidFilterText(mt) || utf8.RuneCountInString(mt) > maxMIMETypeFilterLen {
				fieldErrs = append(fieldErrs, apperr.FieldError{Field: "mimeType", Message: "VALIDATION_DEFAULT"})
				break
			}
		}
	}

	sortBy := strings.TrimSpace(filter.Sort)
	if sortBy == "" {
		sortBy = SortCreatedAt
	}
	if sortBy != SortCreatedAt && sortBy != SortOriginalName && sortBy != SortSizeBytes {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "sort", Message: "VALIDATION_ONE_OF"})
	} else {
		out.Sort = sortBy
	}

	order := strings.ToLower(strings.TrimSpace(filter.Order))
	if order == "" {
		order = OrderDesc
	}
	if order != OrderAsc && order != OrderDesc {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "order", Message: "VALIDATION_ONE_OF"})
	} else {
		out.Order = order
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

func splitMultiValues(rawValues []string) []string {
	if len(rawValues) == 0 {
		return nil
	}
	out := make([]string, 0, len(rawValues))
	for _, item := range rawValues {
		out = append(out, strings.Split(item, ",")...)
	}
	return out
}

func normalizeMultiValues(rawValues []string) []string {
	if len(rawValues) == 0 {
		return nil
	}
	out := make([]string, 0, len(rawValues))
	seen := make(map[string]struct{}, len(rawValues))
	for _, part := range rawValues {
		norm := strings.ToLower(strings.TrimSpace(part))
		if norm == "" {
			continue
		}
		if _, exists := seen[norm]; exists {
			continue
		}
		seen[norm] = struct{}{}
		out = append(out, norm)
	}
	return out
}
