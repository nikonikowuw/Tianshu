package audit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

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
	maxRedactDepth                  = 16
	redactedPlaceholder             = "[REDACTED]"
)

var sensitiveKeyFragments = []string{
	"password",
	"passwd",
	"passphrase",
	"passcode",
	"secret",
	"token",
	"cookie",
	"authorization",
	"credential",
	"apikey",
	"accesskey",
	"privatekey",
	"signedurl",
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
	return redactMap(src, 0)
}

func redactMap(src map[string]any, depth int) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		cleanKey := sanitizeJSONText(k)
		if isSensitiveKey(k) || isSensitiveKey(cleanKey) {
			out[cleanKey] = redactedPlaceholder
			continue
		}
		if out[cleanKey] == redactedPlaceholder {
			continue
		}
		out[cleanKey] = redactValue(v, depth+1)
	}
	return out
}

func redactValue(v any, depth int) any {
	if v == nil {
		return nil
	}
	switch typed := v.(type) {
	case string:
		return sanitizeJSONText(typed)
	case bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64,
		json.Number, time.Time, uuid.UUID:
		return v
	case map[string]any:
		if depth >= maxRedactDepth {
			return redactedPlaceholder
		}
		return redactMap(typed, depth)
	case map[string]string:
		if depth >= maxRedactDepth {
			return redactedPlaceholder
		}
		out := make(map[string]any, len(typed))
		for k, val := range typed {
			cleanKey := sanitizeJSONText(k)
			if isSensitiveKey(k) || isSensitiveKey(cleanKey) {
				out[cleanKey] = redactedPlaceholder
			} else if out[cleanKey] != redactedPlaceholder {
				out[cleanKey] = sanitizeJSONText(val)
			}
		}
		return out
	case []any:
		if depth >= maxRedactDepth {
			return redactedPlaceholder
		}
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = redactValue(item, depth+1)
		}
		return out
	case []map[string]any:
		if depth >= maxRedactDepth {
			return redactedPlaceholder
		}
		out := make([]any, len(typed))
		for i, item := range typed {
			if item == nil {
				continue
			}
			out[i] = redactMap(item, depth+1)
		}
		return out
	case []string:
		out := make([]string, len(typed))
		for i, item := range typed {
			out[i] = sanitizeJSONText(item)
		}
		return out
	default:
		if depth >= maxRedactDepth {
			return redactedPlaceholder
		}
		return redactViaJSON(v, depth)
	}
}

func redactViaJSON(v any, depth int) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("<unserializable:%T>", v)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var decoded any
	if err := dec.Decode(&decoded); err != nil {
		return fmt.Sprintf("<unserializable:%T>", v)
	}
	return redactValue(decoded, depth)
}

var sensitiveKeyReplacer = strings.NewReplacer("_", "", "-", "", ".", "", " ", "", "\t", "")

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = sensitiveKeyReplacer.Replace(normalized)
	if normalized == "" {
		return false
	}
	if strings.HasPrefix(normalized, "pwd") || strings.HasSuffix(normalized, "pwd") || hasWordToken(key, "pwd") {
		return true
	}
	for _, frag := range sensitiveKeyFragments {
		if strings.Contains(normalized, frag) {
			return true
		}
	}
	return false
}

func hasWordToken(key, target string) bool {
	var token strings.Builder
	flush := func() bool {
		if token.Len() == 0 {
			return false
		}
		matched := token.String() == target
		token.Reset()
		return matched
	}

	var prevLower bool
	for _, r := range key {
		switch {
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			if flush() {
				return true
			}
			prevLower = false
		case unicode.IsUpper(r):
			if prevLower && flush() {
				return true
			}
			token.WriteRune(unicode.ToLower(r))
			prevLower = false
		default:
			token.WriteRune(unicode.ToLower(r))
			prevLower = unicode.IsLower(r)
		}
	}
	return flush()
}

func cloneMetadata(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	return maps.Clone(src)
}

func sanitizeJSONText(raw string) string {
	if raw == "" {
		return ""
	}
	cleaned := strings.ReplaceAll(raw, "\x00", "")
	if !utf8.ValidString(cleaned) {
		cleaned = strings.ToValidUTF8(cleaned, "")
	}
	return cleaned
}

func sanitizeUTF8(raw string) string {
	return strings.TrimSpace(sanitizeJSONText(raw))
}

func truncateUA(ua string) string {
	cleaned := sanitizeUTF8(ua)
	if len(cleaned) <= maxUserAgentBytes {
		return cleaned
	}
	cut := maxUserAgentBytes
	for cut > 0 && !utf8.RuneStart(cleaned[cut]) {
		cut--
	}
	return cleaned[:cut]
}
