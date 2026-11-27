package files_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/modules/audit"
	"tianshu/internal/modules/files"
	"tianshu/internal/modules/identity"
	"tianshu/internal/platform/config"
	"tianshu/internal/platform/database"
	"tianshu/internal/platform/logger"
	"tianshu/internal/platform/security"
	"tianshu/internal/platform/storage"
	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/pagination"
	"tianshu/migrations"
)

type identityAdapter struct {
	svc *identity.Service
}

func (a *identityAdapter) GetUsername(ctx context.Context, userID uuid.UUID) (string, error) {
	u, err := a.svc.GetByID(ctx, userID)
	if err != nil {
		return "", err
	}
	return u.Username, nil
}

func (a *identityAdapter) ListUsernamesByIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]string, error) {
	return a.svc.ListUsernamesByIDs(ctx, userIDs)
}

func (a *identityAdapter) IsAvatarFileInUse(ctx context.Context, fileID uuid.UUID) (bool, error) {
	return a.svc.IsAvatarFileInUse(ctx, fileID)
}

type auditAdapter struct {
	svc *audit.Service
}

func (a auditAdapter) Record(ctx context.Context, entry files.AuditEntry) error {
	return a.svc.Record(ctx, audit.RecordInput{
		ActorID:       entry.ActorID,
		ActorUsername: entry.ActorUsername,
		Action:        entry.Action,
		Resource:      entry.Resource,
		ResourceID:    entry.ResourceID,
		Status:        entry.Status,
		IPAddress:     entry.IPAddress,
		UserAgent:     entry.UserAgent,
		Metadata:      entry.Metadata,
	})
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

func TestFilesPostgresLifecycleFiltersAvatarGuardAndOrphanCleanup(t *testing.T) {
	pool := openIsolatedTestPool(t)
	ctx := t.Context()

	log := logger.New("test", "error", io.Discard)
	tx := database.NewTxManager(pool)
	hasher, err := security.NewPasswordHasher(security.DefaultArgon2Params())
	if err != nil {
		t.Fatalf("NewPasswordHasher: %v", err)
	}
	identitySvc := identity.NewService(pool, hasher, identity.ServiceDeps{Tx: tx, Log: log})

	admin, err := identitySvc.CreateInitialAdmin(ctx, identity.CreateInitialAdminInput{
		Username:    "filesadmin",
		Email:       "filesadmin@example.com",
		DisplayName: "Files Admin",
		Password:    "strongpass123",
	})
	if err != nil {
		t.Fatalf("CreateInitialAdmin: %v", err)
	}

	localStore, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	reg, err := storage.NewRegistry(storage.DriverLocal, localStore)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	adapter := &identityAdapter{svc: identitySvc}
	auditSvc := audit.NewService(pool, log)
	fileSvc := files.NewService(pool, reg, files.ServiceConfig{
		SignedURLSecret: "integration-test-secret-at-least-32-bytes",
		SignedURLTTL:    5 * time.Minute,
	}, files.ServiceDeps{
		Tx:        tx,
		Actors:    adapter,
		AvatarRef: adapter,
		Audit:     auditAdapter{svc: auditSvc},
		Log:       log,
	})

	// Upload a text file and a PDF document.
	txtRec, err := fileSvc.Upload(ctx, files.UploadInput{
		Filename:   "readme.txt",
		Reader:     strings.NewReader("hello postgres files"),
		UploaderID: &admin.ID,
	})
	if err != nil {
		t.Fatalf("Upload txt: %v", err)
	}
	pdfRec, err := fileSvc.Upload(ctx, files.UploadInput{
		Filename:   "invoice.pdf",
		Reader:     bytes.NewReader([]byte("%PDF-1.4\n%%EOF\n")),
		UploaderID: &admin.ID,
	})
	if err != nil {
		t.Fatalf("Upload pdf: %v", err)
	}

	// Filter by query and sort by original_name ASC.
	listed, err := fileSvc.List(ctx, files.ListFilter{
		Pagination: pagination.Normalize(1, 20),
		Categories: []string{files.CategoryDocument},
		Drivers:    []string{storage.DriverLocal},
		Sort:       files.SortOriginalName,
		Order:      files.OrderAsc,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if listed.Total != 2 || len(listed.Items) != 2 {
		t.Fatalf("listed total=%d len=%d, want 2", listed.Total, len(listed.Items))
	}
	if listed.Items[0].OriginalName != "invoice.pdf" || listed.Items[0].Uploader == nil || listed.Items[0].Uploader.Username != "filesadmin" {
		t.Fatalf("unexpected first item: %+v", listed.Items[0])
	}

	var uploadAudits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action = 'file.upload'`).Scan(&uploadAudits); err != nil {
		t.Fatalf("count upload audits: %v", err)
	}
	if uploadAudits != 2 {
		t.Fatalf("file.upload audit rows = %d, want 2", uploadAudits)
	}

	// Bind txtRec as admin's avatar -> Delete must return 409 FILES_IN_USE.
	if _, err := pool.Exec(ctx, "UPDATE users SET avatar_file_id = $1 WHERE id = $2", txtRec.ID, admin.ID); err != nil {
		t.Fatalf("bind avatar_file_id: %v", err)
	}
	err = fileSvc.Delete(ctx, files.DeleteInput{FileID: txtRec.ID, ActorID: &admin.ID})
	appErr, ok := apperr.As(err)
	if !ok || appErr.Code != files.ErrCodeInUse {
		t.Fatalf("Delete referenced avatar err = %v, want %s", err, files.ErrCodeInUse)
	}

	// Unbind avatar and delete cleanly.
	if _, err := pool.Exec(ctx, "UPDATE users SET avatar_file_id = NULL WHERE id = $1", admin.ID); err != nil {
		t.Fatalf("clear avatar_file_id: %v", err)
	}
	if err := fileSvc.Delete(ctx, files.DeleteInput{FileID: txtRec.ID, ActorID: &admin.ID}); err != nil {
		t.Fatalf("Delete unreferenced file: %v", err)
	}
	var deleteAudits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action = 'file.delete'`).Scan(&deleteAudits); err != nil {
		t.Fatalf("count delete audits: %v", err)
	}
	if deleteAudits != 1 {
		t.Fatalf("file.delete audit rows = %d, want 1", deleteAudits)
	}

	// Insert a stale pending record and verify CleanupOrphans purges both object and DB row.
	orphanID := uuid.Must(uuid.NewV7())
	orphanKey := "2026/11/" + orphanID.String() + ".txt"
	if _, err := localStore.Put(ctx, orphanKey, strings.NewReader("orphan")); err != nil {
		t.Fatalf("put orphan: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO files (id, uploader_id, storage_driver, object_key, original_name, mime_type, size_bytes, sha256, status, created_at, updated_at)
		VALUES ($1, $2, 'local', $3, 'stale.txt', 'text/plain', 6, 'abc', 'pending', now() - interval '2 hours', now() - interval '2 hours')
	`, orphanID, admin.ID, orphanKey); err != nil {
		t.Fatalf("insert stale pending: %v", err)
	}

	cleaned, err := fileSvc.CleanupOrphans(ctx, 15*time.Minute, 20)
	if err != nil || cleaned != 1 {
		t.Fatalf("CleanupOrphans = (%d, %v), want (1, nil)", cleaned, err)
	}

	// Ensure remaining ready file is untouched even when backdated older than pending cutoff.
	if _, err := pool.Exec(ctx, "UPDATE files SET updated_at = now() - interval '3 hours' WHERE id = $1", pdfRec.ID); err != nil {
		t.Fatalf("backdate pdfRec: %v", err)
	}
	cleanedAgain, err := fileSvc.CleanupOrphans(ctx, 15*time.Minute, 20)
	if err != nil || cleanedAgain != 0 {
		t.Fatalf("CleanupOrphans on ready file = (%d, %v), want (0, nil)", cleanedAgain, err)
	}
	gotPDF, err := fileSvc.Get(ctx, pdfRec.ID)
	if err != nil || gotPDF.ID != pdfRec.ID {
		t.Fatalf("expected pdfRec intact, got (%+v, %v)", gotPDF, err)
	}
}
