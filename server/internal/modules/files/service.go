package files

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/platform/database"
	"tianshu/internal/platform/logger"
	"tianshu/internal/platform/storage"
	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/pagination"
)

const (
	defaultSignedURLPathPrefix = "/api/v1/files/signed/"

	defaultMaxFileSizeBytes     = int64(10 << 20)
	defaultMaxAvatarSizeBytes   = int64(2 << 20)
	defaultSignedURLTTL         = 5 * time.Minute
	defaultCleanupBatchSize     = int32(100)
	defaultPendingCleanupTTL    = 15 * time.Minute
	defaultDeletingCleanupGrace = time.Minute
	defaultCompensationTimeout  = 10 * time.Second

	auditResourceFile       = "file"
	auditActionUpload       = "file.upload"
	auditActionUploadAvatar = "file.upload_avatar"
	auditActionDelete       = "file.delete"
	auditStatusSuccess      = "success"
)

// ServiceConfig configures file size limits, extra allowed MIME types, and signed URL parameters.
type ServiceConfig struct {
	MaxFileSizeBytes    int64
	MaxAvatarSizeBytes  int64
	AllowedMIMETypes    []string
	SignedURLSecret     string
	SignedURLTTL        time.Duration
	SignedURLPathPrefix string
}

// ServiceDeps holds optional cross-cutting dependencies for files.Service.
type ServiceDeps struct {
	Tx           TxRunner
	Actors       ActorResolver
	AvatarRef    AvatarRefChecker
	AvatarCaller AvatarCallerValidator
	Audit        AuditRecorder
	Log          *slog.Logger
}

// Service orchestrates file validation, multi-driver storage persistence, signed URLs, and orphan cleanup.
type Service struct {
	store               store
	storage             *storage.Registry
	tx                  TxRunner
	actors              ActorResolver
	avatarRef           AvatarRefChecker
	avatarCaller        AvatarCallerValidator
	audit               AuditRecorder
	log                 *slog.Logger
	signer              *urlSigner
	signedURLPathPrefix string
	maxFileSizeBytes    int64
	maxAvatarSizeBytes  int64
	signedURLTTL        time.Duration
	allowedMIMETypes    map[string]mimeRule
	allowedExtensions   []string
	now                 func() time.Time
}

// NewService constructs a PostgreSQL-backed files Service.
func NewService(
	pool *pgxpool.Pool,
	reg *storage.Registry,
	cfg ServiceConfig,
	deps ...ServiceDeps,
) *Service {
	var d ServiceDeps
	if len(deps) > 0 {
		d = deps[0]
	}
	if pool != nil && d.Tx == nil {
		d.Tx = database.NewTxManager(pool)
	}
	return newServiceWithStore(newStore(pool), reg, cfg, d)
}

func newServiceWithStore(
	s store,
	reg *storage.Registry,
	cfg ServiceConfig,
	deps ServiceDeps,
) *Service {
	maxFile := cfg.MaxFileSizeBytes
	if maxFile <= 0 {
		maxFile = defaultMaxFileSizeBytes
	}
	maxAvatar := cfg.MaxAvatarSizeBytes
	if maxAvatar <= 0 {
		maxAvatar = defaultMaxAvatarSizeBytes
	}
	ttl := cfg.SignedURLTTL
	if ttl <= 0 {
		ttl = defaultSignedURLTTL
	}
	log := deps.Log
	if log == nil {
		log = slog.Default()
	}

	allowedMIMETypes := buildAllowedMIMEMap(cfg.AllowedMIMETypes)
	return &Service{
		store:               s,
		storage:             reg,
		tx:                  deps.Tx,
		actors:              deps.Actors,
		avatarRef:           deps.AvatarRef,
		avatarCaller:        deps.AvatarCaller,
		audit:               deps.Audit,
		log:                 log,
		signer:              newURLSigner(cfg.SignedURLSecret),
		signedURLPathPrefix: normalizeSignedURLPathPrefix(cfg.SignedURLPathPrefix),
		maxFileSizeBytes:    maxFile,
		maxAvatarSizeBytes:  maxAvatar,
		signedURLTTL:        ttl,
		allowedMIMETypes:    allowedMIMETypes,
		allowedExtensions:   buildAllowedExtensions(allowedMIMETypes),
		now:                 time.Now,
	}
}

func normalizeSignedURLPathPrefix(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return defaultSignedURLPathPrefix
	}
	if !strings.HasPrefix(trimmed, "/") {
		trimmed = "/" + trimmed
	}
	return strings.TrimRight(trimmed, "/") + "/"
}

// MaxFileSizeBytes returns the configured general upload byte limit.
func (s *Service) MaxFileSizeBytes() int64 {
	return s.maxFileSizeBytes
}

// MaxAvatarSizeBytes returns the configured avatar upload byte limit.
func (s *Service) MaxAvatarSizeBytes() int64 {
	return s.maxAvatarSizeBytes
}

// UploadPolicy returns the configured limits and accepted extensions for client pre-checks.
func (s *Service) UploadPolicy() UploadPolicy {
	return UploadPolicy{
		MaxFileSizeBytes:   s.maxFileSizeBytes,
		MaxAvatarSizeBytes: s.maxAvatarSizeBytes,
		AllowedExtensions:  slices.Clone(s.allowedExtensions),
	}
}

// Upload validates, sniffs, hashes, and persists a general file using a pending->ready two-phase write.
func (s *Service) Upload(ctx context.Context, input UploadInput) (File, error) {
	prepared, err := prepareGeneralUpload(input.Filename, input.Reader, s.maxFileSizeBytes, s.allowedMIMETypes)
	if err != nil {
		var appErr *apperr.Error
		if errors.As(err, &appErr) {
			return File{}, appErr
		}
		return File{}, apperr.Internal(err)
	}
	defer prepared.Close()

	return s.persistPreparedUpload(ctx, prepared, "", auditActionUpload, input.UploaderID, input.Meta)
}

// UploadAvatar validates an image, strips EXIF via pure-Go re-encoding (<=512px PNG), and stores it.
func (s *Service) UploadAvatar(ctx context.Context, input UploadInput) (File, error) {
	if input.UploaderID == nil || *input.UploaderID == uuid.Nil {
		return File{}, apperr.Unauthorized(apperr.CodeUnauthorized)
	}
	if s.avatarCaller != nil {
		if err := s.avatarCaller.EnsureActiveAvatarCaller(ctx, *input.UploaderID, input.SessionID); err != nil {
			var appErr *apperr.Error
			if errors.As(err, &appErr) {
				return File{}, appErr
			}
			return File{}, apperr.Internal(err)
		}
	}

	prepared, err := prepareAvatarUpload(input.Filename, input.Reader, s.maxAvatarSizeBytes)
	if err != nil {
		var appErr *apperr.Error
		if errors.As(err, &appErr) {
			return File{}, appErr
		}
		return File{}, apperr.Internal(err)
	}
	defer prepared.Close()

	return s.persistPreparedUpload(ctx, prepared, "avatars", auditActionUploadAvatar, input.UploaderID, input.Meta)
}

// ValidateAndLockAvatarFile acquires a FOR SHARE row lock on fileID within the caller's
// transaction and verifies that it is a ready avatar image uploaded by ownerID.
func (s *Service) ValidateAndLockAvatarFile(ctx context.Context, fileID, ownerID uuid.UUID) error {
	if fileID == uuid.Nil || ownerID == uuid.Nil {
		return errFileNotFound()
	}
	rec, err := s.store.GetFileByIDForShare(ctx, fileID)
	if errors.Is(err, errRecordNotFound) {
		return errFileNotFound()
	}
	if err != nil {
		return apperr.Internal(fmt.Errorf("lock avatar file %s: %w", fileID, err))
	}
	if rec.Status != StatusReady ||
		!isAllowedImageMIME(rec.MIMEType) ||
		!strings.HasPrefix(rec.ObjectKey, "avatars/") ||
		rec.UploaderID == nil ||
		*rec.UploaderID != ownerID {
		return errFileNotFound()
	}
	return nil
}

// SignAvatarURL mints a short-lived inline signed URL path for a ready avatar file.
func (s *Service) SignAvatarURL(ctx context.Context, fileID uuid.UUID) (string, error) {
	if fileID == uuid.Nil {
		return "", nil
	}
	res, err := s.CreateSignedURL(ctx, fileID, DispositionInline)
	if err != nil {
		return "", err
	}
	return res.URL, nil
}

func (s *Service) persistPreparedUpload(
	ctx context.Context,
	prepared *preparedUpload,
	keyPrefix string,
	auditAction string,
	uploaderID *uuid.UUID,
	meta RequestMeta,
) (File, error) {
	if s.storage == nil || s.storage.Default() == nil {
		return File{}, errStorageUnavailable(errors.New("default storage driver is nil"))
	}

	id, err := uuid.NewV7()
	if err != nil {
		return File{}, apperr.Internal(fmt.Errorf("generate file uuid: %w", err))
	}

	datePrefix := s.now().UTC().Format("2006/01")
	objectKey := datePrefix + "/" + id.String() + prepared.ext
	if keyPrefix != "" {
		objectKey = keyPrefix + "/" + objectKey
	}

	activeStore := s.storage.Default()
	pending := File{
		ID:            id,
		UploaderID:    cloneUUIDPtr(uploaderID),
		StorageDriver: activeStore.Driver(),
		ObjectKey:     objectKey,
		OriginalName:  prepared.originalName,
		MIMEType:      prepared.mimeType,
		SizeBytes:     prepared.sizeBytes,
		SHA256:        prepared.sha256,
		Status:        StatusPending,
	}

	// Phase 1: persist pending metadata row before writing binary bytes so failed writes stay trackable.
	if _, err := s.store.InsertPendingFile(ctx, pending); err != nil {
		return File{}, apperr.Internal(fmt.Errorf("insert pending file: %w", err))
	}

	// Phase 2: write binary payload to the active storage driver.
	if _, err := activeStore.Put(ctx, objectKey, prepared.file); err != nil {
		s.compensateFailedUpload(ctx, activeStore, id, objectKey)
		return File{}, storageOperationError("put file object", err, false)
	}

	// Phase 3: promote status to ready and record audit atomically.
	var ready File
	actorUsername := s.resolveUsername(ctx, uploaderID)
	err = s.runInTx(ctx, func(txCtx context.Context) error {
		var markErr error
		ready, markErr = s.store.MarkFileReady(txCtx, id)
		if markErr != nil {
			return fmt.Errorf("mark file ready: %w", markErr)
		}
		if s.audit != nil {
			if auditErr := s.audit.Record(txCtx, AuditEntry{
				ActorID:       cloneUUIDPtr(uploaderID),
				ActorUsername: actorUsername,
				Action:        auditAction,
				Resource:      auditResourceFile,
				ResourceID:    id.String(),
				Status:        auditStatusSuccess,
				IPAddress:     meta.IPAddress,
				UserAgent:     meta.UserAgent,
				Metadata: map[string]any{
					"originalName":  ready.OriginalName,
					"mimeType":      ready.MIMEType,
					"sizeBytes":     ready.SizeBytes,
					"storageDriver": ready.StorageDriver,
					"sha256":        ready.SHA256,
				},
			}); auditErr != nil {
				return fmt.Errorf("record file upload audit: %w", auditErr)
			}
		}
		return nil
	})
	if err != nil {
		s.compensateFailedUpload(ctx, activeStore, id, objectKey)
		return File{}, apperr.Internal(err)
	}

	if uploaderID != nil && *uploaderID != uuid.Nil {
		ready.Uploader = &UploaderRef{
			ID:       *uploaderID,
			Username: actorUsername,
		}
	}
	return ready, nil
}

func (s *Service) compensateFailedUpload(ctx context.Context, drv storage.Storage, fileID uuid.UUID, objectKey string) {
	compCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultCompensationTimeout)
	defer cancel()

	log := s.ctxLog(ctx)
	if err := drv.Delete(compCtx, objectKey); err != nil {
		log.Warn("failed to delete orphan object during upload compensation",
			"file_id", fileID.String(),
			"storage_driver", drv.Driver(),
			"object_key", objectKey,
			"error", err,
		)
	}
	if err := s.store.MarkFileStatus(compCtx, fileID, StatusFailed); err != nil {
		log.Warn("failed to mark file status failed during upload compensation",
			"file_id", fileID.String(),
			"error", err,
		)
	}
}

// List returns a paginated slice of ready files with uploader projections hydrated in a single batch.
func (s *Service) List(ctx context.Context, filter ListFilter) (pagination.Result[File], error) {
	normalized, err := normalizeListFilter(filter)
	if err != nil {
		return pagination.Result[File]{}, err
	}

	total, err := s.store.CountReadyFiles(ctx, normalized)
	if err != nil {
		return pagination.Result[File]{}, apperr.Internal(fmt.Errorf("count files: %w", err))
	}
	if total == 0 {
		return pagination.NewResult([]File{}, 0, normalized.Pagination), nil
	}

	items, err := s.store.ListReadyFiles(ctx, normalized)
	if err != nil {
		return pagination.Result[File]{}, apperr.Internal(fmt.Errorf("list files: %w", err))
	}

	s.hydrateUploaders(ctx, items)
	return pagination.NewResult(items, total, normalized.Pagination), nil
}

// Get returns metadata for a single ready file by ID.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (File, error) {
	rec, err := s.store.GetReadyFileByID(ctx, id)
	if errors.Is(err, errRecordNotFound) {
		return File{}, errFileNotFound()
	}
	if err != nil {
		return File{}, apperr.Internal(fmt.Errorf("get file: %w", err))
	}
	items := []File{rec}
	s.hydrateUploaders(ctx, items)
	return items[0], nil
}

// OpenContent opens the stored content stream for a ready file, routing by its persisted storage_driver.
func (s *Service) OpenContent(ctx context.Context, id uuid.UUID, forceDownload bool) (ContentStream, error) {
	rec, err := s.store.GetReadyFileByID(ctx, id)
	if errors.Is(err, errRecordNotFound) {
		return ContentStream{}, errFileNotFound()
	}
	if err != nil {
		return ContentStream{}, apperr.Internal(fmt.Errorf("get file for content: %w", err))
	}

	requested := DispositionInline
	if forceDownload {
		requested = DispositionAttachment
	}
	return s.openFileStream(ctx, rec, requested)
}

// CreateSignedURL mints a short-lived HMAC-signed URL path for safe browser previews and downloads.
func (s *Service) CreateSignedURL(ctx context.Context, id uuid.UUID, requestedDisposition string) (SignedURLResult, error) {
	disp := strings.ToLower(strings.TrimSpace(requestedDisposition))
	if disp != "" && disp != DispositionInline && disp != DispositionAttachment {
		return SignedURLResult{}, apperr.Validation([]apperr.FieldError{{
			Field:   "disposition",
			Message: ErrCodeInvalidDisposition,
		}})
	}

	rec, err := s.store.GetReadyFileByID(ctx, id)
	if errors.Is(err, errRecordNotFound) {
		return SignedURLResult{}, errFileNotFound()
	}
	if err != nil {
		return SignedURLResult{}, apperr.Internal(fmt.Errorf("get file for signed url: %w", err))
	}

	effective := effectiveDisposition(rec.MIMEType, disp)
	expiresAt := s.now().UTC().Add(s.signedURLTTL)
	token, err := s.signer.Sign(rec.ID, effective, expiresAt)
	if err != nil {
		return SignedURLResult{}, apperr.Internal(fmt.Errorf("sign file url: %w", err))
	}
	return SignedURLResult{
		URL:       s.signedURLPathPrefix + token,
		ExpiresAt: expiresAt,
	}, nil
}

// OpenSignedContent verifies a short-lived signed token and streams the underlying file content.
func (s *Service) OpenSignedContent(ctx context.Context, token string) (ContentStream, error) {
	fileID, disposition, err := s.signer.Verify(token, s.now())
	if err != nil {
		return ContentStream{}, err
	}

	rec, err := s.store.GetReadyFileByID(ctx, fileID)
	if errors.Is(err, errRecordNotFound) {
		return ContentStream{}, errFileNotFound()
	}
	if err != nil {
		return ContentStream{}, apperr.Internal(fmt.Errorf("get signed file: %w", err))
	}
	return s.openFileStream(ctx, rec, disposition)
}

func (s *Service) openFileStream(ctx context.Context, rec File, requestedDisposition string) (ContentStream, error) {
	if s.storage == nil {
		return ContentStream{}, errStorageUnavailable(errors.New("storage registry is nil"))
	}
	drv, err := s.storage.Driver(rec.StorageDriver)
	if err != nil {
		return ContentStream{}, errStorageUnavailable(err)
	}

	rc, err := drv.Get(ctx, rec.ObjectKey)
	if err != nil {
		return ContentStream{}, storageOperationError("open storage object", err, true)
	}

	return ContentStream{
		File:        rec,
		Reader:      rc,
		Disposition: effectiveDisposition(rec.MIMEType, requestedDisposition),
	}, nil
}

// storageOperationError classifies driver failures so infrastructure outages surface as the
// documented 503 FILES_STORAGE_UNAVAILABLE instead of an opaque internal error.
func storageOperationError(operation string, err error, notFoundAs404 bool) *apperr.Error {
	wrapped := fmt.Errorf("%s: %w", operation, err)
	switch {
	case notFoundAs404 && errors.Is(err, storage.ErrNotFound):
		return errFileNotFound()
	case errors.Is(err, storage.ErrInvalidKey):
		return apperr.Internal(wrapped)
	case errors.Is(err, context.Canceled):
		return apperr.Internal(wrapped)
	default:
		return errStorageUnavailable(wrapped)
	}
}

// Delete removes a file after verifying no active user avatar references it.
// Metadata transitions to "deleting" in a transaction with an audit entry first; physical storage
// deletion runs immediately after commit, and any storage failure is retried by CleanupOrphans.
func (s *Service) Delete(ctx context.Context, input DeleteInput) error {
	var target File
	actorUsername := s.resolveUsername(ctx, input.ActorID)

	err := s.runInTx(ctx, func(txCtx context.Context) error {
		rec, lockErr := s.store.GetFileByIDForUpdate(txCtx, input.FileID)
		if errors.Is(lockErr, errRecordNotFound) || (lockErr == nil && rec.Status != StatusReady) {
			return errFileNotFound()
		}
		if lockErr != nil {
			return apperr.Internal(fmt.Errorf("lock file for delete: %w", lockErr))
		}

		if s.avatarRef != nil {
			inUse, refErr := s.avatarRef.IsAvatarFileInUse(txCtx, rec.ID)
			if refErr != nil {
				return apperr.Internal(fmt.Errorf("check avatar reference: %w", refErr))
			}
			if inUse {
				return errFileInUse()
			}
		}

		if markErr := s.store.MarkFileStatus(txCtx, rec.ID, StatusDeleting); markErr != nil {
			return apperr.Internal(fmt.Errorf("mark file deleting: %w", markErr))
		}

		if s.audit != nil {
			if auditErr := s.audit.Record(txCtx, AuditEntry{
				ActorID:       cloneUUIDPtr(input.ActorID),
				ActorUsername: actorUsername,
				Action:        auditActionDelete,
				Resource:      auditResourceFile,
				ResourceID:    rec.ID.String(),
				Status:        auditStatusSuccess,
				IPAddress:     input.Meta.IPAddress,
				UserAgent:     input.Meta.UserAgent,
				Metadata: map[string]any{
					"originalName":  rec.OriginalName,
					"mimeType":      rec.MIMEType,
					"sizeBytes":     rec.SizeBytes,
					"storageDriver": rec.StorageDriver,
				},
			}); auditErr != nil {
				return apperr.Internal(fmt.Errorf("record file delete audit: %w", auditErr))
			}
		}

		target = rec
		return nil
	})
	if err != nil {
		var appErr *apperr.Error
		if errors.As(err, &appErr) {
			return appErr
		}
		return apperr.Internal(err)
	}

	if s.storage == nil {
		return nil
	}
	log := s.ctxLog(ctx)
	drv, err := s.storage.Driver(target.StorageDriver)
	if err != nil {
		log.Warn("storage driver unavailable during delete; deferred to orphan janitor",
			"file_id", target.ID.String(),
			"storage_driver", target.StorageDriver,
			"error", err,
		)
		return nil
	}

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultCompensationTimeout)
	defer cancel()

	if err := drv.Delete(cleanupCtx, target.ObjectKey); err != nil {
		log.Warn("storage delete failed; deferred to orphan janitor",
			"file_id", target.ID.String(),
			"storage_driver", target.StorageDriver,
			"object_key", target.ObjectKey,
			"error", err,
		)
		return nil
	}

	if _, err := s.store.DeleteFileRow(cleanupCtx, target.ID); err != nil {
		log.Warn("failed to delete finalized file row after object removal; deferred to janitor",
			"file_id", target.ID.String(),
			"error", err,
		)
	}
	return nil
}

// CleanupOrphans deletes expired pending uploads, failed rows, and pending-delete objects in bounded batches.
func (s *Service) CleanupOrphans(ctx context.Context, pendingTTL time.Duration, batchLimit int32) (int64, error) {
	if s.storage == nil {
		return 0, nil
	}
	if pendingTTL <= 0 {
		pendingTTL = defaultPendingCleanupTTL
	}
	if batchLimit <= 0 {
		batchLimit = defaultCleanupBatchSize
	}

	driverMap := s.storage.Drivers()
	driverNames := make([]string, 0, len(driverMap))
	for name := range driverMap {
		driverNames = append(driverNames, name)
	}
	if len(driverNames) == 0 {
		return 0, nil
	}

	now := s.now().UTC()
	pendingCutoff := now.Add(-pendingTTL)
	deletingCutoff := now.Add(-defaultDeletingCleanupGrace)
	candidates, err := s.store.ListCleanupCandidates(ctx, driverNames, pendingCutoff, deletingCutoff, batchLimit)
	if err != nil {
		return 0, apperr.Internal(fmt.Errorf("list file cleanup candidates: %w", err))
	}

	log := s.ctxLog(ctx)
	var cleaned int64
	for _, rec := range candidates {
		if err := ctx.Err(); err != nil {
			return cleaned, apperr.Internal(err)
		}

		// Atomically claim expired pending rows before touching storage to prevent TOCTOU deletion
		// of an upload that transitioned to ready after ListCleanupCandidates returned.
		if rec.Status == StatusPending {
			claimed, claimErr := s.store.ClaimExpiredPendingFile(ctx, rec.ID, pendingCutoff)
			if claimErr != nil {
				log.Warn("failed to claim expired pending file",
					"file_id", rec.ID.String(),
					"error", claimErr,
				)
				continue
			}
			if !claimed {
				continue
			}
		}

		drv, err := s.storage.Driver(rec.StorageDriver)
		if err != nil {
			log.Warn("skipping orphan cleanup for unconfigured storage driver",
				"file_id", rec.ID.String(),
				"storage_driver", rec.StorageDriver,
			)
			continue
		}
		if err := drv.Delete(ctx, rec.ObjectKey); err != nil {
			log.Warn("failed to delete orphan storage object",
				"file_id", rec.ID.String(),
				"storage_driver", rec.StorageDriver,
				"object_key", rec.ObjectKey,
				"error", err,
			)
			// Push failing candidates to the back of the queue to avoid head-of-line blocking.
			if touchErr := s.store.TouchCleanupCandidate(ctx, rec.ID); touchErr != nil {
				log.Warn("failed to touch cleanup candidate after storage delete failure",
					"file_id", rec.ID.String(),
					"error", touchErr,
				)
			}
			continue
		}
		deleted, err := s.store.DeleteFileRow(ctx, rec.ID)
		if err != nil {
			log.Warn("failed to delete orphan file row",
				"file_id", rec.ID.String(),
				"error", err,
			)
			if touchErr := s.store.TouchCleanupCandidate(ctx, rec.ID); touchErr != nil {
				log.Warn("failed to touch cleanup candidate after db delete failure",
					"file_id", rec.ID.String(),
					"error", touchErr,
				)
			}
			continue
		}
		if deleted {
			cleaned++
		}
	}
	return cleaned, nil
}

func (s *Service) ctxLog(ctx context.Context) *slog.Logger {
	return logger.FromContext(ctx, s.log)
}

func (s *Service) runInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if s.tx != nil {
		return s.tx.WithinTx(ctx, fn)
	}
	return fn(ctx)
}

func (s *Service) resolveUsername(ctx context.Context, userID *uuid.UUID) string {
	if s.actors == nil || userID == nil || *userID == uuid.Nil {
		return ""
	}
	username, err := s.actors.GetUsername(ctx, *userID)
	if err != nil {
		s.ctxLog(ctx).Warn("failed to resolve actor username for file audit",
			"user_id", userID.String(),
			"error", err,
		)
		return ""
	}
	return username
}

func (s *Service) hydrateUploaders(ctx context.Context, items []File) {
	if s.actors == nil || len(items) == 0 {
		return
	}
	idSet := make(map[uuid.UUID]struct{}, len(items))
	ids := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		if item.UploaderID == nil || *item.UploaderID == uuid.Nil {
			continue
		}
		uid := *item.UploaderID
		if _, exists := idSet[uid]; !exists {
			idSet[uid] = struct{}{}
			ids = append(ids, uid)
		}
	}
	if len(ids) == 0 {
		return
	}

	names, err := s.actors.ListUsernamesByIDs(ctx, ids)
	if err != nil {
		s.ctxLog(ctx).Warn("failed to resolve file uploader usernames", "error", err)
		return
	}
	for i := range items {
		if items[i].UploaderID == nil || *items[i].UploaderID == uuid.Nil {
			continue
		}
		uid := *items[i].UploaderID
		items[i].Uploader = &UploaderRef{
			ID:       uid,
			Username: names[uid],
		}
	}
}

func effectiveDisposition(mimeType, requested string) string {
	// Documents and any non-safe-image MIME types are unconditionally forced to download as attachments.
	if !isAllowedImageMIME(mimeType) {
		return DispositionAttachment
	}
	if requested == DispositionAttachment {
		return DispositionAttachment
	}
	return DispositionInline
}

func normalizeListFilter(filter ListFilter) (ListFilter, error) {
	out := filter
	out.Pagination = pagination.Normalize(filter.Pagination.Page, filter.Pagination.PageSize)

	normalized, fieldErrs := validateAndNormalizeFilterFields(out)
	if len(fieldErrs) > 0 {
		return ListFilter{}, apperr.Validation(fieldErrs)
	}
	return normalized, nil
}

func cloneUUIDPtr(id *uuid.UUID) *uuid.UUID {
	if id == nil || *id == uuid.Nil {
		return nil
	}
	copied := *id
	return &copied
}
