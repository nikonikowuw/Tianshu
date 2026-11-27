package dashboard_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/modules/dashboard"
	"tianshu/internal/platform/config"
	"tianshu/internal/platform/database"
	"tianshu/migrations"
)

func openDashboardTestPool(t *testing.T) *pgxpool.Pool {
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
			t.Fatalf("postgres ping failed: %v", err)
		}
		t.Skipf("postgres ping failed: %v", err)
	}

	schema := "dashboard_it_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		adminPool.Close()
		t.Fatalf("create isolated schema %s: %v", schema, err)
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = adminPool.Exec(cleanupCtx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
		adminPool.Close()
	})

	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	poolCfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		t.Fatalf("open schema pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := database.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate isolated schema: %v", err)
	}
	return pool
}

func TestDashboardSummaryPostgresAggregation(t *testing.T) {
	pool := openDashboardTestPool(t)
	ctx := t.Context()
	now := time.Now().UTC()

	activeUser1 := uuid.Must(uuid.NewV7())
	activeUser2 := uuid.Must(uuid.NewV7())
	disabledUser := uuid.Must(uuid.NewV7())
	deletedUser := uuid.Must(uuid.NewV7())

	// Insert users: 2 active (1 recent, 1 older than 7d), 1 disabled (recent), 1 soft-deleted (must be excluded).
	mustExec(t, pool, `
		INSERT INTO users (id, username, email, password_hash, display_name, status, deleted_at, created_at, updated_at)
		VALUES
			($1, 'admin', 'admin@example.com', 'hash', 'Admin', 'active', NULL, $5, $5),
			($2, 'veteran', 'veteran@example.com', 'hash', 'Veteran', 'active', NULL, $6, $6),
			($3, 'locked', 'locked@example.com', 'hash', 'Locked', 'disabled', NULL, $5, $5),
			($4, 'ghost', 'ghost@example.com', 'hash', 'Ghost', 'active', $5, $5, $5)
	`, activeUser1, activeUser2, disabledUser, deletedUser, now.Add(-2*time.Hour), now.AddDate(0, 0, -20))

	// Insert roles: 1 system + 2 custom.
	mustExec(t, pool, `
		INSERT INTO roles (id, code, name, is_system)
		VALUES ($1, 'super_admin', 'Super Admin', true), ($2, 'editor', 'Editor', false), ($3, 'viewer', 'Viewer', false)
	`, uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()))

	// Insert files: 2 ready (1 image 4096B, 1 document 8192B), plus pending/failed/deleting (must be excluded).
	mustExec(t, pool, `
		INSERT INTO files (id, uploader_id, storage_driver, object_key, original_name, mime_type, size_bytes, sha256, status)
		VALUES
			($1, $6, 'local', 'general/img.png', 'img.png', 'image/png', 4096, 'sha1', 'ready'),
			($2, $6, 'local', 'general/doc.pdf', 'doc.pdf', 'application/pdf', 8192, 'sha2', 'ready'),
			($3, $6, 'local', 'general/p.png', 'p.png', 'image/png', 99999, 'sha3', 'pending'),
			($4, $6, 'local', 'general/f.png', 'f.png', 'image/png', 99999, 'sha4', 'failed'),
			($5, $6, 'local', 'general/d.png', 'd.png', 'image/png', 99999, 'sha5', 'deleting')
	`, uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), activeUser1)

	// Insert sessions: 1 valid active session, 1 revoked, 1 expired, 1 belonging to disabled user (all 3 excluded).
	mustExec(t, pool, `
		INSERT INTO user_sessions (id, user_id, refresh_token_hash, expires_at, revoked_at)
		VALUES
			($1, $5, 'tok-active', $7, NULL),
			($2, $5, 'tok-revoked', $7, $8),
			($3, $5, 'tok-expired', $9, NULL),
			($4, $6, 'tok-disabled', $7, NULL)
	`, uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()),
		activeUser1, disabledUser, now.Add(24*time.Hour), now.Add(-1*time.Hour), now.Add(-1*time.Hour))

	// Insert 10 audit logs across today, 3 days ago, and 10 days ago to test 7d vs 14d windows & recentLimit=8.
	today := time.Date(now.Year(), now.Month(), now.Day(), 1, 0, 0, 0, time.UTC)
	threeDaysAgo := today.AddDate(0, 0, -3)
	tenDaysAgo := today.AddDate(0, 0, -10)

	events := []struct {
		actor     string
		action    string
		resource  string
		status    string
		createdAt time.Time
	}{
		{"admin", "auth.login", "session", "success", today.Add(5 * time.Minute)},
		{"admin", "auth.login", "session", "failure", today.Add(4 * time.Minute)},
		{"admin", "file.upload", "file", "success", today.Add(3 * time.Minute)},
		{"admin", "user.create", "user", "success", today.Add(2 * time.Minute)},
		{"admin", "role.create", "role", "success", today.Add(1 * time.Minute)},
		{"admin", "profile.update", "user", "success", threeDaysAgo.Add(3 * time.Minute)},
		{"admin", "auth.login", "session", "success", threeDaysAgo.Add(2 * time.Minute)},
		{"admin", "file.delete", "file", "success", threeDaysAgo.Add(1 * time.Minute)},
		{"admin", "auth.login", "session", "success", tenDaysAgo.Add(2 * time.Minute)},
		{"admin", "setup.init", "system", "success", tenDaysAgo.Add(1 * time.Minute)},
	}
	for _, ev := range events {
		mustExec(t, pool, `
			INSERT INTO audit_logs (id, actor_username, action, resource, status, ip_address, created_at)
			VALUES ($1, $2, $3, $4, $5, '127.0.0.1', $6)
		`, uuid.Must(uuid.NewV7()), ev.actor, ev.action, ev.resource, ev.status, ev.createdAt)
	}

	svc := dashboard.NewService(pool)

	summary7, err := svc.Summary(ctx, dashboard.SummaryInput{Days: 7})
	if err != nil {
		t.Fatalf("Summary(7) error = %v", err)
	}
	if summary7.Users.Total != 3 || summary7.Users.Active != 2 || summary7.Users.Disabled != 1 || summary7.Users.NewLast7Days != 2 {
		t.Fatalf("Users = %+v, want {Total:3 Active:2 Disabled:1 NewLast7Days:2}", summary7.Users)
	}
	if summary7.Roles.Total != 3 || summary7.Roles.System != 1 || summary7.Roles.Custom != 2 {
		t.Fatalf("Roles = %+v, want {Total:3 System:1 Custom:2}", summary7.Roles)
	}
	if summary7.Files.Total != 2 || summary7.Files.TotalSizeBytes != 12288 || summary7.Files.Images != 1 || summary7.Files.Documents != 1 {
		t.Fatalf("Files = %+v, want {Total:2 TotalSizeBytes:12288 Images:1 Documents:1}", summary7.Files)
	}
	if summary7.Sessions.Active != 1 {
		t.Fatalf("Sessions.Active = %d, want 1", summary7.Sessions.Active)
	}
	if len(summary7.ActivityTrend) != 7 {
		t.Fatalf("len(ActivityTrend) = %d, want 7", len(summary7.ActivityTrend))
	}
	if summary7.Audit.TotalInRange != 8 || summary7.Audit.LoginsInRange != 2 || summary7.Audit.FailuresInRange != 1 {
		t.Fatalf("Audit(7d) = %+v, want {TotalInRange:8 LoginsInRange:2 FailuresInRange:1}", summary7.Audit)
	}
	if len(summary7.RecentActivity) != 8 {
		t.Fatalf("len(RecentActivity) = %d, want 8", len(summary7.RecentActivity))
	}
	if summary7.RecentActivity[0].Action != "auth.login" || summary7.RecentActivity[0].Status != "success" {
		t.Fatalf("RecentActivity[0] = %+v, want latest login success", summary7.RecentActivity[0])
	}

	// 14-day window includes the two 10-day-old events.
	summary14, err := svc.Summary(ctx, dashboard.SummaryInput{Days: 14})
	if err != nil {
		t.Fatalf("Summary(14) error = %v", err)
	}
	if len(summary14.ActivityTrend) != 14 {
		t.Fatalf("len(ActivityTrend) = %d, want 14", len(summary14.ActivityTrend))
	}
	if summary14.Audit.TotalInRange != 10 || summary14.Audit.LoginsInRange != 3 || summary14.Audit.FailuresInRange != 1 {
		t.Fatalf("Audit(14d) = %+v, want {TotalInRange:10 LoginsInRange:3 FailuresInRange:1}", summary14.Audit)
	}
}

func mustExec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("exec failed: %v", err)
	}
}
