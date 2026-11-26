package audit

import (
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"tianshu/internal/shared/pagination"
)

// Audit result statuses stored in audit_logs.status.
const (
	StatusSuccess = "success"
	StatusFailure = "failure"
)

// Allowed sort columns for audit log queries.
const (
	SortCreatedAt = "created_at"
	SortAction    = "action"
	SortResource  = "resource"
	SortStatus    = "status"
)

// Sort directions for audit log queries.
const (
	OrderAsc  = "asc"
	OrderDesc = "desc"
)

const (
	defaultRetentionBatchSize int32 = 5000
	maxUserAgentBytes               = 512
	redactedPlaceholder             = "[REDACTED]"
)

var sensitiveKeyFragments = []string{
	"password",
	"passwd",
	"secret",
	"token",
	"cookie",
	"authorization",
	"credential",
	"apikey",
	"privatekey",
	"jwt",
}

// Entry represents a persisted immutable audit log record.
type Entry struct {
	ID            uuid.UUID
	ActorID       *uuid.UUID
	ActorUsername string
	Action        string
	Resource      string
	ResourceID    string
	Status        string
	IPAddress     string
	UserAgent     string
	RequestID     string
	Metadata      map[string]any
	CreatedAt     time.Time
}

// RecordInput defines the domain payload required to append an audit log entry.
type RecordInput struct {
	ActorID       *uuid.UUID
	ActorUsername string
	Action        string
	Resource      string
	ResourceID    string
	Status        string
	IPAddress     string
	UserAgent     string
	RequestID     string
	Metadata      map[string]any
}

// ListFilter defines validated query parameters for listing audit logs.
type ListFilter struct {
	Pagination pagination.Params
	Q          string
	Actor      string
	ActorID    *uuid.UUID
	Actions    []string
	Resources  []string
	Statuses   []string
	From       *time.Time
	To         *time.Time
	Sort       string
	Order      string
}

// RedactMetadata deep-clones metadata while replacing sensitive values with "[REDACTED]".
func RedactMetadata(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		if isSensitiveKey(k) {
			out[k] = redactedPlaceholder
			continue
		}
		out[k] = redactValue(v)
	}
	return out
}

func redactValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		return RedactMetadata(typed)
	case map[string]string:
		out := make(map[string]any, len(typed))
		for k, val := range typed {
			if isSensitiveKey(k) {
				out[k] = redactedPlaceholder
			} else {
				out[k] = val
			}
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = redactValue(item)
		}
		return out
	case []map[string]any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = RedactMetadata(item)
		}
		return out
	case []string:
		return slices.Clone(typed)
	default:
		return v
	}
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, "-", "")
	if normalized == "pwd" {
		return true
	}
	for _, frag := range sensitiveKeyFragments {
		if strings.Contains(normalized, frag) {
			return true
		}
	}
	return false
}

func cloneMetadata(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	return maps.Clone(src)
}

func truncateUA(ua string) string {
	trimmed := strings.TrimSpace(ua)
	if len(trimmed) > maxUserAgentBytes {
		return trimmed[:maxUserAgentBytes]
	}
	return trimmed
}
