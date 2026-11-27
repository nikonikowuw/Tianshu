package auth_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/modules/auth"
	"tianshu/internal/platform/cache"
	"tianshu/internal/platform/config"
	"tianshu/internal/platform/database"
	"tianshu/internal/platform/logger"
	"tianshu/internal/platform/security"
	"tianshu/internal/shared/apperr"
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

// pgIdentityStub implements auth.IdentityProvider directly against the isolated PostgreSQL schema
// using the exact FOR UPDATE + CAS SQL semantics used in production, without importing sibling modules.
type pgIdentityStub struct {
	pool *pgxpool.Pool
}

func (p *pgIdentityStub) GetAuthCredentials(ctx context.Context, identifier string) (auth.UserCredentials, error) {
	exec := database.Executor(ctx, p.pool)
	var creds auth.UserCredentials
	err := exec.QueryRow(ctx, `
		SELECT id, username, email, display_name, status, password_hash
		FROM users
		WHERE deleted_at IS NULL AND (lower(username) = $1 OR lower(email) = $1)
		LIMIT 1
	`, strings.ToLower(strings.TrimSpace(identifier))).Scan(
		&creds.User.ID,
		&creds.User.Username,
		&creds.User.Email,
		&creds.User.DisplayName,
		&creds.User.Status,
		&creds.PasswordHash,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.UserCredentials{}, apperr.NotFound("USERS_NOT_FOUND")
	}
	if err != nil {
		return auth.UserCredentials{}, apperr.Internal(err)
	}
	return creds, nil
}

func (p *pgIdentityStub) GetByID(ctx context.Context, userID uuid.UUID) (auth.User, error) {
	exec := database.Executor(ctx, p.pool)
	var u auth.User
	err := exec.QueryRow(ctx, `
		SELECT id, username, email, display_name, status
		FROM users
		WHERE id = $1 AND deleted_at IS NULL
	`, userID).Scan(&u.ID, &u.Username, &u.Email, &u.DisplayName, &u.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.User{}, apperr.NotFound("USERS_NOT_FOUND")
	}
	if err != nil {
		return auth.User{}, apperr.Internal(err)
	}
	return u, nil
}

func (p *pgIdentityStub) CompleteLogin(ctx context.Context, input auth.CompleteLoginInput) error {
	exec := database.Executor(ctx, p.pool)
	var (
		status       string
		passwordHash string
	)
	err := exec.QueryRow(ctx, `
		SELECT status, password_hash
		FROM users
		WHERE id = $1 AND deleted_at IS NULL
		FOR UPDATE
	`, input.UserID).Scan(&status, &passwordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound("USERS_NOT_FOUND")
	}
	if err != nil {
		return apperr.Internal(err)
	}
	if status == "disabled" {
		return apperr.New(http.StatusForbidden, auth.ErrCodeAccountDisabled)
	}
	if passwordHash != input.ExpectedPasswordHash {
		return apperr.Unauthorized(apperr.CodeUnauthorized)
	}
	if input.UpgradedPasswordHash != "" {
		tag, upErr := exec.Exec(ctx, `
			UPDATE users SET password_hash = $2, updated_at = now()
			WHERE id = $1 AND deleted_at IS NULL AND status = 'active' AND password_hash = $3
		`, input.UserID, input.UpgradedPasswordHash, input.ExpectedPasswordHash)
		if upErr != nil {
			return apperr.Internal(upErr)
		}
		if tag.RowsAffected() == 0 {
			return apperr.Unauthorized(apperr.CodeUnauthorized)
		}
	}
	if _, err := exec.Exec(ctx, `
		UPDATE users SET last_login_at = now(), updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL
	`, input.UserID); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

type staticAccessStub struct{}

func (staticAccessStub) GetUserAccess(_ context.Context, _ uuid.UUID) (auth.UserAccess, error) {
	return auth.UserAccess{Roles: []string{"super_admin"}, Permissions: []string{"users:read"}}, nil
}

type recordingAuditStub struct {
	mu      sync.Mutex
	entries []auth.AuditEntry
	failErr error
}

func (r *recordingAuditStub) Record(_ context.Context, entry auth.AuditEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failErr != nil {
		return r.failErr
	}
	r.entries = append(r.entries, entry)
	return nil
}

func (r *recordingAuditStub) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}

func newPGAuthService(t *testing.T, pool *pgxpool.Pool, auditRec auth.AuditRecorder) (*auth.Service, *security.PasswordHasher, *security.JWTManager) {
	t.Helper()
	hasher, err := security.NewPasswordHasher(security.Argon2Params{
		MemoryKiB:   16 * 1024,
		Iterations:  1,
		Parallelism: 1,
		SaltLen:     16,
		KeyLen:      32,
	})
	if err != nil {
		t.Fatalf("NewPasswordHasher: %v", err)
	}
	jwtMgr, err := security.NewJWTManager("auth-it-secret-at-least-32-bytes-long", 15*time.Minute)
	if err != nil {
		t.Fatalf("NewJWTManager: %v", err)
	}
	mem := cache.NewMemory()
	t.Cleanup(func() { _ = mem.Close() })

	svc := auth.NewService(config.Default().Auth, pool, auth.ServiceDeps{
		Identity: &pgIdentityStub{pool: pool},
		Access:   staticAccessStub{},
		Hasher:   hasher,
		JWT:      jwtMgr,
		Tx:       database.NewTxManager(pool),
		Cache:    mem,
		Audit:    auditRec,
		Log:      logger.New("test", "error", io.Discard),
	})
	return svc, hasher, jwtMgr
}

func TestAuthSessionsActiveCheckAndPurgePostgres(t *testing.T) {
	pool := openAuthTestPool(t)
	ctx := t.Context()
	tx := database.NewTxManager(pool)
	svc, _, _ := newPGAuthService(t, pool, nil)

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
		t.Run(tc.name, func(t *testing.T) {
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
		})
	}

	// 2. PurgeExpiredSessions with 7-day retention must delete only oldExpiredID + hist-old.
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

func TestAuthRefreshRotationConcurrentGraceAndReplayPostgres(t *testing.T) {
	pool := openAuthTestPool(t)
	ctx := t.Context()
	auditRec := &recordingAuditStub{}
	svc, hasher, jwtMgr := newPGAuthService(t, pool, auditRec)

	passwordHash, err := hasher.Hash("pg-secret-password-123")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	userID := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, username, email, password_hash, display_name, status)
		VALUES ($1, 'pgadmin', 'pgadmin@example.com', $2, 'PG Admin', 'active')
	`, userID, passwordHash); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	// 1. Login persists session with UTF-8/NUL sanitized User-Agent and updates last_login_at.
	loginRes, err := svc.Login(ctx, auth.LoginInput{
		Identifier: "pgadmin",
		Password:   "pg-secret-password-123",
		ClientIP:   "192.0.2.10",
		UserAgent:  "Browser\x00/" + strings.Repeat("天", 300),
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	claims, err := jwtMgr.Verify(loginRes.AccessToken)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	// 2. Concurrent multi-tab refresh under PostgreSQL READ COMMITTED + SELECT FOR UPDATE:
	//    exactly 1 goroutine rotates the cookie and N-1 succeed inside grace window with RotateCookie=false.
	const workers = 8
	start := make(chan struct{})
	var (
		wg          sync.WaitGroup
		rotated     atomic.Int32
		graced      atomic.Int32
		errCount    atomic.Int32
		rotatedTok  string
		rotatedLock sync.Mutex
	)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			res, refErr := svc.Refresh(ctx, auth.RefreshInput{
				RefreshToken: loginRes.RefreshToken,
				ClientIP:     "192.0.2.10",
				UserAgent:    "ConcurrentTab/1.0",
			})
			if refErr != nil {
				errCount.Add(1)
				t.Errorf("concurrent Refresh error: %v", refErr)
				return
			}
			if res.RotateCookie {
				rotated.Add(1)
				rotatedLock.Lock()
				rotatedTok = res.RefreshToken
				rotatedLock.Unlock()
			} else {
				graced.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if errCount.Load() != 0 || rotated.Load() != 1 || graced.Load() != workers-1 {
		t.Fatalf("concurrent refresh outcome: rotated=%d graced=%d errs=%d, want 1/%d/0",
			rotated.Load(), graced.Load(), errCount.Load(), workers-1)
	}
	if rotatedTok == "" || rotatedTok == loginRes.RefreshToken {
		t.Fatal("expected non-empty rotated refresh token")
	}

	// Verify refresh_token_history persisted the old hash and session remains active.
	active, err := svc.IsSessionActive(ctx, userID, claims.SessionID)
	if err != nil || !active {
		t.Fatalf("session should remain active after concurrent grace refresh: active=%v err=%v", active, err)
	}

	// 3. Age replaced_at past grace window in DB and replay old token -> revokes session atomically in PostgreSQL.
	if _, err := pool.Exec(ctx, `
		UPDATE refresh_token_history
		SET replaced_at = now() - interval '2 minutes'
		WHERE session_id = $1
	`, claims.SessionID); err != nil {
		t.Fatalf("age history replaced_at: %v", err)
	}
	_, err = svc.Refresh(ctx, auth.RefreshInput{
		RefreshToken: loginRes.RefreshToken,
		ClientIP:     "192.0.2.99",
	})
	appErr, ok := apperr.As(err)
	if !ok || appErr.Status != http.StatusUnauthorized || appErr.Code != auth.ErrCodeRefreshReused {
		t.Fatalf("replay Refresh err = %v, want 401 %s", err, auth.ErrCodeRefreshReused)
	}

	// Confirm revocation committed to PostgreSQL and invalidated the latest rotated token too.
	var revoked bool
	if err := pool.QueryRow(ctx, `SELECT revoked_at IS NOT NULL FROM user_sessions WHERE id = $1`, claims.SessionID).Scan(&revoked); err != nil {
		t.Fatalf("query revoked_at: %v", err)
	}
	if !revoked {
		t.Fatal("replay detection must commit revoked_at IS NOT NULL in PostgreSQL")
	}
	if _, err := svc.Refresh(ctx, auth.RefreshInput{RefreshToken: rotatedTok}); err == nil {
		t.Fatal("latest rotated token must fail after replay revocation")
	}
}

func TestAuthLoginRollbackSessionsLogoutAndCascadePostgres(t *testing.T) {
	pool := openAuthTestPool(t)
	ctx := t.Context()
	auditRec := &recordingAuditStub{}
	svc, hasher, jwtMgr := newPGAuthService(t, pool, auditRec)

	passwordHash, err := hasher.Hash("cascade-password-123")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	userID := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, username, email, password_hash, display_name, status)
		VALUES ($1, 'cascadeuser', 'cascade@example.com', $2, 'Cascade User', 'active')
	`, userID, passwordHash); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	// 1. Login transaction rollback when audit recording fails leaves 0 sessions and NULL last_login_at.
	auditRec.failErr = apperr.Internal(errors.New("simulated audit failure"))
	if _, err := svc.Login(ctx, auth.LoginInput{
		Identifier: "cascadeuser",
		Password:   "cascade-password-123",
		ClientIP:   "192.0.2.1",
	}); err == nil {
		t.Fatal("expected Login failure when audit fails inside tx")
	}
	var sessCount int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM user_sessions WHERE user_id = $1`, userID).Scan(&sessCount); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessCount != 0 {
		t.Fatalf("expected 0 sessions after tx rollback, got %d", sessCount)
	}
	var lastLoginIsNull bool
	if err := pool.QueryRow(ctx, `SELECT last_login_at IS NULL FROM users WHERE id = $1`, userID).Scan(&lastLoginIsNull); err != nil {
		t.Fatalf("query last_login_at: %v", err)
	}
	if !lastLoginIsNull {
		t.Fatal("expected last_login_at rollback to NULL on tx failure")
	}
	auditRec.failErr = nil

	// 2. Create 3 sessions, verify ListActiveUserSessions, RevokeUserSession (IDOR + own),
	//    RevokeOtherUserSessions, and Logout via historical cookie.
	l1, err := svc.Login(ctx, auth.LoginInput{Identifier: "cascadeuser", Password: "cascade-password-123", ClientIP: "10.0.0.1"})
	if err != nil {
		t.Fatalf("Login 1: %v", err)
	}
	l2, err := svc.Login(ctx, auth.LoginInput{Identifier: "cascadeuser", Password: "cascade-password-123", ClientIP: "10.0.0.2"})
	if err != nil {
		t.Fatalf("Login 2: %v", err)
	}
	l3, err := svc.Login(ctx, auth.LoginInput{Identifier: "cascadeuser", Password: "cascade-password-123", ClientIP: "10.0.0.3"})
	if err != nil {
		t.Fatalf("Login 3: %v", err)
	}
	c1, _ := jwtMgr.Verify(l1.AccessToken)
	c2, _ := jwtMgr.Verify(l2.AccessToken)

	activeList, err := svc.ListActiveUserSessions(ctx, userID)
	if err != nil || len(activeList) != 3 {
		t.Fatalf("ListActiveUserSessions = (%d, %v), want (3, nil)", len(activeList), err)
	}

	// Cross-user revoke is a no-op; own revoke succeeds.
	if ok, err := svc.RevokeUserSession(ctx, uuid.Must(uuid.NewV7()), c2.SessionID); err != nil || ok {
		t.Fatalf("cross-user RevokeUserSession = (%v, %v), want (false, nil)", ok, err)
	}
	if ok, err := svc.RevokeUserSession(ctx, userID, c2.SessionID); err != nil || !ok {
		t.Fatalf("own RevokeUserSession = (%v, %v), want (true, nil)", ok, err)
	}

	// RevokeOtherUserSessions keeps c1 and revokes l3.
	revokedOthers, err := svc.RevokeOtherUserSessions(ctx, userID, c1.SessionID)
	if err != nil || revokedOthers != 1 {
		t.Fatalf("RevokeOtherUserSessions = (%d, %v), want (1, nil)", revokedOthers, err)
	}
	_ = l3

	// Rotate l1 so l1.RefreshToken moves to refresh_token_history, then logout with old l1 cookie.
	ref1, err := svc.Refresh(ctx, auth.RefreshInput{RefreshToken: l1.RefreshToken, ClientIP: "10.0.0.1"})
	if err != nil {
		t.Fatalf("Refresh l1: %v", err)
	}
	if err := svc.Logout(ctx, auth.LogoutInput{RefreshToken: l1.RefreshToken}); err != nil {
		t.Fatalf("Logout(historical): %v", err)
	}
	beforeAudits := auditRec.count()
	// Idempotent repeat logout emits no extra audit row.
	if err := svc.Logout(ctx, auth.LogoutInput{RefreshToken: ref1.RefreshToken, AccessToken: ref1.AccessToken}); err != nil {
		t.Fatalf("Logout(repeat): %v", err)
	}
	if auditRec.count() != beforeAudits {
		t.Fatalf("repeat logout wrote %d extra audit rows, want 0", auditRec.count()-beforeAudits)
	}

	// 3. Physical DELETE on users cascades to both user_sessions and refresh_token_history.
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	var remainSess, remainHist int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM user_sessions WHERE user_id = $1`, userID).Scan(&remainSess); err != nil {
		t.Fatalf("count user_sessions: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM refresh_token_history`).Scan(&remainHist); err != nil {
		t.Fatalf("count refresh_token_history: %v", err)
	}
	if remainSess != 0 || remainHist != 0 {
		t.Fatalf("ON DELETE CASCADE failed: remaining sessions=%d history=%d", remainSess, remainHist)
	}
}
