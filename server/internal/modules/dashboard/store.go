package dashboard

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/modules/dashboard/internal/dbgen"
)

type store interface {
	GetUserStats(ctx context.Context, since time.Time) (UserStats, error)
	GetRoleStats(ctx context.Context) (RoleStats, error)
	GetFileStats(ctx context.Context) (FileStats, error)
	CountActiveSessions(ctx context.Context, now time.Time) (int64, error)
	ListDailyAuditActivity(ctx context.Context, startAt, endAt time.Time) ([]DailyActivityPoint, error)
	ListRecentAuditActivity(ctx context.Context, limit int32) ([]RecentActivityItem, error)
}

type pgStore struct {
	pool *pgxpool.Pool
}

func newStore(pool *pgxpool.Pool) store {
	return &pgStore{pool: pool}
}

func (s *pgStore) queries() *dbgen.Queries {
	return dbgen.New(s.pool)
}

func (s *pgStore) GetUserStats(ctx context.Context, since time.Time) (UserStats, error) {
	row, err := s.queries().GetUserStats(ctx, pgtype.Timestamptz{
		Time:  since.UTC(),
		Valid: true,
	})
	if err != nil {
		return UserStats{}, err
	}
	return UserStats{
		Total:        row.TotalUsers,
		Active:       row.ActiveUsers,
		Disabled:     row.DisabledUsers,
		NewLast7Days: row.NewUsersLast7Days,
	}, nil
}

func (s *pgStore) GetRoleStats(ctx context.Context) (RoleStats, error) {
	row, err := s.queries().GetRoleStats(ctx)
	if err != nil {
		return RoleStats{}, err
	}
	return RoleStats{
		Total:  row.TotalRoles,
		System: row.SystemRoles,
		Custom: row.CustomRoles,
	}, nil
}

func (s *pgStore) GetFileStats(ctx context.Context) (FileStats, error) {
	row, err := s.queries().GetFileStats(ctx)
	if err != nil {
		return FileStats{}, err
	}
	return FileStats{
		Total:          row.TotalFiles,
		TotalSizeBytes: row.TotalSizeBytes,
		Images:         row.ImageFiles,
		Documents:      row.DocumentFiles,
	}, nil
}

func (s *pgStore) CountActiveSessions(ctx context.Context, now time.Time) (int64, error) {
	return s.queries().CountActiveSessions(ctx, pgtype.Timestamptz{
		Time:  now.UTC(),
		Valid: true,
	})
}

func (s *pgStore) ListDailyAuditActivity(ctx context.Context, startAt, endAt time.Time) ([]DailyActivityPoint, error) {
	rows, err := s.queries().ListDailyAuditActivity(ctx, dbgen.ListDailyAuditActivityParams{
		StartAt: pgtype.Timestamptz{Time: startAt.UTC(), Valid: true},
		EndAt:   pgtype.Timestamptz{Time: endAt.UTC(), Valid: true},
	})
	if err != nil {
		return nil, err
	}
	items := make([]DailyActivityPoint, 0, len(rows))
	for _, row := range rows {
		items = append(items, DailyActivityPoint{
			Date:       row.Day,
			Logins:     row.Logins,
			Operations: row.Operations,
			Failures:   row.Failures,
		})
	}
	return items, nil
}

func (s *pgStore) ListRecentAuditActivity(ctx context.Context, limit int32) ([]RecentActivityItem, error) {
	rows, err := s.queries().ListRecentAuditActivity(ctx, limit)
	if err != nil {
		return nil, err
	}
	items := make([]RecentActivityItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, RecentActivityItem{
			ID:            row.ID,
			ActorUsername: row.ActorUsername,
			Action:        row.Action,
			Resource:      row.Resource,
			ResourceID:    row.ResourceID,
			Status:        row.Status,
			IPAddress:     row.IpAddress,
			CreatedAt:     row.CreatedAt.Time.UTC(),
		})
	}
	return items, nil
}
