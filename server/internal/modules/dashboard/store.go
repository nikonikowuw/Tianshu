package dashboard

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/modules/dashboard/internal/dbgen"
	"tianshu/internal/platform/database"
)

type store interface {
	WithinReadTx(ctx context.Context, fn func(ctx context.Context, txStore store) error) error
	GetUserStats(ctx context.Context, since time.Time) (UserStats, error)
	GetRoleStats(ctx context.Context) (RoleStats, error)
	GetFileStats(ctx context.Context) (FileStats, error)
	CountActiveSessions(ctx context.Context, now time.Time) (int64, error)
	ListDailyAuditActivity(ctx context.Context, startAt, endAt time.Time) ([]DailyActivityPoint, error)
	ListRecentAuditActivity(ctx context.Context, limit int32) ([]RecentActivityItem, error)
}

var _ store = (*pgStore)(nil)

type pgStore struct {
	pool *pgxpool.Pool
	tx   pgx.Tx
}

func newStore(pool *pgxpool.Pool) *pgStore {
	return &pgStore{pool: pool}
}

func (s *pgStore) queries(ctx context.Context) *dbgen.Queries {
	if s.tx != nil {
		return dbgen.New(s.tx)
	}
	return dbgen.New(database.Executor(ctx, s.pool))
}

// WithinReadTx runs fn inside a REPEATABLE READ READ ONLY transaction so all dashboard
// queries observe a consistent point-in-time snapshot without acquiring row locks.
func (s *pgStore) WithinReadTx(ctx context.Context, fn func(ctx context.Context, txStore store) error) error {
	if s.tx != nil || s.pool == nil {
		return fn(ctx, s)
	}
	if _, ok := database.TxFromContext(ctx); ok {
		return fn(ctx, s)
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return fmt.Errorf("begin read-only transaction: %w", err)
	}

	committed := false
	defer func() {
		if committed {
			return
		}
		rbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rbCtx)
	}()

	txStore := &pgStore{pool: s.pool, tx: tx}
	if err := fn(ctx, txStore); err != nil {
		rbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		rbErr := tx.Rollback(rbCtx)
		cancel()
		if rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			return fmt.Errorf("rollback read-only transaction after %w: %v", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit read-only transaction: %w", err)
	}
	committed = true
	return nil
}

func (s *pgStore) GetUserStats(ctx context.Context, since time.Time) (UserStats, error) {
	row, err := s.queries(ctx).GetUserStats(ctx, pgtype.Timestamptz{
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
	row, err := s.queries(ctx).GetRoleStats(ctx)
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
	row, err := s.queries(ctx).GetFileStats(ctx)
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
	return s.queries(ctx).CountActiveSessions(ctx, pgtype.Timestamptz{
		Time:  now.UTC(),
		Valid: true,
	})
}

func (s *pgStore) ListDailyAuditActivity(ctx context.Context, startAt, endAt time.Time) ([]DailyActivityPoint, error) {
	rows, err := s.queries(ctx).ListDailyAuditActivity(ctx, dbgen.ListDailyAuditActivityParams{
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
	rows, err := s.queries(ctx).ListRecentAuditActivity(ctx, limit)
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
