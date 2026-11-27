package files_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/modules/files"
	"tianshu/internal/platform/config"
	"tianshu/internal/platform/database"
	"tianshu/internal/platform/logger"
	"tianshu/internal/platform/storage"
	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/pagination"
	"tianshu/migrations"
)

type pgTestActors struct {
	pool *pgxpool.Pool
}

func (a pgTestActors) GetUsername(ctx context.Context, userID uuid.UUID) (string, error) {
	var username string
	err := database.Executor(ctx, a.pool).QueryRow(
		ctx,
		`SELECT username FROM users WHERE id = $1 AND deleted_at IS NULL`,
		userID,
	).Scan(&username)
	return username, err
}

func (a pgTestActors) ListUsernamesByIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]string, error) {
	rows, err := database.Executor(ctx, a.pool).Query(
		ctx,
		`SELECT id, username FROM users WHERE id = ANY($1::uuid[])`,
		userIDs,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[uuid.UUID]string, len(userIDs))
	for rows.Next() {
		var id uuid.UUID
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

type pgTestAvatarRef struct {
	pool *pgxpool.Pool
}

func (r pgTestAvatarRef) IsAvatarFileInUse(ctx context.Context, fileID uuid.UUID) (bool, error) {
	var inUse bool
	err := database.Executor(ctx, r.pool).QueryRow(
		ctx,
		`SELECT EXISTS (SELECT 1 FROM users WHERE avatar_file_id = $1 AND deleted_at IS NULL)`,
		fileID,
	).Scan(&inUse)
	return inUse, err
}

type pgTestAudit struct {
	pool *pgxpool.Pool
	mu   sync.Mutex
	fail error
}

func (a *pgTestAudit) setFail(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.fail = err
}

func (a *pgTestAudit) Record(ctx context.Context, entry files.AuditEntry) error {
	a.mu.Lock()
	fail := a.fail
	a.mu.Unlock()
	if fail != nil {
		return fail
	}
	metaJSON, err := json.Marshal(entry.Metadata)
	if err != nil {
		return err
	}
	id := uuid.Must(uuid.NewV7())
	_, err = database.Executor(ctx, a.pool).Exec(ctx, `
		INSERT INTO audit_logs (id, actor_id, actor_username, action, resource, resource_id, status, ip_address, user_agent, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, now())
	`, id, entry.ActorID, entry.ActorUsername, entry.Action, entry.Resource, entry.ResourceID, entry.Status, entry.IPAddress, entry.UserAgent, string(metaJSON))
	return err
}

type pgFlakyStorage struct {
	storage.Storage
	mu            sync.Mutex
	failDeleteKey string
	failDeleteErr error
}

func (s *pgFlakyStorage) setDeleteFailure(key string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failDeleteKey = key
	s.failDeleteErr = err
}

func (s *pgFlakyStorage) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	failKey, failErr := s.failDeleteKey, s.failDeleteErr
	s.mu.Unlock()
	if failErr != nil && (failKey == "" || failKey == key) {
		return failErr
	}
	return s.Storage.Delete(ctx, key)
}

func seedTestUser(t *testing.T, pool *pgxpool.Pool, username, email string) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	_, err := pool.Exec(t.Context(), `
		INSERT INTO users (id, username, email, display_name, password_hash, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'test-hash', 'active', now(), now())
	`, id, username, email, username)
	if err != nil {
		t.Fatalf("seed user %s: %v", username, err)
	}
	return id
}

func makeTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 30, G: 90, B: 180, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func openIsolatedTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn, dsnExplicit := os.LookupEnv("TIANSHU_TEST_DSN")
	if !dsnExplicit {
		dsn = config.Default().Database.DSN
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	adminPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		if dsnExplicit {
			t.Fatalf("postgres unavailable: %v", err)
		}
		t.Skipf("postgres unavailable: %v", err)
	}
	if err := adminPool.Ping(ctx); err != nil {
		adminPool.Close()
		if dsnExplicit {
			t.Fatalf("postgres unreachable: %v", err)
		}
		t.Skipf("postgres unreachable: %v", err)
	}

	schema := "test_files_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		adminPool.Close()
		t.Fatalf("create test schema %s: %v", schema, err)
	}
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dropCancel()
		_, _ = adminPool.Exec(dropCtx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
		adminPool.Close()
	})

	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	poolCfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		t.Fatalf("open schema pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := database.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate test schema: %v", err)
	}
	return pool
}

func TestFilesPostgresLifecycleFiltersRollbackConcurrencyAndOrphanCleanup(t *testing.T) {
	pool := openIsolatedTestPool(t)
	ctx := t.Context()

	log := logger.New("test", "error", io.Discard)
	adminID := seedTestUser(t, pool, "filesadmin", "filesadmin@example.com")

	localStore, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	flakyStore := &pgFlakyStorage{Storage: localStore}
	reg, err := storage.NewRegistry(storage.DriverLocal, flakyStore)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	aud := &pgTestAudit{pool: pool}
	// Intentionally omit Tx to verify NewService defaults to database.NewTxManager(pool).
	fileSvc := files.NewService(pool, reg, files.ServiceConfig{
		SignedURLSecret: "integration-test-secret-at-least-32-bytes",
		SignedURLTTL:    5 * time.Minute,
	}, files.ServiceDeps{
		Actors:    pgTestActors{pool: pool},
		AvatarRef: pgTestAvatarRef{pool: pool},
		Audit:     aud,
		Log:       log,
	})

	// 1) Upload files including ILIKE wildcard characters (%, _) and an image.
	txtRec, err := fileSvc.Upload(ctx, files.UploadInput{
		Filename:   "100%_raw_note.txt",
		Reader:     strings.NewReader("hello postgres files"),
		UploaderID: &adminID,
	})
	if err != nil {
		t.Fatalf("Upload txt: %v", err)
	}
	pdfRec, err := fileSvc.Upload(ctx, files.UploadInput{
		Filename:   "invoice.pdf",
		Reader:     bytes.NewReader([]byte("%PDF-1.4\n12345678901234567890\n%%EOF\n")),
		UploaderID: &adminID,
	})
	if err != nil {
		t.Fatalf("Upload pdf: %v", err)
	}
	imgRec, err := fileSvc.Upload(ctx, files.UploadInput{
		Filename:   "banner.png",
		Reader:     bytes.NewReader(makeTestPNG(t, 24, 24)),
		UploaderID: &adminID,
	})
	if err != nil {
		t.Fatalf("Upload png: %v", err)
	}

	// Verify Q search escapes %, _, and \ so wildcards match literally without matching invoice.pdf or causing SQL 22025.
	for _, needle := range []string{"100%_raw", "%", "_"} {
		searchRes, err := fileSvc.List(ctx, files.ListFilter{
			Pagination: pagination.Normalize(1, 20),
			Q:          needle,
		})
		if err != nil {
			t.Fatalf("List(Q=%q): %v", needle, err)
		}
		if searchRes.Total != 1 || len(searchRes.Items) != 1 || searchRes.Items[0].ID != txtRec.ID {
			t.Fatalf("ILIKE escape mismatch for %q: got total=%d items=%+v", needle, searchRes.Total, searchRes.Items)
		}
	}
	backslashRes, err := fileSvc.List(ctx, files.ListFilter{
		Pagination: pagination.Normalize(1, 20),
		Q:          `\`,
	})
	if err != nil || backslashRes.Total != 0 {
		t.Fatalf("List(Q=backslash) = (%+v, %v), want total=0 and no SQL error", backslashRes, err)
	}

	// Verify category, mimeType, driver, sort (size_bytes DESC & original_name ASC), and pagination offset.
	imgList, err := fileSvc.List(ctx, files.ListFilter{
		Pagination: pagination.Normalize(1, 10),
		Categories: []string{files.CategoryImage},
		MIMETypes:  []string{"image/png"},
		Drivers:    []string{storage.DriverLocal},
	})
	if err != nil || imgList.Total != 1 || imgList.Items[0].ID != imgRec.ID {
		t.Fatalf("image+mime filter = (%+v, %v), want imgRec", imgList, err)
	}

	sortedBySize, err := fileSvc.List(ctx, files.ListFilter{
		Pagination: pagination.Normalize(1, 10),
		Sort:       files.SortSizeBytes,
		Order:      files.OrderDesc,
	})
	if err != nil || sortedBySize.Total != 3 {
		t.Fatalf("List sort size_bytes DESC = (%+v, %v)", sortedBySize, err)
	}
	if sortedBySize.Items[0].SizeBytes < sortedBySize.Items[1].SizeBytes || sortedBySize.Items[1].SizeBytes < sortedBySize.Items[2].SizeBytes {
		t.Fatalf("items not sorted by size_bytes DESC: %+v", sortedBySize.Items)
	}

	sortedDocs, err := fileSvc.List(ctx, files.ListFilter{
		Pagination: pagination.Normalize(1, 10),
		Categories: []string{files.CategoryDocument},
		Drivers:    []string{storage.DriverLocal},
		Sort:       files.SortOriginalName,
		Order:      files.OrderAsc,
	})
	if err != nil || sortedDocs.Total != 2 || sortedDocs.Items[0].ID != txtRec.ID || sortedDocs.Items[0].Uploader == nil || sortedDocs.Items[0].Uploader.Username != "filesadmin" {
		t.Fatalf("unexpected sorted docs: %+v, err=%v", sortedDocs, err)
	}

	page2, err := fileSvc.List(ctx, files.ListFilter{
		Pagination: pagination.Normalize(2, 10),
		Sort:       files.SortCreatedAt,
		Order:      files.OrderDesc,
	})
	if err != nil || page2.Total != 3 || len(page2.Items) != 0 {
		t.Fatalf("page 2 offset result = (%+v, %v), want total=3 len=0", page2, err)
	}

	// 2) Real PostgreSQL transaction rollback on Upload & Delete audit failure.
	aud.setFail(errors.New("simulated audit failure"))
	if _, err := fileSvc.Upload(ctx, files.UploadInput{
		Filename:   "rollback.txt",
		Reader:     strings.NewReader("rollback-body"),
		UploaderID: &adminID,
	}); err == nil {
		t.Fatal("expected Upload to fail when audit fails")
	}
	var failedCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM files WHERE original_name = 'rollback.txt' AND status = 'failed'`).Scan(&failedCount); err != nil || failedCount != 1 {
		t.Fatalf("expected rollback.txt status='failed' in PG, got (%d, %v)", failedCount, err)
	}

	if err := fileSvc.Delete(ctx, files.DeleteInput{FileID: txtRec.ID, ActorID: &adminID}); err == nil {
		t.Fatal("expected Delete to fail when audit fails")
	}
	if stillReady, err := fileSvc.Get(ctx, txtRec.ID); err != nil || stillReady.Status != files.StatusReady {
		t.Fatalf("expected txtRec rolled back to ready in PG, got (%+v, %v)", stillReady, err)
	}
	if _, err := localStore.Stat(ctx, txtRec.ObjectKey); err != nil {
		t.Fatalf("expected txtRec storage object intact after rolled-back Delete: %v", err)
	}
	aud.setFail(nil)

	// 3) Concurrent ValidateAndLockAvatarFile (FOR SHARE) vs Delete (FOR UPDATE) serialization in real PG.
	avatarRec, err := fileSvc.UploadAvatar(ctx, files.UploadInput{
		Filename:   "me.png",
		Reader:     bytes.NewReader(makeTestPNG(t, 64, 64)),
		UploaderID: &adminID,
	})
	if err != nil {
		t.Fatalf("UploadAvatar: %v", err)
	}

	txMgr := database.NewTxManager(pool)
	lockedCh := make(chan struct{})
	releaseCh := make(chan struct{})
	var bindErr, delErr error
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		bindErr = txMgr.WithinTx(ctx, func(txCtx context.Context) error {
			if err := fileSvc.ValidateAndLockAvatarFile(txCtx, avatarRec.ID, adminID); err != nil {
				return err
			}
			close(lockedCh)
			<-releaseCh
			_, err := database.Executor(txCtx, pool).Exec(txCtx, `UPDATE users SET avatar_file_id = $1 WHERE id = $2`, avatarRec.ID, adminID)
			return err
		})
	}()

	go func() {
		defer wg.Done()
		<-lockedCh
		delErr = fileSvc.Delete(ctx, files.DeleteInput{FileID: avatarRec.ID, ActorID: &adminID})
	}()

	// Wait deterministically until PostgreSQL reports the FOR UPDATE lock waiter behind FOR SHARE.
	<-lockedCh
	waitTicker := time.NewTicker(5 * time.Millisecond)
	defer waitTicker.Stop()
	waitDeadline := time.NewTimer(3 * time.Second)
	defer waitDeadline.Stop()
waitForLock:
	for {
		var waiting bool
		if qErr := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_locks WHERE NOT granted)`).Scan(&waiting); qErr == nil && waiting {
			break waitForLock
		}
		select {
		case <-waitDeadline.C:
			break waitForLock
		case <-waitTicker.C:
		}
	}
	close(releaseCh)

	wg.Wait()
	if bindErr != nil {
		t.Fatalf("avatar bind tx failed: %v", bindErr)
	}
	if appErr, ok := apperr.As(delErr); !ok || appErr.Code != files.ErrCodeInUse {
		t.Fatalf("concurrent Delete after FOR SHARE avatar bind = %v, want 409 %s", delErr, files.ErrCodeInUse)
	}

	// Unbind avatar and delete cleanly.
	if _, err := pool.Exec(ctx, `UPDATE users SET avatar_file_id = NULL WHERE id = $1`, adminID); err != nil {
		t.Fatalf("clear avatar_file_id: %v", err)
	}
	if err := fileSvc.Delete(ctx, files.DeleteInput{FileID: avatarRec.ID, ActorID: &adminID}); err != nil {
		t.Fatalf("Delete unreferenced avatar: %v", err)
	}

	// 4) Foreign key ON DELETE SET NULL on files.uploader_id when uploader user is physically deleted.
	tempUserID := seedTestUser(t, pool, "tempuser", "tempuser@example.com")
	tempFile, err := fileSvc.Upload(ctx, files.UploadInput{
		Filename:   "orphan-owner.txt",
		Reader:     strings.NewReader("owned"),
		UploaderID: &tempUserID,
	})
	if err != nil {
		t.Fatalf("Upload tempFile: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, tempUserID); err != nil {
		t.Fatalf("delete temp user: %v", err)
	}
	gotTemp, err := fileSvc.Get(ctx, tempFile.ID)
	if err != nil || gotTemp.UploaderID != nil {
		t.Fatalf("expected ON DELETE SET NULL on files.uploader_id, got (%+v, %v)", gotTemp, err)
	}

	// 5) Real PG CleanupOrphans across all statuses, grace periods, multi-driver filter, and TouchCleanupCandidate backoff.
	insertRaw := func(driver, status string, age time.Duration, putObject bool) (uuid.UUID, string) {
		id := uuid.Must(uuid.NewV7())
		key := "2026/11/" + id.String() + ".txt"
		if putObject {
			if _, err := localStore.Put(ctx, key, strings.NewReader("data")); err != nil {
				t.Fatalf("put %s: %v", key, err)
			}
		}
		ts := time.Now().UTC().Add(-age)
		if _, err := pool.Exec(ctx, `
			INSERT INTO files (id, uploader_id, storage_driver, object_key, original_name, mime_type, size_bytes, sha256, status, created_at, updated_at)
			VALUES ($1, $2, $3, $4, 'janitor.txt', 'text/plain', 4, 'abc', $5, $6, $6)
		`, id, adminID, driver, key, status, ts); err != nil {
			t.Fatalf("insert raw %s/%s: %v", driver, status, err)
		}
		return id, key
	}

	expiredPendingID, _ := insertRaw(storage.DriverLocal, files.StatusPending, 2*time.Hour, true)
	freshPendingID, _ := insertRaw(storage.DriverLocal, files.StatusPending, time.Minute, true)
	agedDeletingID, _ := insertRaw(storage.DriverLocal, files.StatusDeleting, 5*time.Minute, true)
	freshDeletingID, _ := insertRaw(storage.DriverLocal, files.StatusDeleting, 5*time.Second, true)
	s3FailedID, _ := insertRaw(storage.DriverS3, files.StatusFailed, 2*time.Hour, false)
	backoffFailedID, backoffKey := insertRaw(storage.DriverLocal, files.StatusFailed, 3*time.Hour, true)

	// Fail deletion of backoffKey once to verify TouchCleanupCandidate updates updated_at without blocking other rows.
	flakyStore.setDeleteFailure(backoffKey, errors.New("temporary fs error"))
	var beforeTouch time.Time
	if err := pool.QueryRow(ctx, `SELECT updated_at FROM files WHERE id = $1`, backoffFailedID).Scan(&beforeTouch); err != nil {
		t.Fatalf("read beforeTouch: %v", err)
	}

	// Eligible for local driver: rollback.txt (failed), expiredPendingID (pending > 15m), agedDeletingID (deleting > 1m).
	// Excluded: freshPendingID, freshDeletingID, s3FailedID (unconfigured driver), backoffFailedID (storage err -> touched).
	cleaned, err := fileSvc.CleanupOrphans(ctx, 15*time.Minute, 50)
	if err != nil || cleaned != 3 {
		t.Fatalf("CleanupOrphans = (%d, %v), want (3, nil)", cleaned, err)
	}

	var afterTouch time.Time
	if err := pool.QueryRow(ctx, `SELECT updated_at FROM files WHERE id = $1`, backoffFailedID).Scan(&afterTouch); err != nil {
		t.Fatalf("read afterTouch: %v", err)
	}
	if !afterTouch.After(beforeTouch) {
		t.Fatalf("expected TouchCleanupCandidate to advance updated_at: before=%v after=%v", beforeTouch, afterTouch)
	}

	// Verify surviving rows vs deleted rows.
	for _, shouldBeGone := range []uuid.UUID{expiredPendingID, agedDeletingID} {
		var cnt int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM files WHERE id = $1`, shouldBeGone).Scan(&cnt); err != nil || cnt != 0 {
			t.Fatalf("expected %s deleted, count=(%d, %v)", shouldBeGone, cnt, err)
		}
	}
	for _, shouldRemain := range []uuid.UUID{freshPendingID, freshDeletingID, s3FailedID, backoffFailedID, pdfRec.ID} {
		var cnt int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM files WHERE id = $1`, shouldRemain).Scan(&cnt); err != nil || cnt != 1 {
			t.Fatalf("expected %s intact, count=(%d, %v)", shouldRemain, cnt, err)
		}
	}

	// Recover storage and confirm backoffFailedID cleans up on next tick while ready pdfRec stays intact.
	flakyStore.setDeleteFailure("", nil)
	if _, err := pool.Exec(ctx, `UPDATE files SET updated_at = now() - interval '3 hours' WHERE id = $1`, pdfRec.ID); err != nil {
		t.Fatalf("backdate pdfRec: %v", err)
	}
	cleaned2, err := fileSvc.CleanupOrphans(ctx, 15*time.Minute, 50)
	if err != nil || cleaned2 != 1 {
		t.Fatalf("CleanupOrphans second pass = (%d, %v), want (1, nil)", cleaned2, err)
	}
	if gotPDF, err := fileSvc.Get(ctx, pdfRec.ID); err != nil || gotPDF.ID != pdfRec.ID {
		t.Fatalf("expected pdfRec intact, got (%+v, %v)", gotPDF, err)
	}
}
