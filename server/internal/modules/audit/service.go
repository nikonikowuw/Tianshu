package audit

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/platform/logger"
	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/pagination"
)

// Service provides immutable audit log recording, filtered querying, and retention cleanup.
type Service struct {
	store          store
	log            *slog.Logger
	now            func() time.Time
	purgeBatchSize int32
}

// NewService creates a PostgreSQL-backed audit Service.
func NewService(pool *pgxpool.Pool, log *slog.Logger) *Service {
	return newServiceWithStore(newStore(pool), log)
}

func newServiceWithStore(s store, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		store:          s,
		log:            log,
		now:            time.Now,
		purgeBatchSize: defaultRetentionBatchSize,
	}
}

// Record appends an immutable audit log entry after redacting sensitive metadata keys.
// When ctx carries an active database transaction, the insert participates in that transaction.
func (s *Service) Record(ctx context.Context, input RecordInput) error {
	action := strings.TrimSpace(input.Action)
	resource := strings.TrimSpace(input.Resource)
	status := strings.TrimSpace(input.Status)

	var fields []apperr.FieldError
	if action == "" {
		fields = append(fields, apperr.FieldError{Field: "action", Message: ErrCodeInvalidAction})
	}
	if resource == "" {
		fields = append(fields, apperr.FieldError{Field: "resource", Message: ErrCodeInvalidResource})
	}
	if status != StatusSuccess && status != StatusFailure {
		fields = append(fields, apperr.FieldError{Field: "status", Message: ErrCodeInvalidStatus})
	}
	if len(fields) > 0 {
		return apperr.Validation(fields)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return apperr.Internal(fmt.Errorf("generate audit log uuid: %w", err))
	}

	requestID := strings.TrimSpace(input.RequestID)
	if requestID == "" {
		requestID = logger.RequestIDFromContext(ctx)
	}

	var actorID *uuid.UUID
	if input.ActorID != nil && *input.ActorID != uuid.Nil {
		copied := *input.ActorID
		actorID = &copied
	}

	entry := Entry{
		ID:            id,
		ActorID:       actorID,
		ActorUsername: strings.TrimSpace(input.ActorUsername),
		Action:        action,
		Resource:      resource,
		ResourceID:    strings.TrimSpace(input.ResourceID),
		Status:        status,
		IPAddress:     strings.TrimSpace(input.IPAddress),
		UserAgent:     truncateUA(input.UserAgent),
		RequestID:     requestID,
		Metadata:      RedactMetadata(input.Metadata),
	}

	if err := s.store.InsertAuditLog(ctx, entry); err != nil {
		return apperr.Internal(fmt.Errorf("insert audit log: %w", err))
	}
	return nil
}

// List returns paginated audit logs matching filter, sorted stably with a deterministic tie-breaker.
func (s *Service) List(ctx context.Context, filter ListFilter) (pagination.Result[Entry], error) {
	normalized, err := normalizeFilter(filter)
	if err != nil {
		return pagination.Result[Entry]{}, err
	}

	total, err := s.store.CountAuditLogs(ctx, normalized)
	if err != nil {
		return pagination.Result[Entry]{}, apperr.Internal(fmt.Errorf("count audit logs: %w", err))
	}
	if total == 0 {
		return pagination.NewResult([]Entry{}, 0, normalized.Pagination), nil
	}

	items, err := s.store.ListAuditLogs(ctx, normalized)
	if err != nil {
		return pagination.Result[Entry]{}, apperr.Internal(fmt.Errorf("list audit logs: %w", err))
	}
	return pagination.NewResult(items, total, normalized.Pagination), nil
}

// PurgeExpired deletes audit log rows older than retentionDays in bounded batches.
// Passing retentionDays <= 0 disables retention cleanup and returns 0.
func (s *Service) PurgeExpired(ctx context.Context, retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		return 0, nil
	}

	cutoff := s.now().UTC().AddDate(0, 0, -retentionDays)
	batchSize := s.purgeBatchSize
	if batchSize <= 0 {
		batchSize = defaultRetentionBatchSize
	}

	var totalDeleted int64
	for {
		if err := ctx.Err(); err != nil {
			return totalDeleted, err
		}
		deleted, err := s.store.DeleteExpiredBatch(ctx, cutoff, batchSize)
		if err != nil {
			return totalDeleted, fmt.Errorf("delete expired audit logs batch: %w", err)
		}
		totalDeleted += deleted
		if deleted < int64(batchSize) {
			return totalDeleted, nil
		}
	}
}

func normalizeFilter(filter ListFilter) (ListFilter, error) {
	out := filter
	out.Pagination = pagination.Normalize(filter.Pagination.Page, filter.Pagination.PageSize)
	out.Q = strings.TrimSpace(filter.Q)
	out.Actor = strings.TrimSpace(filter.Actor)

	sortCol := strings.TrimSpace(filter.Sort)
	if sortCol == "" {
		sortCol = SortCreatedAt
	}
	switch sortCol {
	case SortCreatedAt, SortAction, SortResource, SortStatus:
		out.Sort = sortCol
	default:
		return ListFilter{}, apperr.Validation([]apperr.FieldError{{
			Field:   "sort",
			Message: "VALIDATION_ONE_OF",
		}})
	}

	sortOrder := strings.ToLower(strings.TrimSpace(filter.Order))
	if sortOrder == "" {
		sortOrder = OrderDesc
	}
	switch sortOrder {
	case OrderAsc, OrderDesc:
		out.Order = sortOrder
	default:
		return ListFilter{}, apperr.Validation([]apperr.FieldError{{
			Field:   "order",
			Message: "VALIDATION_ONE_OF",
		}})
	}

	for _, st := range out.Statuses {
		if st != StatusSuccess && st != StatusFailure {
			return ListFilter{}, apperr.Validation([]apperr.FieldError{{
				Field:   "status",
				Message: ErrCodeInvalidStatus,
			}})
		}
	}

	if out.From != nil && out.To != nil && out.From.After(*out.To) {
		return ListFilter{}, apperr.Validation([]apperr.FieldError{{
			Field:   "from",
			Message: ErrCodeInvalidTimeRange,
		}})
	}

	return out, nil
}
