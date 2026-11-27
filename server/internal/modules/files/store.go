package files

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/modules/files/internal/dbgen"
	"tianshu/internal/platform/database"
)

var errRecordNotFound = errors.New("file record not found")

type store interface {
	InsertPendingFile(ctx context.Context, rec File) (File, error)
	MarkFileReady(ctx context.Context, id uuid.UUID) (File, error)
	MarkFileStatus(ctx context.Context, id uuid.UUID, status string) error
	GetReadyFileByID(ctx context.Context, id uuid.UUID) (File, error)
	GetFileByIDForUpdate(ctx context.Context, id uuid.UUID) (File, error)
	ClaimExpiredPendingFile(ctx context.Context, id uuid.UUID, pendingCutoff time.Time) (bool, error)
	TouchCleanupCandidate(ctx context.Context, id uuid.UUID) error
	DeleteFileRow(ctx context.Context, id uuid.UUID) error
	CountReadyFiles(ctx context.Context, filter ListFilter) (int64, error)
	ListReadyFiles(ctx context.Context, filter ListFilter) ([]File, error)
	ListCleanupCandidates(ctx context.Context, drivers []string, pendingCutoff time.Time, limit int32) ([]File, error)
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

func (s *pgStore) InsertPendingFile(ctx context.Context, rec File) (File, error) {
	row, err := s.queries(ctx).InsertPendingFile(ctx, dbgen.InsertPendingFileParams{
		ID:            rec.ID,
		UploaderID:    ptrToNullUUID(rec.UploaderID),
		StorageDriver: rec.StorageDriver,
		ObjectKey:     rec.ObjectKey,
		OriginalName:  rec.OriginalName,
		MimeType:      rec.MIMEType,
		SizeBytes:     rec.SizeBytes,
		Sha256:        rec.SHA256,
	})
	if err != nil {
		return File{}, err
	}
	return mapFileRow(row), nil
}

func (s *pgStore) MarkFileReady(ctx context.Context, id uuid.UUID) (File, error) {
	row, err := s.queries(ctx).MarkFileReady(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return File{}, errRecordNotFound
	}
	if err != nil {
		return File{}, err
	}
	return mapFileRow(row), nil
}

func (s *pgStore) MarkFileStatus(ctx context.Context, id uuid.UUID, status string) error {
	return s.queries(ctx).MarkFileStatus(ctx, dbgen.MarkFileStatusParams{
		ID:     id,
		Status: status,
	})
}

func (s *pgStore) GetReadyFileByID(ctx context.Context, id uuid.UUID) (File, error) {
	row, err := s.queries(ctx).GetReadyFileByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return File{}, errRecordNotFound
	}
	if err != nil {
		return File{}, err
	}
	return mapFileRow(row), nil
}

func (s *pgStore) GetFileByIDForUpdate(ctx context.Context, id uuid.UUID) (File, error) {
	row, err := s.queries(ctx).GetFileByIDForUpdate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return File{}, errRecordNotFound
	}
	if err != nil {
		return File{}, err
	}
	return mapFileRow(row), nil
}

func (s *pgStore) ClaimExpiredPendingFile(ctx context.Context, id uuid.UUID, pendingCutoff time.Time) (bool, error) {
	rows, err := s.queries(ctx).ClaimExpiredPendingFile(ctx, dbgen.ClaimExpiredPendingFileParams{
		ID:            id,
		PendingCutoff: pgtype.Timestamptz{Time: pendingCutoff.UTC(), Valid: true},
	})
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (s *pgStore) TouchCleanupCandidate(ctx context.Context, id uuid.UUID) error {
	return s.queries(ctx).TouchCleanupCandidate(ctx, id)
}

func (s *pgStore) DeleteFileRow(ctx context.Context, id uuid.UUID) error {
	return s.queries(ctx).DeleteFileRow(ctx, id)
}

func (s *pgStore) CountReadyFiles(ctx context.Context, filter ListFilter) (int64, error) {
	return s.queries(ctx).CountReadyFiles(ctx, dbgen.CountReadyFilesParams{
		Q:          escapeILIKE(filter.Q),
		MimeTypes:  nonNilStrings(filter.MIMETypes),
		Drivers:    nonNilStrings(filter.Drivers),
		Categories: nonNilStrings(filter.Categories),
	})
}

func (s *pgStore) ListReadyFiles(ctx context.Context, filter ListFilter) ([]File, error) {
	rows, err := s.queries(ctx).ListReadyFiles(ctx, dbgen.ListReadyFilesParams{
		Q:          escapeILIKE(filter.Q),
		MimeTypes:  nonNilStrings(filter.MIMETypes),
		Drivers:    nonNilStrings(filter.Drivers),
		Categories: nonNilStrings(filter.Categories),
		SortBy:     filter.Sort,
		SortOrder:  filter.Order,
		PageOffset: filter.Pagination.Offset(),
		PageLimit:  filter.Pagination.Limit(),
	})
	if err != nil {
		return nil, err
	}
	out := make([]File, 0, len(rows))
	for _, r := range rows {
		out = append(out, mapFileRow(r))
	}
	return out, nil
}

func (s *pgStore) ListCleanupCandidates(ctx context.Context, drivers []string, pendingCutoff time.Time, limit int32) ([]File, error) {
	rows, err := s.queries(ctx).ListCleanupCandidates(ctx, dbgen.ListCleanupCandidatesParams{
		Drivers:       nonNilStrings(drivers),
		PendingCutoff: pgtype.Timestamptz{Time: pendingCutoff.UTC(), Valid: true},
		BatchLimit:    limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]File, 0, len(rows))
	for _, r := range rows {
		out = append(out, mapFileRow(r))
	}
	return out, nil
}

func mapFileRow(r dbgen.File) File {
	var uploaderID *uuid.UUID
	if r.UploaderID.Valid {
		uid := r.UploaderID.UUID
		uploaderID = &uid
	}
	return File{
		ID:            r.ID,
		UploaderID:    uploaderID,
		StorageDriver: r.StorageDriver,
		ObjectKey:     r.ObjectKey,
		OriginalName:  r.OriginalName,
		MIMEType:      r.MimeType,
		SizeBytes:     r.SizeBytes,
		SHA256:        r.Sha256,
		Status:        r.Status,
		CreatedAt:     r.CreatedAt.Time.UTC(),
		UpdatedAt:     r.UpdatedAt.Time.UTC(),
	}
}

func ptrToNullUUID(val *uuid.UUID) uuid.NullUUID {
	if val == nil || *val == uuid.Nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *val, Valid: true}
}

var ilikeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func escapeILIKE(s string) string {
	return ilikeEscaper.Replace(s)
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return make([]string, 0)
	}
	return in
}
