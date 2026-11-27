package dashboard

import (
	"strconv"
	"strings"
)

type summaryQuery struct {
	Days string `form:"days"`
}

func (q summaryQuery) toInput() (SummaryInput, error) {
	raw := strings.TrimSpace(q.Days)
	if raw == "" {
		return SummaryInput{}, nil
	}
	days, err := strconv.Atoi(raw)
	if err != nil {
		return SummaryInput{}, invalidDaysError()
	}
	return SummaryInput{Days: &days}, nil
}
