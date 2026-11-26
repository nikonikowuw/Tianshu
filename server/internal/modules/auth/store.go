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
