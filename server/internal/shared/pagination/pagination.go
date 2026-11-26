package pagination

const (
	// DefaultPage is the 1-based fallback page index.
	DefaultPage = 1
	// DefaultPageSize is the default items per page.
	DefaultPageSize = 20
	// MinPageSize is the minimum allowed page size.
	MinPageSize = 10
	// MaxPageSize is the maximum allowed page size.
	MaxPageSize = 100
)

// Params represents normalized 1-based pagination parameters.
type Params struct {
	Page     int
	PageSize int
}

// Normalize clamps page and pageSize into the contract bounds.
func Normalize(page, pageSize int) Params {
	if page < DefaultPage {
		page = DefaultPage
	}
	switch {
	case pageSize <= 0:
		pageSize = DefaultPageSize
	case pageSize < MinPageSize:
		pageSize = MinPageSize
	case pageSize > MaxPageSize:
		pageSize = MaxPageSize
	}
	return Params{
		Page:     page,
		PageSize: pageSize,
	}
}

// Offset returns the SQL OFFSET for the normalized page.
func (p Params) Offset() int32 {
	n := Normalize(p.Page, p.PageSize)
	return int32((n.Page - 1) * n.PageSize)
}

// Limit returns the SQL LIMIT for the normalized page size.
func (p Params) Limit() int32 {
	n := Normalize(p.Page, p.PageSize)
	return int32(n.PageSize)
}

// Result is the contract-fixed paginated list response without an outer envelope.
type Result[T any] struct {
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
}

// NewResult constructs a Result and guarantees empty slices marshal as [] instead of null.
func NewResult[T any](items []T, total int64, params Params) Result[T] {
	n := Normalize(params.Page, params.PageSize)
	if items == nil {
		items = make([]T, 0)
	}
	return Result[T]{
		Items:    items,
		Total:    total,
		Page:     n.Page,
		PageSize: n.PageSize,
	}
}
