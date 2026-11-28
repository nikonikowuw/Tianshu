package dashboard

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/shared/apperr"
)

const (
	defaultRangeDays   = 7
	defaultRecentLimit = int32(8)
	utcDateLayout      = "2006-01-02"
)

// Service computes read-only dashboard overview metrics from PostgreSQL.
type Service struct {
	store       store
	now         func() time.Time
	recentLimit int32
}

// NewService creates a dashboard Service backed by PostgreSQL.
func NewService(pool *pgxpool.Pool) *Service {
	return newServiceWithStore(newStore(pool))
}

func newServiceWithStore(st store) *Service {
	return &Service{
		store:       st,
		now:         time.Now,
		recentLimit: defaultRecentLimit,
	}
}

// Summary aggregates user, role, file, session, trend, and recent activity metrics.
func (s *Service) Summary(ctx context.Context, input SummaryInput) (Summary, error) {
	days := defaultRangeDays
	if input.Days != nil {
		days = *input.Days
	}
	if !isAllowedRangeDays(days) {
		return Summary{}, invalidDaysError()
	}

	now := s.now().UTC()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	rangeStart := todayStart.AddDate(0, 0, -(days - 1))
	rangeEnd := todayStart.AddDate(0, 0, 1)
	newUsersSince := now.AddDate(0, 0, -defaultRangeDays)

	var (
		userStats      UserStats
		roleStats      RoleStats
		fileStats      FileStats
		activeSessions int64
		rawTrend       []DailyActivityPoint
		recent         []RecentActivityItem
	)

	err := s.store.WithinReadTx(ctx, func(ctx context.Context, st store) error {
		var err error
		userStats, err = st.GetUserStats(ctx, newUsersSince)
		if err != nil {
			return apperr.Internal(fmt.Errorf("load dashboard user stats: %w", err))
		}

		roleStats, err = st.GetRoleStats(ctx)
		if err != nil {
			return apperr.Internal(fmt.Errorf("load dashboard role stats: %w", err))
		}

		fileStats, err = st.GetFileStats(ctx)
		if err != nil {
			return apperr.Internal(fmt.Errorf("load dashboard file stats: %w", err))
		}

		activeSessions, err = st.CountActiveSessions(ctx, now)
		if err != nil {
			return apperr.Internal(fmt.Errorf("load dashboard active sessions: %w", err))
		}

		rawTrend, err = st.ListDailyAuditActivity(ctx, rangeStart, rangeEnd)
		if err != nil {
			return apperr.Internal(fmt.Errorf("load dashboard activity trend: %w", err))
		}

		recent, err = st.ListRecentAuditActivity(ctx, s.recentLimit)
		if err != nil {
			return apperr.Internal(fmt.Errorf("load dashboard recent activity: %w", err))
		}
		return nil
	})
	if err != nil {
		if appErr, ok := apperr.As(err); ok {
			return Summary{}, appErr
		}
		return Summary{}, apperr.Internal(fmt.Errorf("load dashboard summary snapshot: %w", err))
	}
	if recent == nil {
		recent = []RecentActivityItem{}
	}

	trend, auditStats := buildContinuousTrend(rangeStart, days, rawTrend)

	return Summary{
		GeneratedAt:    now,
		RangeDays:      days,
		Users:          userStats,
		Roles:          roleStats,
		Files:          fileStats,
		Sessions:       SessionStats{Active: activeSessions},
		Audit:          auditStats,
		ActivityTrend:  trend,
		RecentActivity: recent,
	}, nil
}

func isAllowedRangeDays(days int) bool {
	return days == 7 || days == 14 || days == 30
}

func buildContinuousTrend(
	rangeStart time.Time,
	days int,
	raw []DailyActivityPoint,
) ([]DailyActivityPoint, AuditStats) {
	byDate := make(map[string]DailyActivityPoint, len(raw))
	for _, point := range raw {
		byDate[point.Date] = point
	}

	trend := make([]DailyActivityPoint, 0, days)
	var stats AuditStats
	for i := range days {
		dateKey := rangeStart.AddDate(0, 0, i).Format(utcDateLayout)
		point := byDate[dateKey]
		point.Date = dateKey
		trend = append(trend, point)

		stats.LoginsInRange += point.Logins
		stats.FailuresInRange += point.Failures
		stats.TotalInRange += point.Logins + point.Operations + point.Failures
	}
	return trend, stats
}
