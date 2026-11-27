package dashboard

import "tianshu/internal/shared/apperr"

// Module-prefixed error codes returned by the dashboard module.
const (
	ErrCodeInvalidDays = "DASHBOARD_INVALID_DAYS"
)

func invalidDaysError() *apperr.Error {
	return apperr.Validation([]apperr.FieldError{{
		Field:   "days",
		Message: ErrCodeInvalidDays,
	}})
}
