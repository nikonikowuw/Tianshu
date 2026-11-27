package audit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"tianshu/internal/platform/i18n"
	"tianshu/internal/platform/logger"
	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/pagination"
)

type fakeStore struct {
	entries        []Entry
	deletedBatches []int
	countErr       error
	listErr        error
	deleteErr      error
}

func (f *fakeStore) InsertAuditLog(_ context.Context, entry Entry) error {
	entry.CreatedAt = time.Now().UTC()
	f.entries = append(f.entries, entry)
	return nil
}

func (f *fakeStore) CountAuditLogs(_ context.Context, filter ListFilter) (int64, error) {
	if f.countErr != nil {
		return 0, f.countErr
	}
	return int64(len(f.filterEntries(filter))), nil
}

func (f *fakeStore) ListAuditLogs(_ context.Context, filter ListFilter) ([]Entry, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	matched := f.filterEntries(filter)
	start := min(int(filter.Pagination.Offset()), len(matched))
	end := min(start+int(filter.Pagination.Limit()), len(matched))
	return slices.Clone(matched[start:end]), nil
}

func (f *fakeStore) DeleteExpiredBatch(_ context.Context, cutoff time.Time, limit int32) (int64, error) {
	if f.deleteErr != nil {
		return 0, f.deleteErr
	}
	var remaining []Entry
	var deleted int64
	for _, entry := range f.entries {
		if entry.CreatedAt.Before(cutoff) && deleted < int64(limit) {
			deleted++
			continue
		}
		remaining = append(remaining, entry)
	}
	f.entries = remaining
	f.deletedBatches = append(f.deletedBatches, int(deleted))
	return deleted, nil
}

func (f *fakeStore) filterEntries(filter ListFilter) []Entry {
	var out []Entry
	for _, e := range f.entries {
		if filter.ActorID != nil {
			if e.ActorID == nil || *e.ActorID != *filter.ActorID {
				continue
			}
		}
		if len(filter.Statuses) > 0 && !slices.Contains(filter.Statuses, e.Status) {
			continue
		}
		if len(filter.Actions) > 0 && !slices.Contains(filter.Actions, e.Action) {
			continue
		}
		if len(filter.Resources) > 0 && !slices.Contains(filter.Resources, e.Resource) {
			continue
		}
		if filter.Q != "" {
			q := strings.ToLower(filter.Q)
			if !strings.Contains(strings.ToLower(e.ActorUsername), q) &&
				!strings.Contains(strings.ToLower(e.Action), q) &&
				!strings.Contains(strings.ToLower(e.RequestID), q) {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}

type namedMetaMap map[string]any

type nestedSecretPayload struct {
	Username   string `json:"username"`
	NewPwd     string `json:"newPwd"`
	Passphrase string `json:"passphrase"`
	SignedURL  string `json:"signedUrl"`
	Inner      *struct {
		AccessKey string `json:"access_key_id"`
		SafeNote  string `json:"safeNote"`
	} `json:"inner"`
}

func TestRedactMetadataAndRecord(t *testing.T) {
	st := &fakeStore{}
	svc := newServiceWithStore(st)

	actorID := uuid.Must(uuid.NewV7())
	headers := http.Header{
		"Authorization": {"Bearer secret-header"},
		"Cookie":        {"refresh=secret-cookie"},
		"X-Trace":       {"trace-1"},
	}
	named := namedMetaMap{
		"old_pwd":        "legacy-secret",
		"userPwdConfirm": "confirm-secret",
		"passcode":       "123456",
		"displayName":    "Admin User",
		"cwd":            "/srv/app",
	}
	payload := nestedSecretPayload{
		Username:   "admin",
		NewPwd:     "brand-new-secret",
		Passphrase: "key-passphrase",
		SignedURL:  "/api/v1/files/signed/bearer-tok",
		Inner: &struct {
			AccessKey string `json:"access_key_id"`
			SafeNote  string `json:"safeNote"`
		}{
			AccessKey: "AKIA-SECRET",
			SafeNote:  "visible",
		},
	}
	inputMeta := map[string]any{
		"username":     "admin",
		"password":     "super-secret",
		"refreshToken": "tok-123",
		"nested": map[string]any{
			"api_key": "ak-xyz",
			"reason":  "invalid_credentials",
		},
		"items": []any{
			map[string]any{"authorization": "Bearer abc", "ok": true},
		},
		"headers":    headers,
		"named":      named,
		"structBody": payload,
		"structPtr":  &payload,
		"mapSlice":   []map[string]string{{"token": "tok-slice", "label": "ok"}},
		"rawJson":    json.RawMessage(`{"password":"raw-secret","nested":[{"cookie":"c=1","keep":"yes"}]}`),
	}

	ctx := logger.WithRequestID(t.Context(), "req_audit_001")
	err := svc.Record(ctx, RecordInput{
		ActorID:       &actorID,
		ActorUsername: "admin\x00\xff",
		Action:        "auth.login",
		Resource:      "session",
		ResourceID:    "sess-1",
		Status:        StatusFailure,
		IPAddress:     "127.0.0.1",
		UserAgent:     "a" + strings.Repeat("🚀", 140) + "\x00\xff",
		Metadata:      inputMeta,
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	// Original caller maps/headers must not be mutated and must not alias persisted metadata.
	if inputMeta["password"] != "super-secret" {
		t.Fatal("input metadata was mutated")
	}
	if headers.Get("Authorization") != "Bearer secret-header" {
		t.Fatal("input http.Header was mutated")
	}
	if named["old_pwd"] != "legacy-secret" {
		t.Fatal("input named map was mutated")
	}
	headers.Set("X-Trace", "mutated-after-record")
	named["displayName"] = "mutated-after-record"

	if len(st.entries) != 1 {
		t.Fatalf("entries count = %d, want 1", len(st.entries))
	}
	got := st.entries[0]
	if got.RequestID != "req_audit_001" {
		t.Fatalf("RequestID = %q, want req_audit_001", got.RequestID)
	}
	if got.ActorUsername != "admin" || !utf8.ValidString(got.ActorUsername) {
		t.Fatalf("ActorUsername = %q, want sanitized 'admin'", got.ActorUsername)
	}
	if !utf8.ValidString(got.UserAgent) || len(got.UserAgent) > maxUserAgentBytes || strings.ContainsRune(got.UserAgent, '\x00') {
		t.Fatalf("UserAgent invalid after truncation: len=%d valid=%v", len(got.UserAgent), utf8.ValidString(got.UserAgent))
	}
	if len(got.UserAgent) != 509 { // "a" (1 byte) + 127 * 4-byte emoji = 509 <= 512
		t.Fatalf("UserAgent byte length = %d, want 509", len(got.UserAgent))
	}
	if got.Metadata["password"] != redactedPlaceholder {
		t.Fatalf("password = %v, want %s", got.Metadata["password"], redactedPlaceholder)
	}
	if got.Metadata["refreshToken"] != redactedPlaceholder {
		t.Fatalf("refreshToken = %v, want %s", got.Metadata["refreshToken"], redactedPlaceholder)
	}
	nested := got.Metadata["nested"].(map[string]any)
	if nested["api_key"] != redactedPlaceholder || nested["reason"] != "invalid_credentials" {
		t.Fatalf("unexpected nested metadata: %+v", nested)
	}
	items := got.Metadata["items"].([]any)
	firstItem := items[0].(map[string]any)
	if firstItem["authorization"] != redactedPlaceholder || firstItem["ok"] != true {
		t.Fatalf("unexpected array metadata item: %+v", firstItem)
	}

	gotHeaders := got.Metadata["headers"].(map[string]any)
	if gotHeaders["Authorization"] != redactedPlaceholder || gotHeaders["Cookie"] != redactedPlaceholder {
		t.Fatalf("http.Header sensitive keys not redacted: %+v", gotHeaders)
	}
	gotTrace := gotHeaders["X-Trace"].([]any)
	if len(gotTrace) != 1 || gotTrace[0] != "trace-1" {
		t.Fatalf("http.Header non-sensitive key aliased or corrupted: %+v", gotTrace)
	}

	gotNamed := got.Metadata["named"].(map[string]any)
	if gotNamed["old_pwd"] != redactedPlaceholder || gotNamed["userPwdConfirm"] != redactedPlaceholder || gotNamed["passcode"] != redactedPlaceholder {
		t.Fatalf("named map sensitive keys not redacted: %+v", gotNamed)
	}
	if gotNamed["displayName"] != "Admin User" || gotNamed["cwd"] != "/srv/app" {
		t.Fatalf("named map non-sensitive keys mutated or over-redacted: %+v", gotNamed)
	}

	for _, key := range []string{"structBody", "structPtr"} {
		gotStruct := got.Metadata[key].(map[string]any)
		if gotStruct["newPwd"] != redactedPlaceholder || gotStruct["passphrase"] != redactedPlaceholder || gotStruct["signedUrl"] != redactedPlaceholder {
			t.Fatalf("%s sensitive fields not redacted: %+v", key, gotStruct)
		}
		inner := gotStruct["inner"].(map[string]any)
		if inner["access_key_id"] != redactedPlaceholder || inner["safeNote"] != "visible" {
			t.Fatalf("%s nested struct redaction mismatch: %+v", key, inner)
		}
	}

	gotMapSlice := got.Metadata["mapSlice"].([]any)
	firstMap := gotMapSlice[0].(map[string]any)
	if firstMap["token"] != redactedPlaceholder || firstMap["label"] != "ok" {
		t.Fatalf("[]map[string]string not redacted: %+v", firstMap)
	}

	gotRaw := got.Metadata["rawJson"].(map[string]any)
	if gotRaw["password"] != redactedPlaceholder {
		t.Fatalf("json.RawMessage password not redacted: %+v", gotRaw)
	}
	rawNested := gotRaw["nested"].([]any)[0].(map[string]any)
	if rawNested["cookie"] != redactedPlaceholder || rawNested["keep"] != "yes" {
		t.Fatalf("json.RawMessage nested object not redacted: %+v", rawNested)
	}
}

func TestRedactMetadataDepthLimitAndTruncateUAChinese(t *testing.T) {
	// Build a nested map deeper than maxRedactDepth with a secret at the bottom.
	deep := map[string]any{"password": "leaf-secret"}
	for range maxRedactDepth + 2 {
		deep = map[string]any{"next": deep}
	}
	redacted := RedactMetadata(deep)
	encoded, err := json.Marshal(redacted)
	if err != nil {
		t.Fatalf("json.Marshal(redacted) error = %v", err)
	}
	if strings.Contains(string(encoded), "leaf-secret") {
		t.Fatalf("deeply nested secret leaked past depth limit: %s", string(encoded))
	}
	if !strings.Contains(string(encoded), redactedPlaceholder) {
		t.Fatalf("expected depth cutoff placeholder, got: %s", string(encoded))
	}

	// 171 3-byte Chinese runes = 513 bytes -> must truncate cleanly to 170 runes (510 bytes).
	ua := truncateUA(strings.Repeat("天", 171))
	if !utf8.ValidString(ua) || len(ua) != 510 {
		t.Fatalf("truncateUA Chinese: len=%d valid=%v, want len=510 valid=true", len(ua), utf8.ValidString(ua))
	}
}

func TestRecordValidationAndPurgeExpired(t *testing.T) {
	st := &fakeStore{}
	svc := newServiceWithStore(st)
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	svc.purgeBatchSize = 2

	if err := svc.Record(t.Context(), RecordInput{
		Action:   "",
		Resource: "",
		Status:   "invalid",
	}); err == nil {
		t.Fatal("expected validation error")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Status != 422 || len(appErr.Fields) != 3 {
		t.Fatalf("unexpected validation error: %v", err)
	}

	// Seed 3 expired entries and 1 fresh entry.
	st.entries = []Entry{
		{ID: uuid.Must(uuid.NewV7()), CreatedAt: now.AddDate(0, 0, -200)},
		{ID: uuid.Must(uuid.NewV7()), CreatedAt: now.AddDate(0, 0, -190)},
		{ID: uuid.Must(uuid.NewV7()), CreatedAt: now.AddDate(0, 0, -181)},
		{ID: uuid.Must(uuid.NewV7()), CreatedAt: now.AddDate(0, 0, -10)},
	}

	if deleted, err := svc.PurgeExpired(t.Context(), 0); err != nil || deleted != 0 {
		t.Fatalf("PurgeExpired(0) = (%d, %v), want (0, nil)", deleted, err)
	}

	deleted, err := svc.PurgeExpired(t.Context(), 180)
	if err != nil {
		t.Fatalf("PurgeExpired(180) error = %v", err)
	}
	if deleted != 3 || len(st.entries) != 1 {
		t.Fatalf("deleted=%d remaining=%d, want 3 and 1", deleted, len(st.entries))
	}
	if !slices.Equal(st.deletedBatches, []int{2, 1}) {
		t.Fatalf("deletedBatches = %v, want [2 1]", st.deletedBatches)
	}

	// Canceled context must preserve context.Canceled via apperr.Error.Unwrap().
	canceledCtx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := svc.PurgeExpired(canceledCtx, 180); !errors.Is(err, context.Canceled) {
		t.Fatalf("PurgeExpired(canceled) err = %v, want context.Canceled", err)
	} else if appErr, ok := apperr.As(err); !ok || appErr.Status != 500 {
		t.Fatalf("PurgeExpired(canceled) expected *apperr.Error 500, got %v", err)
	}

	// Store error must be wrapped as *apperr.Error 500.
	st.deleteErr = errors.New("db lock timeout")
	if _, err := svc.PurgeExpired(t.Context(), 180); err == nil {
		t.Fatal("expected purge store error")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Status != 500 {
		t.Fatalf("PurgeExpired(storeErr) expected *apperr.Error 500, got %v", err)
	}
}

func TestListFilterNormalization(t *testing.T) {
	st := &fakeStore{}
	svc := newServiceWithStore(st)

	res, err := svc.List(t.Context(), ListFilter{
		Pagination: pagination.Params{Page: 1, PageSize: 20},
	})
	if err != nil {
		t.Fatalf("List() empty error = %v", err)
	}
	if res.Items == nil || len(res.Items) != 0 || res.Total != 0 {
		t.Fatalf("expected non-nil empty slice, got %+v", res)
	}

	// Explicit Nil UUID must be rejected rather than silently treated as unset.
	nilID := uuid.Nil
	if _, err := svc.List(t.Context(), ListFilter{ActorID: &nilID}); err == nil {
		t.Fatal("expected actorId nil UUID validation error")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Status != 422 || len(appErr.Fields) != 1 || appErr.Fields[0].Field != "actorId" {
		t.Fatalf("unexpected nil actorId error: %v", err)
	}

	// Blank and padded entries in filter slices must be cleaned without mutating caller slices.
	rawActions := []string{" auth.login ", "", "   "}
	normalized, err := normalizeFilter(ListFilter{
		Actions:   rawActions,
		Resources: []string{" session ", " "},
		Statuses:  []string{" success ", ""},
	})
	if err != nil {
		t.Fatalf("normalizeFilter() error = %v", err)
	}
	if !slices.Equal(normalized.Actions, []string{"auth.login"}) ||
		!slices.Equal(normalized.Resources, []string{"session"}) ||
		!slices.Equal(normalized.Statuses, []string{"success"}) {
		t.Fatalf("unexpected cleaned slices: actions=%v resources=%v statuses=%v", normalized.Actions, normalized.Resources, normalized.Statuses)
	}
	if rawActions[0] != " auth.login " {
		t.Fatal("caller Actions slice was mutated")
	}

	if _, err := svc.List(t.Context(), ListFilter{Sort: "actor_password"}); err == nil {
		t.Fatal("expected sort allowlist validation error")
	}
	if _, err := svc.List(t.Context(), ListFilter{Order: "sideways"}); err == nil {
		t.Fatal("expected order allowlist validation error")
	}

	from := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	to := from.Add(-time.Hour)
	if _, err := svc.List(t.Context(), ListFilter{From: &from, To: &to}); err == nil {
		t.Fatal("expected time range validation error")
	}
}

func TestAuditLocalesComplete(t *testing.T) {
	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle() error = %v", err)
	}
	if err := bundle.RegisterFS("audit", LocalesFS, "locales"); err != nil {
		t.Fatalf("RegisterFS(audit) error = %v", err)
	}
	if err := bundle.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	allKeys := []string{
		ErrCodeInvalidStatus,
		ErrCodeInvalidAction,
		ErrCodeInvalidResource,
		ErrCodeInvalidTimeFormat,
		ErrCodeInvalidTimeRange,
		ErrCodeInvalidNumber,
		ErrCodeInvalidPage,
		ErrCodeInvalidPageSize,
		ErrCodeQueryTooLong,
		ErrCodeActorTooLong,
	}
	for _, key := range allKeys {
		zh := bundle.Translate(i18n.LangZhCN, key, nil)
		en := bundle.Translate(i18n.LangEnUS, key, nil)
		if zh == "" || zh == key {
			t.Fatalf("missing zh-CN translation for %s (got %q)", key, zh)
		}
		if en == "" || en == key {
			t.Fatalf("missing en-US translation for %s (got %q)", key, en)
		}
		if zh == en {
			t.Fatalf("zh-CN and en-US translations identical for %s: %q", key, zh)
		}
	}
}
