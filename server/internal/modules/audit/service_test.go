package audit

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"tianshu/internal/platform/i18n"
	"tianshu/internal/platform/logger"
	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/pagination"
)

type fakeStore struct {
	entries        []Entry
	deletedBatches []int
}

func (f *fakeStore) InsertAuditLog(_ context.Context, entry Entry) error {
	entry.CreatedAt = time.Now().UTC()
	f.entries = append(f.entries, entry)
	return nil
}

func (f *fakeStore) CountAuditLogs(_ context.Context, filter ListFilter) (int64, error) {
	return int64(len(f.filterEntries(filter))), nil
}

func (f *fakeStore) ListAuditLogs(_ context.Context, filter ListFilter) ([]Entry, error) {
	matched := f.filterEntries(filter)
	start := min(int(filter.Pagination.Offset()), len(matched))
	end := min(start+int(filter.Pagination.Limit()), len(matched))
	return slices.Clone(matched[start:end]), nil
}

func (f *fakeStore) DeleteExpiredBatch(_ context.Context, cutoff time.Time, limit int32) (int64, error) {
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

func TestRedactMetadataAndRecord(t *testing.T) {
	st := &fakeStore{}
	svc := newServiceWithStore(st, logger.New("test", "error", io.Discard))

	actorID := uuid.Must(uuid.NewV7())
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
	}

	ctx := logger.WithRequestID(t.Context(), "req_audit_001")
	err := svc.Record(ctx, RecordInput{
		ActorID:       &actorID,
		ActorUsername: "admin",
		Action:        "auth.login",
		Resource:      "session",
		ResourceID:    "sess-1",
		Status:        StatusFailure,
		IPAddress:     "127.0.0.1",
		UserAgent:     strings.Repeat("x", 700),
		Metadata:      inputMeta,
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	// Original caller map must not be mutated.
	if inputMeta["password"] != "super-secret" {
		t.Fatal("input metadata was mutated")
	}

	if len(st.entries) != 1 {
		t.Fatalf("entries count = %d, want 1", len(st.entries))
	}
	got := st.entries[0]
	if got.RequestID != "req_audit_001" {
		t.Fatalf("RequestID = %q, want req_audit_001", got.RequestID)
	}
	if len(got.UserAgent) != maxUserAgentBytes {
		t.Fatalf("UserAgent length = %d, want %d", len(got.UserAgent), maxUserAgentBytes)
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
}

func TestRecordValidationAndPurgeExpired(t *testing.T) {
	st := &fakeStore{}
	svc := newServiceWithStore(st, logger.New("test", "error", io.Discard))
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
}

func TestListFilterNormalization(t *testing.T) {
	st := &fakeStore{}
	svc := newServiceWithStore(st, logger.New("test", "error", io.Discard))

	res, err := svc.List(t.Context(), ListFilter{
		Pagination: pagination.Params{Page: 1, PageSize: 20},
	})
	if err != nil {
		t.Fatalf("List() empty error = %v", err)
	}
	if res.Items == nil || len(res.Items) != 0 || res.Total != 0 {
		t.Fatalf("expected non-nil empty slice, got %+v", res)
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
}
