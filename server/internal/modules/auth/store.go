package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/modules/auth/internal/dbgen"
	"tianshu/internal/platform/database"
)

type revokedSession struct {
	ID     uuid.UUID
	UserID uuid.UUID
}

type store interface {
	CreateSession(ctx context.Context, rec sessionRecord) error
	GetSessionByRefreshHashForUpdate(ctx context.Context, tokenHash string) (sessionRecord, error)
	GetSessionByIDForUpdate(ctx context.Context, sessionID uuid.UUID) (sessionRecord, error)
	RotateSessionRefreshToken(ctx context.Context, sessionID uuid.UUID, newHash, ua, ip string, expiresAt time.Time) error
	InsertRefreshTokenHistory(ctx context.Context, tokenHash string, sessionID uuid.UUID, expiresAt time.Time) error
	GetRefreshTokenHistoryByHash(ctx context.Context, tokenHash string) (refreshHistoryRecord, error)
	RevokeSession(ctx context.Context, sessionID uuid.UUID) (revokedSession, bool, error)
	RevokeSessionByRefreshHash(ctx context.Context, tokenHash string) (revokedSession, bool, error)
	RevokeAllUserSessions(ctx context.Context, userID uuid.UUID) error
	ListActiveUserSessions(ctx context.Context, userID uuid.UUID) ([]ActiveSession, error)
	IsUserSessionActive(ctx context.Context, userID, sessionID uuid.UUID) (bool, error)
	LockActiveUserSessionForShare(ctx context.Context, userID, sessionID uuid.UUID) (bool, error)
	RevokeUserSessionByID(ctx context.Context, userID, sessionID uuid.UUID) (bool, error)
	RevokeOtherUserSessions(ctx context.Context, userID, keepSessionID uuid.UUID) (int64, error)
	DeleteExpiredRefreshTokenHistoryBatch(ctx context.Context, cutoff time.Time, limit int32) (int64, error)
	DeleteExpiredUserSessionsBatch(ctx context.Context, cutoff time.Time, limit int32) (int64, error)
}

type pgStore struct {
	pool *pgxpool.Pool
}

var _ store = (*pgStore)(nil)

func newStore(pool *pgxpool.Pool) *pgStore {
	return &pgStore{pool: pool}
}

func (s *pgStore) queries(ctx context.Context) *dbgen.Queries {
	return dbgen.New(database.Executor(ctx, s.pool))
}

func (s *pgStore) CreateSession(ctx context.Context, rec sessionRecord) error {
	return s.queries(ctx).CreateSession(ctx, dbgen.CreateSessionParams{
		ID:               rec.ID,
		UserID:           rec.UserID,
		RefreshTokenHash: rec.RefreshTokenHash,
		UserAgent:        rec.UserAgent,
		IpAddress:        rec.IPAddress,
		ExpiresAt:        pgtype.Timestamptz{Time: rec.ExpiresAt.UTC(), Valid: true},
	})
}

func (s *pgStore) GetSessionByRefreshHashForUpdate(ctx context.Context, tokenHash string) (sessionRecord, error) {
	row, err := s.queries(ctx).GetSessionByRefreshHashForUpdate(ctx, tokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return sessionRecord{}, errSessionNotFound
	}
	if err != nil {
		return sessionRecord{}, err
	}
	return mapSessionRow(row), nil
}

func (s *pgStore) GetSessionByIDForUpdate(ctx context.Context, sessionID uuid.UUID) (sessionRecord, error) {
	row, err := s.queries(ctx).GetSessionByIDForUpdate(ctx, sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return sessionRecord{}, errSessionNotFound
	}
	if err != nil {
		return sessionRecord{}, err
	}
	return mapSessionRow(row), nil
}

func (s *pgStore) RotateSessionRefreshToken(
	ctx context.Context,
	sessionID uuid.UUID,
	newHash, ua, ip string,
	expiresAt time.Time,
) error {
	return s.queries(ctx).RotateSessionRefreshToken(ctx, dbgen.RotateSessionRefreshTokenParams{
		ID:               sessionID,
		RefreshTokenHash: newHash,
		UserAgent:        ua,
		IpAddress:        ip,
		ExpiresAt:        pgtype.Timestamptz{Time: expiresAt.UTC(), Valid: true},
	})
}

func (s *pgStore) InsertRefreshTokenHistory(
	ctx context.Context,
	tokenHash string,
	sessionID uuid.UUID,
	expiresAt time.Time,
) error {
	return s.queries(ctx).InsertRefreshTokenHistory(ctx, dbgen.InsertRefreshTokenHistoryParams{
		TokenHash: tokenHash,
		SessionID: sessionID,
		ExpiresAt: pgtype.Timestamptz{Time: expiresAt.UTC(), Valid: true},
	})
}

func (s *pgStore) GetRefreshTokenHistoryByHash(ctx context.Context, tokenHash string) (refreshHistoryRecord, error) {
	row, err := s.queries(ctx).GetRefreshTokenHistoryByHash(ctx, tokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return refreshHistoryRecord{}, errHistoryNotFound
	}
	if err != nil {
		return refreshHistoryRecord{}, err
	}
	return refreshHistoryRecord{
		TokenHash:  row.TokenHash,
		SessionID:  row.SessionID,
		ReplacedAt: row.ReplacedAt.Time,
		ExpiresAt:  row.ExpiresAt.Time,
	}, nil
}

func (s *pgStore) RevokeSession(ctx context.Context, sessionID uuid.UUID) (revokedSession, bool, error) {
	row, err := s.queries(ctx).RevokeSession(ctx, sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return revokedSession{}, false, nil
	}
	if err != nil {
		return revokedSession{}, false, err
	}
	return revokedSession{ID: row.ID, UserID: row.UserID}, true, nil
}

func (s *pgStore) RevokeSessionByRefreshHash(ctx context.Context, tokenHash string) (revokedSession, bool, error) {
	row, err := s.queries(ctx).RevokeSessionByRefreshHash(ctx, tokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return revokedSession{}, false, nil
	}
	if err != nil {
		return revokedSession{}, false, err
	}
	return revokedSession{ID: row.ID, UserID: row.UserID}, true, nil
}

func (s *pgStore) RevokeAllUserSessions(ctx context.Context, userID uuid.UUID) error {
	return s.queries(ctx).RevokeAllUserSessions(ctx, userID)
}

func (s *pgStore) ListActiveUserSessions(ctx context.Context, userID uuid.UUID) ([]ActiveSession, error) {
	rows, err := s.queries(ctx).ListActiveUserSessions(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]ActiveSession, 0, len(rows))
	for _, row := range rows {
		out = append(out, ActiveSession{
			ID:         row.ID,
			IPAddress:  row.IpAddress,
			UserAgent:  row.UserAgent,
			LastUsedAt: row.LastUsedAt.Time.UTC(),
			CreatedAt:  row.CreatedAt.Time.UTC(),
			ExpiresAt:  row.ExpiresAt.Time.UTC(),
		})
	}
	return out, nil
}

func (s *pgStore) IsUserSessionActive(ctx context.Context, userID, sessionID uuid.UUID) (bool, error) {
	return s.queries(ctx).IsUserSessionActive(ctx, dbgen.IsUserSessionActiveParams{
		ID:     sessionID,
		UserID: userID,
	})
}

func (s *pgStore) LockActiveUserSessionForShare(ctx context.Context, userID, sessionID uuid.UUID) (bool, error) {
	_, err := s.queries(ctx).LockActiveUserSessionForShare(ctx, dbgen.LockActiveUserSessionForShareParams{
		ID:     sessionID,
		UserID: userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *pgStore) RevokeUserSessionByID(ctx context.Context, userID, sessionID uuid.UUID) (bool, error) {
	affected, err := s.queries(ctx).RevokeUserSessionByID(ctx, dbgen.RevokeUserSessionByIDParams{
		ID:     sessionID,
		UserID: userID,
	})
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func (s *pgStore) RevokeOtherUserSessions(ctx context.Context, userID, keepSessionID uuid.UUID) (int64, error) {
	return s.queries(ctx).RevokeOtherUserSessions(ctx, dbgen.RevokeOtherUserSessionsParams{
		UserID: userID,
		ID:     keepSessionID,
	})
}

func (s *pgStore) DeleteExpiredRefreshTokenHistoryBatch(ctx context.Context, cutoff time.Time, limit int32) (int64, error) {
	return s.queries(ctx).DeleteExpiredRefreshTokenHistoryBatch(ctx, dbgen.DeleteExpiredRefreshTokenHistoryBatchParams{
		ExpiresAt: pgtype.Timestamptz{Time: cutoff.UTC(), Valid: true},
		Limit:     limit,
	})
}

func (s *pgStore) DeleteExpiredUserSessionsBatch(ctx context.Context, cutoff time.Time, limit int32) (int64, error) {
	return s.queries(ctx).DeleteExpiredUserSessionsBatch(ctx, dbgen.DeleteExpiredUserSessionsBatchParams{
		ExpiresAt: pgtype.Timestamptz{Time: cutoff.UTC(), Valid: true},
		Limit:     limit,
	})
}

func mapSessionRow(row dbgen.UserSession) sessionRecord {
	var revokedAt *time.Time
	if row.RevokedAt.Valid {
		t := row.RevokedAt.Time
		revokedAt = &t
	}
	return sessionRecord{
		ID:               row.ID,
		UserID:           row.UserID,
		RefreshTokenHash: row.RefreshTokenHash,
		UserAgent:        row.UserAgent,
		IPAddress:        row.IpAddress,
		ExpiresAt:        row.ExpiresAt.Time,
		RevokedAt:        revokedAt,
		LastUsedAt:       row.LastUsedAt.Time,
		CreatedAt:        row.CreatedAt.Time,
	}
}
