package audit_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/modules/audit"
	"tianshu/internal/platform/config"
	"tianshu/internal/platform/database"
	"tianshu/internal/platform/logger"
	"tianshu/internal/shared/pagination"
	"tianshu/migrations"
)

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
			t.Fatalf("postgres ping failed: %v", err)
		}
		t.Skipf("postgres ping failed: %v", err)
	}

	schema := "audit_it_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	poolCfg.ConnConfig.RuntimeParams["search_path"] = schema

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		t.Fatalf("open schema pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := database.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return pool
}

func TestAuditPostgresIntegration(t *testing.T) {
	pool := openIsolatedTestPool(t)
	txMgr := database.NewTxManager(pool)
	svc := audit.NewService(pool, logger.New("test", "error", io.Discard))

	// 1. Record entries inside a committed transaction and verify redaction + request_id propagation.
	reqCtx := logger.WithRequestID(t.Context(), "req_pg_integration_01")
	err := txMgr.WithinTx(reqCtx, func(txCtx context.Context) error {
		if err := svc.Record(txCtx, audit.RecordInput{
			ActorUsername: "alice_admin",
			Action:        "auth.login",
			Resource:      "session",
			ResourceID:    "sess-100",
			Status:        audit.StatusSuccess,
			IPAddress:     "10.1.2.3",
			UserAgent:     "TestBrowser/1.0",
			Metadata: map[string]any{
				"password": "should-be-redacted",
				"method":   "password",
			},
		}); err != nil {
			return err
		}
		return svc.Record(txCtx, audit.RecordInput{
			ActorUsername: "bob_100%",
			Action:        "auth.login",
			Resource:      "session",
			Status:        audit.StatusFailure,
			IPAddress:     "10.1.2.4",
			Metadata:      map[string]any{"reason": "invalid_credentials"},
		})
	})
	if err != nil {
		t.Fatalf("WithinTx() error = %v", err)
	}

	// 2. Rolled-back transaction must NOT persist audit row.
	_ = txMgr.WithinTx(reqCtx, func(txCtx context.Context) error {
		if err := svc.Record(txCtx, audit.RecordInput{
			ActorUsername: "rolled_back_user",
			Action:        "setup.init",
			Resource:      "system",
			Status:        audit.StatusSuccess,
		}); err != nil {
			return err
		}
		return fmt.Errorf("force rollback")
	})

	// 3. Query with literal '%' in q to verify ILIKE wildcard escaping.
	resPct, err := svc.List(t.Context(), audit.ListFilter{
		Pagination: pagination.Params{Page: 1, PageSize: 20},
		Q:          "100%",
	})
	if err != nil {
		t.Fatalf("List(100%%) error = %v", err)
	}
	if resPct.Total != 1 || len(resPct.Items) != 1 || resPct.Items[0].ActorUsername != "bob_100%" {
		t.Fatalf("escaped ILIKE result = %+v, want only bob_100%%", resPct)
	}

	// 4. Filter by status=success and verify metadata redaction & request_id persisted.
	resSuccess, err := svc.List(t.Context(), audit.ListFilter{
		Pagination: pagination.Params{Page: 1, PageSize: 20},
		Statuses:   []string{audit.StatusSuccess},
		Sort:       audit.SortAction,
		Order:      audit.OrderAsc,
	})
	if err != nil {
		t.Fatalf("List(success) error = %v", err)
	}
	if resSuccess.Total != 1 || resSuccess.Items[0].ActorUsername != "alice_admin" {
		t.Fatalf("success list = %+v, want alice_admin", resSuccess)
	}
	if resSuccess.Items[0].RequestID != "req_pg_integration_01" {
		t.Fatalf("RequestID = %q, want req_pg_integration_01", resSuccess.Items[0].RequestID)
	}
	if resSuccess.Items[0].Metadata["password"] != "[REDACTED]" {
		t.Fatalf("persisted metadata password = %v, want [REDACTED]", resSuccess.Items[0].Metadata["password"])
	}

	// 5. Retention purge deletes old rows in batches while keeping fresh rows.
	if _, err := pool.Exec(t.Context(),
		"UPDATE audit_logs SET created_at = now() - interval '200 days' WHERE actor_username = 'bob_100%'",
	); err != nil {
		t.Fatalf("backdate audit row: %v", err)
	}
	deleted, err := svc.PurgeExpired(t.Context(), 180)
	if err != nil {
		t.Fatalf("PurgeExpired() error = %v", err)
	}
	if deleted != 1 {
		t.Fatalf("PurgeExpired() deleted = %d, want 1", deleted)
	}

	remaining, err := svc.List(t.Context(), audit.ListFilter{
		Pagination: pagination.Params{Page: 1, PageSize: 20},
	})
	if err != nil {
		t.Fatalf("List(after purge) error = %v", err)
	}
	if remaining.Total != 1 || remaining.Items[0].ActorUsername != "alice_admin" {
		t.Fatalf("remaining after purge = %+v, want only alice_admin", remaining)
	}
}
