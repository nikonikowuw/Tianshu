package audit

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/modules/audit/internal/dbgen"
	"tianshu/internal/platform/database"
)

type store interface {
	InsertAuditLog(ctx context.Context, entry Entry) error
	CountAuditLogs(ctx context.Context, filter ListFilter) (int64, error)
	ListAuditLogs(ctx context.Context, filter ListFilter) ([]Entry, error)
	DeleteExpiredBatch(ctx context.Context, cutoff time.Time, limit int32) (int64, error)
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

func (s *pgStore) InsertAuditLog(ctx context.Context, entry Entry) error {
	metaBytes, err := json.Marshal(cloneMetadata(entry.Metadata))
	if err != nil {
		return err
	}

	var actorID uuid.NullUUID
	if entry.ActorID != nil && *entry.ActorID != uuid.Nil {
		actorID = uuid.NullUUID{UUID: *entry.ActorID, Valid: true}
	}

	return s.queries(ctx).InsertAuditLog(ctx, dbgen.InsertAuditLogParams{
		ID:            entry.ID,
		ActorID:       actorID,
		ActorUsername: entry.ActorUsername,
		Action:        entry.Action,
		Resource:      entry.Resource,
		ResourceID:    entry.ResourceID,
		Status:        entry.Status,
		IpAddress:     entry.IPAddress,
		UserAgent:     entry.UserAgent,
		RequestID:     entry.RequestID,
		Metadata:      metaBytes,
	})
}

func (s *pgStore) CountAuditLogs(ctx context.Context, filter ListFilter) (int64, error) {
	q := s.queries(ctx)
	if !hasSecondaryFilters(filter) {
		if filter.From == nil && filter.To == nil {
			return q.CountAuditLogsAll(ctx)
		}
		if filter.From != nil && filter.To != nil {
			return q.CountAuditLogsByTimeRange(ctx, dbgen.CountAuditLogsByTimeRangeParams{
				FromAt: pgtype.Timestamptz{Time: filter.From.UTC(), Valid: true},
				ToAt:   pgtype.Timestamptz{Time: filter.To.UTC(), Valid: true},
			})
		}
	}

	actorID, hasActorID := resolveActorUUID(filter.ActorID)
	fromAt, hasFrom := resolveTimestamp(filter.From)
	toAt, hasTo := resolveTimestamp(filter.To)

	return q.CountAuditLogs(ctx, dbgen.CountAuditLogsParams{
		Q:          escapeILIKE(filter.Q),
		Actor:      escapeILIKE(filter.Actor),
		HasActorID: hasActorID,
		ActorID:    actorID,
		Actions:    nonNilStrings(filter.Actions),
		Resources:  nonNilStrings(filter.Resources),
		Statuses:   nonNilStrings(filter.Statuses),
		HasFrom:    hasFrom,
		FromAt:     fromAt,
		HasTo:      hasTo,
		ToAt:       toAt,
	})
}

func (s *pgStore) ListAuditLogs(ctx context.Context, filter ListFilter) ([]Entry, error) {
	q := s.queries(ctx)
	if rows, handled, err := listAuditLogsHotPath(ctx, q, filter); handled {
		if err != nil {
			return nil, err
		}
		return mapAuditLogRows(rows), nil
	}

	actorID, hasActorID := resolveActorUUID(filter.ActorID)
	fromAt, hasFrom := resolveTimestamp(filter.From)
	toAt, hasTo := resolveTimestamp(filter.To)

	rows, err := q.ListAuditLogs(ctx, dbgen.ListAuditLogsParams{
		Q:          escapeILIKE(filter.Q),
		Actor:      escapeILIKE(filter.Actor),
		HasActorID: hasActorID,
		ActorID:    actorID,
		Actions:    nonNilStrings(filter.Actions),
		Resources:  nonNilStrings(filter.Resources),
		Statuses:   nonNilStrings(filter.Statuses),
		HasFrom:    hasFrom,
		FromAt:     fromAt,
		HasTo:      hasTo,
		ToAt:       toAt,
		SortBy:     filter.Sort,
		SortOrder:  filter.Order,
		PageLimit:  filter.Pagination.Limit(),
		PageOffset: filter.Pagination.Offset(),
	})
	if err != nil {
		return nil, err
	}

	return mapAuditLogRows(rows), nil
}

func listAuditLogsHotPath(ctx context.Context, q *dbgen.Queries, filter ListFilter) ([]dbgen.AuditLog, bool, error) {
	if hasSecondaryFilters(filter) || filter.Sort != SortCreatedAt {
		return nil, false, nil
	}
	limit := filter.Pagination.Limit()
	offset := filter.Pagination.Offset()

	if filter.From == nil && filter.To == nil {
		if filter.Order == OrderAsc {
			rows, err := q.ListAuditLogsOldest(ctx, dbgen.ListAuditLogsOldestParams{
				PageLimit:  limit,
				PageOffset: offset,
			})
			return rows, true, err
		}
		rows, err := q.ListAuditLogsRecent(ctx, dbgen.ListAuditLogsRecentParams{
			PageLimit:  limit,
			PageOffset: offset,
		})
		return rows, true, err
	}

	if filter.From != nil && filter.To != nil {
		fromTS := pgtype.Timestamptz{Time: filter.From.UTC(), Valid: true}
		toTS := pgtype.Timestamptz{Time: filter.To.UTC(), Valid: true}
		if filter.Order == OrderAsc {
			rows, err := q.ListAuditLogsByTimeRangeOldest(ctx, dbgen.ListAuditLogsByTimeRangeOldestParams{
				FromAt:     fromTS,
				ToAt:       toTS,
				PageLimit:  limit,
				PageOffset: offset,
			})
			return rows, true, err
		}
		rows, err := q.ListAuditLogsByTimeRangeRecent(ctx, dbgen.ListAuditLogsByTimeRangeRecentParams{
			FromAt:     fromTS,
			ToAt:       toTS,
			PageLimit:  limit,
			PageOffset: offset,
		})
		return rows, true, err
	}

	return nil, false, nil
}

func hasSecondaryFilters(filter ListFilter) bool {
	return filter.Q != "" ||
		filter.Actor != "" ||
		filter.ActorID != nil ||
		len(filter.Actions) > 0 ||
		len(filter.Resources) > 0 ||
		len(filter.Statuses) > 0
}

func mapAuditLogRows(rows []dbgen.AuditLog) []Entry {
	items := make([]Entry, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapAuditLogRow(row))
	}
	return items
}

func (s *pgStore) DeleteExpiredBatch(ctx context.Context, cutoff time.Time, limit int32) (int64, error) {
	return s.queries(ctx).DeleteExpiredAuditLogsBatch(ctx, dbgen.DeleteExpiredAuditLogsBatchParams{
		CreatedAt: pgtype.Timestamptz{Time: cutoff.UTC(), Valid: true},
		Limit:     limit,
	})
}

func mapAuditLogRow(row dbgen.AuditLog) Entry {
	var actorID *uuid.UUID
	if row.ActorID.Valid {
		id := row.ActorID.UUID
		actorID = &id
	}

	meta := make(map[string]any)
	if len(row.Metadata) > 0 {
		_ = json.Unmarshal(row.Metadata, &meta)
	}
	if meta == nil {
		meta = make(map[string]any)
	}

	return Entry{
		ID:            row.ID,
		ActorID:       actorID,
		ActorUsername: row.ActorUsername,
		Action:        row.Action,
		Resource:      row.Resource,
		ResourceID:    row.ResourceID,
		Status:        row.Status,
		IPAddress:     row.IpAddress,
		UserAgent:     row.UserAgent,
		RequestID:     row.RequestID,
		Metadata:      meta,
		CreatedAt:     row.CreatedAt.Time,
	}
}

var ilikeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func resolveActorUUID(id *uuid.UUID) (uuid.UUID, bool) {
	if id == nil {
		return uuid.Nil, false
	}
	return *id, true
}

func resolveTimestamp(t *time.Time) (pgtype.Timestamptz, bool) {
	if t == nil {
		return pgtype.Timestamptz{}, false
	}
	return pgtype.Timestamptz{Time: t.UTC(), Valid: true}, true
}

func nonNilStrings(in []string) []string {
	if len(in) == 0 {
		return make([]string, 0)
	}
	return in
}

func escapeILIKE(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	return ilikeEscaper.Replace(trimmed)
}
