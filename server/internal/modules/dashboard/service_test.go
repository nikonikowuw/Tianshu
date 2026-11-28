package dashboard

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"tianshu/internal/platform/i18n"
	"tianshu/internal/shared/apperr"
)

type fakeStore struct {
	userStats   UserStats
	roleStats   RoleStats
	fileStats   FileStats
	activeSess  int64
	trend       []DailyActivityPoint
	recent      []RecentActivityItem
	gotSince    time.Time
	gotNow      time.Time
	gotStartAt  time.Time
	gotEndAt    time.Time
	gotLimit    int32
	readTxCalls int
	readTxErr   error
	userErr     error
	roleErr     error
	fileErr     error
	sessionsErr error
	trendErr    error
	recentErr   error
}

func (f *fakeStore) WithinReadTx(ctx context.Context, fn func(context.Context, store) error) error {
	f.readTxCalls++
	if f.readTxErr != nil {
		return f.readTxErr
	}
	return fn(ctx, f)
}

func (f *fakeStore) GetUserStats(_ context.Context, since time.Time) (UserStats, error) {
	f.gotSince = since
	if f.userErr != nil {
		return UserStats{}, f.userErr
	}
	return f.userStats, nil
}

func (f *fakeStore) GetRoleStats(_ context.Context) (RoleStats, error) {
	if f.roleErr != nil {
		return RoleStats{}, f.roleErr
	}
	return f.roleStats, nil
}

func (f *fakeStore) GetFileStats(_ context.Context) (FileStats, error) {
	if f.fileErr != nil {
		return FileStats{}, f.fileErr
	}
	return f.fileStats, nil
}

func (f *fakeStore) CountActiveSessions(_ context.Context, now time.Time) (int64, error) {
	f.gotNow = now
	if f.sessionsErr != nil {
		return 0, f.sessionsErr
	}
	return f.activeSess, nil
}

func (f *fakeStore) ListDailyAuditActivity(_ context.Context, startAt, endAt time.Time) ([]DailyActivityPoint, error) {
	f.gotStartAt = startAt
	f.gotEndAt = endAt
	if f.trendErr != nil {
		return nil, f.trendErr
	}
	return f.trend, nil
}

func (f *fakeStore) ListRecentAuditActivity(_ context.Context, limit int32) ([]RecentActivityItem, error) {
	f.gotLimit = limit
	if f.recentErr != nil {
		return nil, f.recentErr
	}
	return f.recent, nil
}

func TestSummaryDefaultsGapFillsTrendAndAggregates(t *testing.T) {
	fixedNow := time.Date(2026, 11, 27, 15, 30, 0, 0, time.UTC)
	eventID := uuid.Must(uuid.NewV7())

	st := &fakeStore{
		userStats:  UserStats{Total: 12, Active: 10, Disabled: 2, NewLast7Days: 3},
		roleStats:  RoleStats{Total: 4, System: 1, Custom: 3},
		fileStats:  FileStats{Total: 9, TotalSizeBytes: 20480, Images: 6, Documents: 3},
		activeSess: 5,
		trend: []DailyActivityPoint{
			{Date: "2026-11-22", Logins: 3, Operations: 4, Failures: 1},
			{Date: "2026-11-27", Logins: 2, Operations: 1, Failures: 0},
		},
		recent: []RecentActivityItem{
			{
				ID:            eventID,
				ActorUsername: "admin",
				Action:        "auth.login",
				Resource:      "session",
				Status:        "success",
				IPAddress:     "127.0.0.1",
				CreatedAt:     fixedNow.Add(-5 * time.Minute),
			},
		},
	}

	svc := newServiceWithStore(st)
	svc.now = func() time.Time { return fixedNow }

	summary, err := svc.Summary(t.Context(), SummaryInput{})
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if summary.RangeDays != 7 {
		t.Fatalf("RangeDays = %d, want 7", summary.RangeDays)
	}
	if !summary.GeneratedAt.Equal(fixedNow) {
		t.Fatalf("GeneratedAt = %v, want %v", summary.GeneratedAt, fixedNow)
	}
	if len(summary.ActivityTrend) != 7 {
		t.Fatalf("len(ActivityTrend) = %d, want 7", len(summary.ActivityTrend))
	}
	if summary.ActivityTrend[0].Date != "2026-11-21" || summary.ActivityTrend[6].Date != "2026-11-27" {
		t.Fatalf("trend bounds = [%s .. %s], want [2026-11-21 .. 2026-11-27]", summary.ActivityTrend[0].Date, summary.ActivityTrend[6].Date)
	}
	if summary.ActivityTrend[1].Logins != 3 || summary.ActivityTrend[1].Operations != 4 || summary.ActivityTrend[1].Failures != 1 {
		t.Fatalf("trend[1] = %+v, want {2026-11-22 3 4 1}", summary.ActivityTrend[1])
	}
	if summary.ActivityTrend[2].Date != "2026-11-23" || summary.ActivityTrend[2].Logins != 0 {
		t.Fatalf("expected zero-filled gap on 2026-11-23, got %+v", summary.ActivityTrend[2])
	}
	if summary.Audit.LoginsInRange != 5 || summary.Audit.FailuresInRange != 1 || summary.Audit.TotalInRange != 11 {
		t.Fatalf("Audit = %+v, want {TotalInRange:11 LoginsInRange:5 FailuresInRange:1}", summary.Audit)
	}
	if wantStart := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC); !st.gotStartAt.Equal(wantStart) {
		t.Fatalf("gotStartAt = %v, want %v", st.gotStartAt, wantStart)
	}
	if wantEnd := time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC); !st.gotEndAt.Equal(wantEnd) {
		t.Fatalf("gotEndAt = %v, want %v", st.gotEndAt, wantEnd)
	}
	if wantSince := fixedNow.AddDate(0, 0, -7); !st.gotSince.Equal(wantSince) {
		t.Fatalf("gotSince = %v, want %v", st.gotSince, wantSince)
	}
	if st.gotLimit != defaultRecentLimit {
		t.Fatalf("gotLimit = %d, want %d", st.gotLimit, defaultRecentLimit)
	}
	if st.readTxCalls != 1 {
		t.Fatalf("readTxCalls = %d, want 1", st.readTxCalls)
	}
}

func TestSummaryRangeValidationAndEmptyRecent(t *testing.T) {
	fixedNow := time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC)

	t.Run("accepts 14 and 30 days across month boundary", func(t *testing.T) {
		for _, days := range []int{14, 30} {
			st := &fakeStore{}
			svc := newServiceWithStore(st)
			svc.now = func() time.Time { return fixedNow }

			d := days
			res, err := svc.Summary(t.Context(), SummaryInput{Days: &d})
			if err != nil {
				t.Fatalf("Summary(days=%d) error = %v", days, err)
			}
			if res.RangeDays != days || len(res.ActivityTrend) != days {
				t.Fatalf("Summary(days=%d) got range=%d len=%d", days, res.RangeDays, len(res.ActivityTrend))
			}
			if res.RecentActivity == nil {
				t.Fatalf("expected non-nil empty RecentActivity slice for days=%d", days)
			}
		}
	})

	t.Run("rejects unsupported days including zero with 422", func(t *testing.T) {
		svc := newServiceWithStore(&fakeStore{})
		for _, days := range []int{-7, 0, 1, 10, 15, 90} {
			d := days
			_, err := svc.Summary(t.Context(), SummaryInput{Days: &d})
			appErr, ok := apperr.As(err)
			if !ok || appErr.Code != apperr.CodeValidationFailed || len(appErr.Fields) != 1 || appErr.Fields[0].Field != "days" || appErr.Fields[0].Message != ErrCodeInvalidDays {
				t.Fatalf("Summary(days=%d) err = %v, want validation error %s on days", days, err, ErrCodeInvalidDays)
			}
		}
	})
}

func TestSummaryPropagatesStoreErrorsAsInternal(t *testing.T) {
	boom := errors.New("db unavailable")
	cases := []struct {
		name  string
		store *fakeStore
	}{
		{name: "read tx", store: &fakeStore{readTxErr: boom}},
		{name: "user stats", store: &fakeStore{userErr: boom}},
		{name: "role stats", store: &fakeStore{roleErr: boom}},
		{name: "file stats", store: &fakeStore{fileErr: boom}},
		{name: "sessions", store: &fakeStore{sessionsErr: boom}},
		{name: "trend", store: &fakeStore{trendErr: boom}},
		{name: "recent", store: &fakeStore{recentErr: boom}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newServiceWithStore(tc.store)
			days := 7
			_, err := svc.Summary(t.Context(), SummaryInput{Days: &days})
			appErr, ok := apperr.As(err)
			if !ok || appErr.Code != apperr.CodeInternal {
				t.Fatalf("Summary() err = %v, want INTERNAL_ERROR", err)
			}
		})
	}
}

func TestDashboardLocalesComplete(t *testing.T) {
	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle() error = %v", err)
	}
	if err := bundle.RegisterFS("dashboard", LocalesFS, "locales"); err != nil {
		t.Fatalf("RegisterFS(dashboard) error = %v", err)
	}
	if err := bundle.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	for _, lang := range []string{"zh-CN", "en-US"} {
		if got := bundle.Translate(lang, ErrCodeInvalidDays, nil); got == "" || got == ErrCodeInvalidDays {
			t.Fatalf("missing %s translation for %s", lang, ErrCodeInvalidDays)
		}
	}
}
