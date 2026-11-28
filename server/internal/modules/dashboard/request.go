package dashboard

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func bindSummaryRequest(c *gin.Context) (SummaryInput, error) {
	raw := strings.TrimSpace(c.Query("days"))
	if raw == "" {
		return SummaryInput{}, nil
	}
	days, err := strconv.Atoi(raw)
	if err != nil {
		return SummaryInput{}, invalidDaysError()
	}
	return SummaryInput{Days: &days}, nil
}
