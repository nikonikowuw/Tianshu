package pagination

import (
	"math"
	"testing"
)

func TestOffsetClampsAbsurdPageInsteadOfOverflowing(t *testing.T) {
	tests := []struct {
		name string
		page int
		size int
		want int32
	}{
		{name: "first page", page: 1, size: 20, want: 0},
		{name: "second page", page: 2, size: 20, want: 20},
		{name: "overflowing page", page: math.MaxInt32/100 + 2, size: 100, want: math.MaxInt32},
		{name: "huge page", page: 1 << 62, size: 100, want: math.MaxInt32},
		{name: "minimum int page", page: math.MinInt, size: 100, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Params{Page: tt.page, PageSize: tt.size}.Offset()
			if got != tt.want {
				t.Fatalf("Offset() = %d, want %d", got, tt.want)
			}
			if got < 0 {
				t.Fatalf("Offset() = %d, must never be negative", got)
			}
		})
	}
}
