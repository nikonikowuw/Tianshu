package audit_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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
	svc := audit.NewService(pool)

	actorID := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(t.Context(),
		"INSERT INTO users (id, username, email, password_hash) VALUES ($1, $2, $3, $4)",
		actorID, "alice_admin", "alice@example.com", "argon2id-test-hash",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	// 1. Record entries inside a committed transaction and verify redaction, UTF-8 UA truncation, and request_id propagation.
	reqCtx := logger.WithRequestID(t.Context(), "req_pg_integration_01")
	err := txMgr.WithinTx(reqCtx, func(txCtx context.Context) error {
		if err := svc.Record(txCtx, audit.RecordInput{
			ActorID:       &actorID,
			ActorUsername: "alice_admin\x00",
			Action:        "auth.login",
			Resource:      "session",
			ResourceID:    "sess-100",
			Status:        audit.StatusSuccess,
			IPAddress:     "10.1.2.3",
			UserAgent:     "TestBrowser/1.0 " + strings.Repeat("中文🚀", 100) + "\x00\xff",
			Metadata: map[string]any{
				"password": "should-be-redacted",
				"method":   "password",
			},
		}); err != nil {
			return err
		}
		return svc.Record(txCtx, audit.RecordInput{
			ActorUsername: `bob_100%\wild`,
			Action:        "role.update",
			Resource:      "role",
			Status:        audit.StatusFailure,
			IPAddress:     "10.1.2.4",
			Metadata:      map[string]any{"reason": "invalid_credentials"},
		})
	})
	if err != nil {
		t.Fatalf("WithinTx() error = %v", err)
	}

	// 2. Rolled-back transaction must return error and must NOT persist audit row.
	if err := txMgr.WithinTx(reqCtx, func(txCtx context.Context) error {
		if err := svc.Record(txCtx, audit.RecordInput{
			ActorUsername: "rolled_back_user",
			Action:        "setup.init",
			Resource:      "system",
			Status:        audit.StatusSuccess,
		}); err != nil {
			return err
		}
		return fmt.Errorf("force rollback")
	}); err == nil {
		t.Fatal("expected forced rollback WithinTx to return an error")
	}

	// 3. Query with literal '%', '_', and '\' in q to verify ILIKE wildcard escaping.
	for _, needle := range []string{"100%", `%\wild`, `bob_100`} {
		resEsc, err := svc.List(t.Context(), audit.ListFilter{
			Pagination: pagination.Params{Page: 1, PageSize: 20},
			Q:          needle,
		})
		if err != nil {
			t.Fatalf("List(%q) error = %v", needle, err)
		}
		if resEsc.Total != 1 || len(resEsc.Items) != 1 || resEsc.Items[0].ActorUsername != `bob_100%\wild` {
			t.Fatalf("escaped ILIKE(%q) result = %+v, want only bob_100%%\\wild", needle, resEsc)
		}
	}

	// 4. Filter by actorId, actor, actions, resources, and time bounds.
	resActorID, err := svc.List(t.Context(), audit.ListFilter{
		Pagination: pagination.Params{Page: 1, PageSize: 20},
		ActorID:    &actorID,
	})
	if err != nil {
		t.Fatalf("List(actorID) error = %v", err)
	}
	if resActorID.Total != 1 || resActorID.Items[0].ActorID == nil || *resActorID.Items[0].ActorID != actorID {
		t.Fatalf("actorID list = %+v, want alice_admin", resActorID)
	}
	if !utf8.ValidString(resActorID.Items[0].UserAgent) || len(resActorID.Items[0].UserAgent) > 512 {
		t.Fatalf("persisted UserAgent invalid: len=%d valid=%v", len(resActorID.Items[0].UserAgent), utf8.ValidString(resActorID.Items[0].UserAgent))
	}

	otherID := uuid.Must(uuid.NewV7())
	resOtherID, err := svc.List(t.Context(), audit.ListFilter{
		Pagination: pagination.Params{Page: 1, PageSize: 20},
		ActorID:    &otherID,
	})
	if err != nil {
		t.Fatalf("List(otherID) error = %v", err)
	}
	if resOtherID.Total != 0 || len(resOtherID.Items) != 0 {
		t.Fatalf("otherID list = %+v, want empty", resOtherID)
	}

	fromBound := time.Now().UTC().Add(-time.Hour)
	toBound := time.Now().UTC().Add(time.Hour)
	resFiltered, err := svc.List(t.Context(), audit.ListFilter{
		Pagination: pagination.Params{Page: 1, PageSize: 20},
		Actor:      "bob_100%",
		Actions:    []string{"role.update"},
		Resources:  []string{"role"},
		Statuses:   []string{audit.StatusFailure},
		From:       &fromBound,
		To:         &toBound,
	})
	if err != nil {
		t.Fatalf("List(combined filters) error = %v", err)
	}
	if resFiltered.Total != 1 || resFiltered.Items[0].ActorUsername != `bob_100%\wild` {
		t.Fatalf("combined filter list = %+v, want bob_100%%\\wild", resFiltered)
	}

	// 5. Verify ON DELETE SET NULL preserves audit row and actor_username while nulling actor_id.
	if _, err := pool.Exec(t.Context(), "DELETE FROM users WHERE id = $1", actorID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
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
	if resSuccess.Items[0].ActorID != nil {
		t.Fatalf("ActorID after user delete = %v, want nil (ON DELETE SET NULL)", resSuccess.Items[0].ActorID)
	}
	if resSuccess.Items[0].RequestID != "req_pg_integration_01" {
		t.Fatalf("RequestID = %q, want req_pg_integration_01", resSuccess.Items[0].RequestID)
	}
	if resSuccess.Items[0].Metadata["password"] != "[REDACTED]" {
		t.Fatalf("persisted metadata password = %v, want [REDACTED]", resSuccess.Items[0].Metadata["password"])
	}

	// 6. Retention purge deletes old rows in batches while keeping fresh rows.
	if _, err := pool.Exec(t.Context(),
		`UPDATE audit_logs SET created_at = now() - interval '200 days' WHERE actor_username = $1`,
		`bob_100%\wild`,
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
