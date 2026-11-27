package auth_test

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/modules/auth"
	"tianshu/internal/platform/cache"
	"tianshu/internal/platform/config"
	"tianshu/internal/platform/database"
	"tianshu/internal/platform/logger"
	"tianshu/internal/platform/security"
	"tianshu/migrations"
)

func openAuthTestPool(t *testing.T) *pgxpool.Pool {
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

	schema := "auth_it_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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

func TestAuthSessionsActiveCheckAndPurgePostgres(t *testing.T) {
	pool := openAuthTestPool(t)
	ctx := t.Context()
	log := logger.New("test", "error", io.Discard)
	tx := database.NewTxManager(pool)
	mem := cache.NewMemory()
	defer func() { _ = mem.Close() }()

	hasher, err := security.NewPasswordHasher(security.DefaultArgon2Params())
	if err != nil {
		t.Fatalf("NewPasswordHasher: %v", err)
	}
	jwtMgr, err := security.NewJWTManager("auth-it-secret-at-least-32-bytes-long", 15*time.Minute)
	if err != nil {
		t.Fatalf("NewJWTManager: %v", err)
	}

	svc := auth.NewService(config.Default().Auth, pool, auth.ServiceDeps{
		Hasher: hasher,
		JWT:    jwtMgr,
		Tx:     tx,
		Cache:  mem,
		Log:    log,
	})

	userID := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, username, email, password_hash, display_name, status)
		VALUES ($1, 'purgeuser', 'purge@example.com', 'dummy-hash', 'Purge User', 'active')
	`, userID); err != nil {
		t.Fatalf("insert test user: %v", err)
	}

	now := time.Now().UTC()
	activeID := uuid.Must(uuid.NewV7())
	recentExpiredID := uuid.Must(uuid.NewV7())
	revokedFutureID := uuid.Must(uuid.NewV7())
	oldExpiredID := uuid.Must(uuid.NewV7())

	insertSession := func(id uuid.UUID, hash string, expiresAt time.Time, revokedAt *time.Time) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
			INSERT INTO user_sessions (id, user_id, refresh_token_hash, user_agent, ip_address, expires_at, revoked_at)
			VALUES ($1, $2, $3, 'TestUA', '127.0.0.1', $4, $5)
		`, id, userID, hash, expiresAt, revokedAt); err != nil {
			t.Fatalf("insert session %s: %v", id, err)
		}
	}
	insertHistory := func(hash string, sessionID uuid.UUID, expiresAt time.Time) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
			INSERT INTO refresh_token_history (token_hash, session_id, replaced_at, expires_at)
			VALUES ($1, $2, now(), $3)
		`, hash, sessionID, expiresAt); err != nil {
			t.Fatalf("insert history %s: %v", hash, err)
		}
	}

	revokedTime := now.Add(-10 * 24 * time.Hour)
	insertSession(activeID, "hash-active", now.Add(7*24*time.Hour), nil)
	insertSession(recentExpiredID, "hash-recent-expired", now.Add(-24*time.Hour), nil)
	insertSession(revokedFutureID, "hash-revoked-future", now.Add(24*time.Hour), &revokedTime)
	insertSession(oldExpiredID, "hash-old-expired", now.Add(-10*24*time.Hour), &revokedTime)

	insertHistory("hist-active", activeID, now.Add(7*24*time.Hour))
	insertHistory("hist-recent", recentExpiredID, now.Add(-24*time.Hour))
	insertHistory("hist-old", oldExpiredID, now.Add(-10*24*time.Hour))

	// 1. Verify IsSessionActive and LockActiveSessionForShare semantics on PostgreSQL.
	for _, tc := range []struct {
		name string
		id   uuid.UUID
		want bool
	}{
		{"active", activeID, true},
		{"recentExpired", recentExpiredID, false},
		{"revokedFuture", revokedFutureID, false},
		{"oldExpired", oldExpiredID, false},
	} {
		got, err := svc.IsSessionActive(ctx, userID, tc.id)
		if err != nil || got != tc.want {
			t.Fatalf("IsSessionActive(%s) = (%v, %v), want (%v, nil)", tc.name, got, err, tc.want)
		}
		if err := tx.WithinTx(ctx, func(txCtx context.Context) error {
			locked, lockErr := svc.LockActiveSessionForShare(txCtx, userID, tc.id)
			if lockErr != nil || locked != tc.want {
				t.Fatalf("LockActiveSessionForShare(%s) = (%v, %v), want (%v, nil)", tc.name, locked, lockErr, tc.want)
			}
			return nil
		}); err != nil {
			t.Fatalf("WithinTx(%s): %v", tc.name, err)
		}
	}

	// 2. PurgeExpiredSessions with 7-day retention must delete only oldExpiredID + hist-old
	//    while preserving active, recently-expired (within retention), and revoked-but-unexpired rows.
	deleted, err := svc.PurgeExpiredSessions(ctx, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("PurgeExpiredSessions: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("PurgeExpiredSessions deleted = %d, want 2 (1 history + 1 session)", deleted)
	}

	var remainingSessions, remainingHistory int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM user_sessions WHERE user_id = $1`, userID).Scan(&remainingSessions); err != nil {
		t.Fatalf("count remaining sessions: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM refresh_token_history`).Scan(&remainingHistory); err != nil {
		t.Fatalf("count remaining history: %v", err)
	}
	if remainingSessions != 3 || remainingHistory != 2 {
		t.Fatalf("remaining = (sessions=%d, history=%d), want (3, 2)", remainingSessions, remainingHistory)
	}
}
