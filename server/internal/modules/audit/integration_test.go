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
	"tianshu/internal/shared/apperr"
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
				"password":   "should-be-redacted",
				"method":     "password",
				"note":       "clean\x00value\xff",
				"bad\x00key": "v\x00",
				"tags":       []string{"t\x001", "t\xff2"},
				"strMap":     map[string]string{"api.key": "k", "msg": "hi\x00"},
				"badCh":      make(chan int),
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

	// 3b. Verify q matches resource_id, request_id, and ip_address, and NUL/UTF-8 queries return 422 before hitting Postgres.
	for _, tc := range []struct {
		needle    string
		wantTotal int64
	}{
		{"sess-100", 1},
		{"req_pg_integration_01", 2},
		{"10.1.2.3", 1},
	} {
		resCol, err := svc.List(t.Context(), audit.ListFilter{
			Pagination: pagination.Params{Page: 1, PageSize: 20},
			Q:          tc.needle,
		})
		if err != nil {
			t.Fatalf("List(%q) error = %v", tc.needle, err)
		}
		if resCol.Total != tc.wantTotal || int64(len(resCol.Items)) != tc.wantTotal {
			t.Fatalf("q=%q result = %+v, want total=%d", tc.needle, resCol, tc.wantTotal)
		}
	}
	for _, bad := range []string{"abc\x00", "\xff"} {
		if _, err := svc.List(t.Context(), audit.ListFilter{Q: bad}); err == nil {
			t.Fatalf("List(Q=%q) expected 422 error", bad)
		} else if appErr, ok := apperr.As(err); !ok || appErr.Status != 422 {
			t.Fatalf("List(Q=%q) err = %v, want 422", bad, err)
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

func TestAuditHotPathTieBreakerAndSkipLockedPurge(t *testing.T) {
	pool := openIsolatedTestPool(t)
	svc := audit.NewService(pool)

	// 1. Verify migration 00005 replaced idx_audit_logs_created_at with idx_audit_logs_created_at_id.
	var hasComposite, hasLegacy bool
	if err := pool.QueryRow(t.Context(), `
		SELECT
			EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname = current_schema() AND tablename = 'audit_logs' AND indexname = 'idx_audit_logs_created_at_id'),
			EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname = current_schema() AND tablename = 'audit_logs' AND indexname = 'idx_audit_logs_created_at')
	`).Scan(&hasComposite, &hasLegacy); err != nil {
		t.Fatalf("inspect pg_indexes: %v", err)
	}
	if !hasComposite || hasLegacy {
		t.Fatalf("audit_logs indexes: hasComposite=%v hasLegacy=%v, want true/false", hasComposite, hasLegacy)
	}

	// Verify hot-path query uses idx_audit_logs_created_at_id with zero Sort node.
	explainTx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin explain tx: %v", err)
	}
	defer func() { _ = explainTx.Rollback(t.Context()) }()
	if _, err := explainTx.Exec(t.Context(), "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatalf("disable seqscan for explain: %v", err)
	}
	for _, q := range []string{
		"EXPLAIN SELECT id FROM audit_logs ORDER BY created_at DESC, id DESC LIMIT 20 OFFSET 0",
		"EXPLAIN SELECT id FROM audit_logs WHERE created_at >= now() - interval '1 day' ORDER BY created_at DESC, id DESC LIMIT 20 OFFSET 0",
		"EXPLAIN SELECT id FROM audit_logs WHERE created_at <= now() ORDER BY created_at ASC, id ASC LIMIT 20 OFFSET 0",
	} {
		planRows, err := explainTx.Query(t.Context(), q)
		if err != nil {
			t.Fatalf("explain hot-path query %q: %v", q, err)
		}
		var planLines []string
		for planRows.Next() {
			var line string
			if err := planRows.Scan(&line); err != nil {
				planRows.Close()
				t.Fatalf("scan explain line: %v", err)
			}
			planLines = append(planLines, line)
		}
		planRows.Close()
		if err := planRows.Err(); err != nil {
			t.Fatalf("explain rows err: %v", err)
		}
		plan := strings.Join(planLines, "\n")
		if !strings.Contains(plan, "idx_audit_logs_created_at_id") || strings.Contains(plan, "Sort") {
			t.Fatalf("expected zero-sort index scan on idx_audit_logs_created_at_id for %q, got plan:\n%s", q, plan)
		}
	}
	_ = explainTx.Rollback(t.Context())

	// 2. Seed deterministic rows:
	//    - idOld1, idOld2: expired (>180 days ago) for SKIP LOCKED concurrency verification
	//    - idTieLow, idTieHigh: identical action & created_at to verify deterministic tie-breaker
	//    - idNewest: most recent row
	now := time.Now().UTC().Truncate(time.Microsecond)
	idOld1 := uuid.MustParse("01900000-0000-7000-8000-000000000001")
	idOld2 := uuid.MustParse("01900000-0000-7000-8000-000000000002")
	idTieLow := uuid.MustParse("01900000-0000-7000-8000-000000000010")
	idTieHigh := uuid.MustParse("01900000-0000-7000-8000-000000000011")
	idNewest := uuid.MustParse("01900000-0000-7000-8000-000000000020")

	tOld1 := now.AddDate(0, 0, -201)
	tOld2 := now.AddDate(0, 0, -200)
	tTie := now.Add(-2 * time.Hour)
	tNewest := now.Add(-1 * time.Hour)

	seedRows := []struct {
		id        uuid.UUID
		action    string
		createdAt time.Time
	}{
		{idOld1, "auth.login", tOld1},
		{idOld2, "auth.login", tOld2},
		{idTieLow, "role.update", tTie},
		{idTieHigh, "role.update", tTie},
		{idNewest, "user.create", tNewest},
	}
	for _, r := range seedRows {
		if _, err := pool.Exec(t.Context(), `
			INSERT INTO audit_logs (id, actor_username, action, resource, status, created_at)
			VALUES ($1, 'tester', $2, 'system', 'success', $3)
		`, r.id, r.action, r.createdAt); err != nil {
			t.Fatalf("seed row %s: %v", r.id, err)
		}
	}

	assertAuditIDs := func(t *testing.T, label string, got pagination.Result[audit.Entry], want []uuid.UUID) {
		t.Helper()
		if got.Total != int64(len(want)) || len(got.Items) != len(want) {
			t.Fatalf("%s: total=%d len=%d, want %d", label, got.Total, len(got.Items), len(want))
		}
		for i, wantID := range want {
			if got.Items[i].ID != wantID {
				t.Fatalf("%s[%d] = %s, want %s", label, i, got.Items[i].ID, wantID)
			}
		}
	}

	// 3. Verify unfiltered hot-path DESC & ASC ordering (including identical created_at UUID tie-breaker).
	descRes, err := svc.List(t.Context(), audit.ListFilter{
		Pagination: pagination.Params{Page: 1, PageSize: 20},
		Sort:       audit.SortCreatedAt,
		Order:      audit.OrderDesc,
	})
	if err != nil {
		t.Fatalf("List(unfiltered DESC) error = %v", err)
	}
	assertAuditIDs(t, "unfiltered DESC", descRes, []uuid.UUID{idNewest, idTieHigh, idTieLow, idOld2, idOld1})

	ascRes, err := svc.List(t.Context(), audit.ListFilter{
		Pagination: pagination.Params{Page: 1, PageSize: 20},
		Sort:       audit.SortCreatedAt,
		Order:      audit.OrderAsc,
	})
	if err != nil {
		t.Fatalf("List(unfiltered ASC) error = %v", err)
	}
	assertAuditIDs(t, "unfiltered ASC", ascRes, []uuid.UUID{idOld1, idOld2, idTieLow, idTieHigh, idNewest})

	// 4. Verify time-range-only hot-path (both DESC and ASC) and single-bound fallback.
	fromWindow := tTie.Add(-time.Minute)
	toWindow := tNewest.Add(time.Minute)
	rangeDesc, err := svc.List(t.Context(), audit.ListFilter{
		Pagination: pagination.Params{Page: 1, PageSize: 20},
		From:       &fromWindow,
		To:         &toWindow,
		Sort:       audit.SortCreatedAt,
		Order:      audit.OrderDesc,
	})
	if err != nil {
		t.Fatalf("List(timeRange DESC) error = %v", err)
	}
	assertAuditIDs(t, "timeRange DESC", rangeDesc, []uuid.UUID{idNewest, idTieHigh, idTieLow})

	rangeAsc, err := svc.List(t.Context(), audit.ListFilter{
		Pagination: pagination.Params{Page: 1, PageSize: 20},
		From:       &fromWindow,
		To:         &toWindow,
		Sort:       audit.SortCreatedAt,
		Order:      audit.OrderAsc,
	})
	if err != nil {
		t.Fatalf("List(timeRange ASC) error = %v", err)
	}
	assertAuditIDs(t, "timeRange ASC", rangeAsc, []uuid.UUID{idTieLow, idTieHigh, idNewest})

	fromOnlyRes, err := svc.List(t.Context(), audit.ListFilter{
		Pagination: pagination.Params{Page: 1, PageSize: 20},
		From:       &fromWindow,
	})
	if err != nil {
		t.Fatalf("List(from-only) error = %v", err)
	}
	assertAuditIDs(t, "from-only DESC", fromOnlyRes, []uuid.UUID{idNewest, idTieHigh, idTieLow})

	fromOnlyAsc, err := svc.List(t.Context(), audit.ListFilter{
		Pagination: pagination.Params{Page: 1, PageSize: 20},
		From:       &fromWindow,
		Order:      audit.OrderAsc,
	})
	if err != nil {
		t.Fatalf("List(from-only ASC) error = %v", err)
	}
	assertAuditIDs(t, "from-only ASC", fromOnlyAsc, []uuid.UUID{idTieLow, idTieHigh, idNewest})

	toOnlyWindow := tOld2.Add(time.Minute)
	toOnlyDesc, err := svc.List(t.Context(), audit.ListFilter{
		Pagination: pagination.Params{Page: 1, PageSize: 20},
		To:         &toOnlyWindow,
	})
	if err != nil {
		t.Fatalf("List(to-only DESC) error = %v", err)
	}
	assertAuditIDs(t, "to-only DESC", toOnlyDesc, []uuid.UUID{idOld2, idOld1})

	toOnlyAsc, err := svc.List(t.Context(), audit.ListFilter{
		Pagination: pagination.Params{Page: 1, PageSize: 20},
		To:         &toOnlyWindow,
		Order:      audit.OrderAsc,
	})
	if err != nil {
		t.Fatalf("List(to-only ASC) error = %v", err)
	}
	assertAuditIDs(t, "to-only ASC", toOnlyAsc, []uuid.UUID{idOld1, idOld2})

	// 5. Verify general ListAuditLogs tie-breaker for BOTH created_at ASC (id ASC) and
	//    non-created_at sorts (action / resource / status -> created_at DESC, id DESC).
	generalCreatedAsc, err := svc.List(t.Context(), audit.ListFilter{
		Pagination: pagination.Params{Page: 1, PageSize: 20},
		Actions:    []string{"role.update"},
		Sort:       audit.SortCreatedAt,
		Order:      audit.OrderAsc,
	})
	if err != nil {
		t.Fatalf("List(general created_at ASC) error = %v", err)
	}
	assertAuditIDs(t, "general created_at ASC", generalCreatedAsc, []uuid.UUID{idTieLow, idTieHigh})

	for _, sortCol := range []string{audit.SortAction, audit.SortResource, audit.SortStatus} {
		for _, order := range []string{audit.OrderAsc, audit.OrderDesc} {
			byCol, err := svc.List(t.Context(), audit.ListFilter{
				Pagination: pagination.Params{Page: 1, PageSize: 20},
				Actions:    []string{"role.update"},
				Sort:       sortCol,
				Order:      order,
			})
			if err != nil {
				t.Fatalf("List(%s %s) error = %v", sortCol, order, err)
			}
			assertAuditIDs(t, "general "+sortCol+" "+order, byCol, []uuid.UUID{idTieHigh, idTieLow})
		}
	}

	// 6. Verify FOR UPDATE SKIP LOCKED skips rows locked by another transaction instead of blocking.
	lockTx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin lock tx: %v", err)
	}
	defer func() { _ = lockTx.Rollback(t.Context()) }()
	var lockedID uuid.UUID
	if err := lockTx.QueryRow(t.Context(), "SELECT id FROM audit_logs WHERE id = $1 FOR UPDATE", idOld1).Scan(&lockedID); err != nil {
		t.Fatalf("lock oldest expired row: %v", err)
	}

	purgeCtx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	deletedWhileLocked, err := svc.PurgeExpired(purgeCtx, 180)
	if err != nil {
		t.Fatalf("PurgeExpired(while locked) error = %v", err)
	}
	if deletedWhileLocked != 1 {
		t.Fatalf("PurgeExpired(while locked) deleted = %d, want 1 (skipping locked idOld1)", deletedWhileLocked)
	}

	if err := lockTx.Rollback(t.Context()); err != nil {
		t.Fatalf("rollback lock tx: %v", err)
	}

	deletedAfterUnlock, err := svc.PurgeExpired(t.Context(), 180)
	if err != nil {
		t.Fatalf("PurgeExpired(after unlock) error = %v", err)
	}
	if deletedAfterUnlock != 1 {
		t.Fatalf("PurgeExpired(after unlock) deleted = %d, want 1 (now-unlocked idOld1)", deletedAfterUnlock)
	}
}
