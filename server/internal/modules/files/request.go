package files

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"tianshu/internal/platform/storage"
	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/pagination"
)

const (
	maxSearchQueryLen = 100
	maxMultiFilterLen = 20
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

	q := strings.TrimSpace(c.Query("q"))
	if utf8.RuneCountInString(q) > maxSearchQueryLen {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "q", Message: ErrCodeQueryTooLong})
	}

	categories := parseMultiQuery(c, "category")
	for _, cat := range categories {
		if cat != CategoryImage && cat != CategoryDocument {
			fieldErrs = append(fieldErrs, apperr.FieldError{Field: "category", Message: ErrCodeInvalidCategory})
			break
		}
	}

	drivers := parseMultiQuery(c, "driver")
	for _, drv := range drivers {
		if drv != storage.DriverLocal && drv != storage.DriverS3 {
			fieldErrs = append(fieldErrs, apperr.FieldError{Field: "driver", Message: ErrCodeInvalidDriver})
			break
		}
	}

	mimeTypes := parseMultiQuery(c, "mimeType")
	if len(mimeTypes) > maxMultiFilterLen {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "mimeType", Message: ErrCodeTooManyMIMEFilters})
	}

	sortBy := strings.TrimSpace(c.Query("sort"))
	if sortBy != "" && sortBy != SortCreatedAt && sortBy != SortOriginalName && sortBy != SortSizeBytes {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "sort", Message: "VALIDATION_ONE_OF"})
	}

	order := strings.ToLower(strings.TrimSpace(c.Query("order")))
	if order != "" && order != OrderAsc && order != OrderDesc {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "order", Message: "VALIDATION_ONE_OF"})
	}

	if len(fieldErrs) > 0 {
		return ListFilter{}, apperr.Validation(sortAndDedupeFieldErrors(fieldErrs))
	}

	return ListFilter{
		Pagination: pagination.Normalize(page, pageSize),
		Q:          q,
		Categories: categories,
		MIMETypes:  mimeTypes,
		Drivers:    drivers,
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

func parseMultiQuery(c *gin.Context, key string) []string {
	rawValues := c.QueryArray(key)
	if len(rawValues) == 0 {
		return nil
	}
	out := make([]string, 0, len(rawValues))
	seen := make(map[string]struct{}, len(rawValues))
	for _, item := range rawValues {
		for _, part := range strings.Split(item, ",") {
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
	}
	return out
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
