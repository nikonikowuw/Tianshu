package audit

// Translation keys and module-prefixed error codes owned by the audit module.
const (
	ErrCodeInvalidStatus     = "AUDIT_INVALID_STATUS"
	ErrCodeInvalidAction     = "AUDIT_INVALID_ACTION"
	ErrCodeInvalidResource   = "AUDIT_INVALID_RESOURCE"
	ErrCodeInvalidTimeFormat = "AUDIT_INVALID_TIME_FORMAT"
	ErrCodeInvalidTimeRange  = "AUDIT_INVALID_TIME_RANGE"
	ErrCodeInvalidNumber     = "AUDIT_INVALID_NUMBER"
	ErrCodeInvalidPage       = "AUDIT_INVALID_PAGE"
	ErrCodeInvalidPageSize   = "AUDIT_INVALID_PAGE_SIZE"
	ErrCodeQueryTooLong      = "AUDIT_QUERY_TOO_LONG"
	ErrCodeActorTooLong      = "AUDIT_ACTOR_TOO_LONG"
)
