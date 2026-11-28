package auth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"tianshu/internal/platform/cache"
	"tianshu/internal/platform/config"
	"tianshu/internal/platform/database"
	"tianshu/internal/platform/i18n"
	"tianshu/internal/platform/logger"
	"tianshu/internal/platform/security"
	"tianshu/internal/shared/apperr"
)

type fakeAuthStore struct {
	sessions        map[uuid.UUID]sessionRecord
	history         map[string]refreshHistoryRecord
	now             func() time.Time
	revokeByIDCalls int
	// revokeErr, when set, is returned by every revoke path to exercise transaction rollback branches.
	revokeErr error
}

func newFakeAuthStore(nowFn func() time.Time) *fakeAuthStore {
	return &fakeAuthStore{
		sessions: make(map[uuid.UUID]sessionRecord),
		history:  make(map[string]refreshHistoryRecord),
		now:      nowFn,
	}
}

func (f *fakeAuthStore) CreateSession(_ context.Context, rec sessionRecord) error {
	rec.CreatedAt = f.now()
	rec.LastUsedAt = f.now()
	f.sessions[rec.ID] = rec
	return nil
}

func (f *fakeAuthStore) GetSessionByRefreshHashForUpdate(_ context.Context, tokenHash string) (sessionRecord, error) {
	for _, s := range f.sessions {
		if s.RefreshTokenHash == tokenHash {
			return s, nil
		}
	}
	return sessionRecord{}, errSessionNotFound
}

func (f *fakeAuthStore) GetSessionByIDForUpdate(_ context.Context, sessionID uuid.UUID) (sessionRecord, error) {
	s, ok := f.sessions[sessionID]
	if !ok {
		return sessionRecord{}, errSessionNotFound
	}
	return s, nil
}

func (f *fakeAuthStore) RotateSessionRefreshToken(
	_ context.Context,
	sessionID uuid.UUID,
	newHash, ua, ip string,
	expiresAt time.Time,
) error {
	s, ok := f.sessions[sessionID]
	if !ok {
		return errSessionNotFound
	}
	s.RefreshTokenHash = newHash
	s.UserAgent = ua
	s.IPAddress = ip
	s.ExpiresAt = expiresAt
	s.LastUsedAt = f.now()
	f.sessions[sessionID] = s
	return nil
}

func (f *fakeAuthStore) InsertRefreshTokenHistory(
	_ context.Context,
	tokenHash string,
	sessionID uuid.UUID,
	replacedAt, expiresAt time.Time,
) error {
	f.history[tokenHash] = refreshHistoryRecord{
		TokenHash:  tokenHash,
		SessionID:  sessionID,
		ReplacedAt: replacedAt.UTC(),
		ExpiresAt:  expiresAt.UTC(),
	}
	return nil
}

func (f *fakeAuthStore) GetRefreshTokenHistoryByHash(_ context.Context, tokenHash string) (refreshHistoryRecord, error) {
	h, ok := f.history[tokenHash]
	if !ok {
		return refreshHistoryRecord{}, errHistoryNotFound
	}
	return h, nil
}

func (f *fakeAuthStore) RevokeSession(_ context.Context, sessionID uuid.UUID) (revokedSession, bool, error) {
	if f.revokeErr != nil {
		return revokedSession{}, false, f.revokeErr
	}
	f.revokeByIDCalls++
	if s, ok := f.sessions[sessionID]; ok && s.RevokedAt == nil {
		t := f.now()
		s.RevokedAt = &t
		f.sessions[sessionID] = s
		return revokedSession{ID: s.ID, UserID: s.UserID}, true, nil
	}
	return revokedSession{}, false, nil
}

func (f *fakeAuthStore) RevokeSessionByRefreshHash(_ context.Context, tokenHash string) (revokedSession, bool, error) {
	if f.revokeErr != nil {
		return revokedSession{}, false, f.revokeErr
	}
	for id, s := range f.sessions {
		if s.RefreshTokenHash == tokenHash && s.RevokedAt == nil {
			t := f.now()
			s.RevokedAt = &t
			f.sessions[id] = s
			return revokedSession{ID: s.ID, UserID: s.UserID}, true, nil
		}
	}
	return revokedSession{}, false, nil
}

func (f *fakeAuthStore) RevokeAllUserSessions(_ context.Context, userID uuid.UUID) error {
	if f.revokeErr != nil {
		return f.revokeErr
	}
	for id, s := range f.sessions {
		if s.UserID == userID && s.RevokedAt == nil {
			t := f.now()
			s.RevokedAt = &t
			f.sessions[id] = s
		}
	}
	return nil
}

func (f *fakeAuthStore) ListActiveUserSessions(_ context.Context, userID uuid.UUID) ([]ActiveSession, error) {
	now := f.now().UTC()
	out := make([]ActiveSession, 0)
	for _, s := range f.sessions {
		if s.UserID == userID && s.RevokedAt == nil && s.ExpiresAt.After(now) {
			out = append(out, ActiveSession{
				ID:         s.ID,
				IPAddress:  s.IPAddress,
				UserAgent:  s.UserAgent,
				LastUsedAt: s.LastUsedAt,
				CreatedAt:  s.CreatedAt,
				ExpiresAt:  s.ExpiresAt,
			})
		}
	}
	return out, nil
}

func (f *fakeAuthStore) IsUserSessionActive(_ context.Context, userID, sessionID uuid.UUID) (bool, error) {
	now := f.now().UTC()
	s, ok := f.sessions[sessionID]
	return ok && s.UserID == userID && s.RevokedAt == nil && s.ExpiresAt.After(now), nil
}

func (f *fakeAuthStore) LockActiveUserSessionForShare(ctx context.Context, userID, sessionID uuid.UUID) (bool, error) {
	return f.IsUserSessionActive(ctx, userID, sessionID)
}

func (f *fakeAuthStore) RevokeUserSessionByID(_ context.Context, userID, sessionID uuid.UUID) (bool, error) {
	now := f.now().UTC()
	s, ok := f.sessions[sessionID]
	if !ok || s.UserID != userID || s.RevokedAt != nil || !s.ExpiresAt.After(now) {
		return false, nil
	}
	s.RevokedAt = &now
	f.sessions[sessionID] = s
	return true, nil
}

func (f *fakeAuthStore) RevokeOtherUserSessions(_ context.Context, userID, keepSessionID uuid.UUID) (int64, error) {
	now := f.now().UTC()
	var count int64
	for id, s := range f.sessions {
		if s.UserID == userID && s.ID != keepSessionID && s.RevokedAt == nil && s.ExpiresAt.After(now) {
			t := now
			s.RevokedAt = &t
			f.sessions[id] = s
			count++
		}
	}
	return count, nil
}

func (f *fakeAuthStore) DeleteExpiredRefreshTokenHistoryBatch(_ context.Context, cutoff time.Time, limit int32) (int64, error) {
	var deleted int64
	for k, h := range f.history {
		if deleted >= int64(limit) {
			break
		}
		if h.ExpiresAt.Before(cutoff) {
			delete(f.history, k)
			deleted++
		}
	}
	return deleted, nil
}

func (f *fakeAuthStore) DeleteExpiredUserSessionsBatch(_ context.Context, cutoff time.Time, limit int32) (int64, error) {
	var deleted int64
	for id, s := range f.sessions {
		if deleted >= int64(limit) {
			break
		}
		if s.ExpiresAt.Before(cutoff) {
			delete(f.sessions, id)
			deleted++
		}
	}
	return deleted, nil
}

type fakeIdentityProvider struct {
	users           map[uuid.UUID]UserCredentials
	getByIDErr      error
	completeHook    func(ctx context.Context, input CompleteLoginInput) error
	completeCalls   int
	lastUpgradeHash string
}

func (f *fakeIdentityProvider) GetAuthCredentials(_ context.Context, identifier string) (UserCredentials, error) {
	lower := strings.ToLower(strings.TrimSpace(identifier))
	for _, u := range f.users {
		if strings.EqualFold(u.User.Username, lower) || strings.EqualFold(u.User.Email, lower) {
			return u, nil
		}
	}
	return UserCredentials{}, apperr.NotFound("USERS_NOT_FOUND")
}

func (f *fakeIdentityProvider) GetByID(_ context.Context, userID uuid.UUID) (User, error) {
	if f.getByIDErr != nil {
		return User{}, f.getByIDErr
	}
	u, ok := f.users[userID]
	if !ok {
		return User{}, apperr.NotFound("USERS_NOT_FOUND")
	}
	return u.User, nil
}

func (f *fakeIdentityProvider) CompleteLogin(ctx context.Context, input CompleteLoginInput) error {
	f.completeCalls++
	if f.completeHook != nil {
		if err := f.completeHook(ctx, input); err != nil {
			return err
		}
	}
	u, ok := f.users[input.UserID]
	if !ok {
		return apperr.NotFound("USERS_NOT_FOUND")
	}
	if u.User.Status == userStatusDisabled {
		return accountDisabledError()
	}
	if u.PasswordHash != input.ExpectedPasswordHash {
		return apperr.Unauthorized(apperr.CodeUnauthorized)
	}
	if input.UpgradedPasswordHash != "" {
		u.PasswordHash = input.UpgradedPasswordHash
		f.users[input.UserID] = u
		f.lastUpgradeHash = input.UpgradedPasswordHash
	}
	return nil
}

type fakeAccessProvider struct {
	access UserAccess
	err    error
}

func (f *fakeAccessProvider) GetUserAccess(_ context.Context, _ uuid.UUID) (UserAccess, error) {
	if f.err != nil {
		return UserAccess{}, f.err
	}
	return f.access, nil
}

type fakeTx struct {
	st          *fakeAuthStore
	outerErr    error
	failOnOuter bool
}

func (f fakeTx) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if f.failOnOuter && f.outerErr != nil {
		return f.outerErr
	}
	// Snapshot store maps so any non-nil tx error truly rolls back mutations like PostgreSQL.
	snapSessions := make(map[uuid.UUID]sessionRecord, len(f.st.sessions))
	for k, v := range f.st.sessions {
		snapSessions[k] = v
	}
	snapHistory := make(map[string]refreshHistoryRecord, len(f.st.history))
	for k, v := range f.st.history {
		snapHistory[k] = v
	}

	err := database.WithSimulatedTx(ctx, fn)
	if err != nil {
		f.st.sessions = snapSessions
		f.st.history = snapHistory
	}
	return err
}

type fakeAuditRecorder struct {
	entries []AuditEntry
	err     error
}

func (f *fakeAuditRecorder) Record(_ context.Context, entry AuditEntry) error {
	if f.err != nil {
		return f.err
	}
	f.entries = append(f.entries, entry)
	return nil
}

type spyPasswordVerifier struct {
	inner            PasswordVerifier
	dummyCalls       int
	forceNeedsRehash bool
	hashErr          error
	verifyErr        error
}

func (s *spyPasswordVerifier) Hash(password string) (string, error) {
	if s.hashErr != nil {
		return "", s.hashErr
	}
	return s.inner.Hash(password)
}

func (s *spyPasswordVerifier) Verify(password, encodedHash string) (bool, bool, error) {
	if s.verifyErr != nil {
		return false, false, s.verifyErr
	}
	matched, needsRehash, err := s.inner.Verify(password, encodedHash)
	if matched && s.forceNeedsRehash {
		needsRehash = true
	}
	return matched, needsRehash, err
}

func (s *spyPasswordVerifier) VerifyDummy(password string) {
	s.dummyCalls++
	s.inner.VerifyDummy(password)
}

func newTestAuthHarness(t *testing.T) (*Service, *fakeAuthStore, *fakeIdentityProvider, *time.Time) {
	t.Helper()
	svc, st, idProvider, now, _ := newTestAuthHarnessWithAudit(t)
	return svc, st, idProvider, now
}

func newTestAuthHarnessWithAudit(t *testing.T) (*Service, *fakeAuthStore, *fakeIdentityProvider, *time.Time, *fakeAuditRecorder) {
	t.Helper()
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return now }

	hasher, err := security.NewPasswordHasher(security.Argon2Params{
		MemoryKiB:   16 * 1024,
		Iterations:  1,
		Parallelism: 1,
		SaltLen:     16,
		KeyLen:      32,
	})
	if err != nil {
		t.Fatalf("NewPasswordHasher() error = %v", err)
	}
	hash, err := hasher.Hash("correct-password-123")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}

	userID := uuid.Must(uuid.NewV7())
	idProvider := &fakeIdentityProvider{
		users: map[uuid.UUID]UserCredentials{
			userID: {
				User: User{
					ID:          userID,
					Username:    "admin",
					Email:       "admin@example.com",
					DisplayName: "Admin",
					Status:      userStatusActive,
				},
				PasswordHash: hash,
			},
		},
	}

	jwtMgr, err := security.NewJWTManager("test-secret-for-auth-service-32-bytes!!", 15*time.Minute)
	if err != nil {
		t.Fatalf("NewJWTManager() error = %v", err)
	}

	mem := cache.NewMemory()
	t.Cleanup(func() { _ = mem.Close() })

	st := newFakeAuthStore(nowFn)
	auditRec := &fakeAuditRecorder{}
	svc := newServiceWithStore(
		config.AuthConfig{
			JWTSecret:       "test-secret-for-auth-service-32-bytes!!",
			AccessTokenTTL:  15 * time.Minute,
			RefreshTokenTTL: 7 * 24 * time.Hour,
		},
		st,
		ServiceDeps{
			Identity: idProvider,
			Access: &fakeAccessProvider{access: UserAccess{
				Roles:       []string{"super_admin"},
				Permissions: []string{"dashboard:read", "users:read"},
			}},
			Hasher: hasher,
			JWT:    jwtMgr,
			Tx:     fakeTx{st: st},
			Cache:  mem,
			Audit:  auditRec,
			Log:    logger.New("test", "error", io.Discard),
		},
	)
	svc.now = nowFn
	return svc, st, idProvider, &now, auditRec
}

func TestLoginRefreshGraceWindowAndReplayRevocation(t *testing.T) {
	svc, _, _, now := newTestAuthHarness(t)

	// 1. Login with uppercase email succeeds and returns tokens + permissions.
	loginRes, err := svc.Login(t.Context(), LoginInput{
		Identifier: "ADMIN@EXAMPLE.COM",
		Password:   "correct-password-123",
		ClientIP:   "127.0.0.1",
		UserAgent:  "test-agent",
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if loginRes.AccessToken == "" || loginRes.RefreshToken == "" {
		t.Fatal("expected non-empty access and refresh tokens")
	}

	// 2. First refresh rotates token and records old token in refresh_token_history.
	ref1, err := svc.Refresh(t.Context(), RefreshInput{
		RefreshToken: loginRes.RefreshToken,
		ClientIP:     "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("first Refresh() error = %v", err)
	}
	if !ref1.RotateCookie || ref1.RefreshToken == "" || ref1.RefreshToken == loginRes.RefreshToken {
		t.Fatal("expected refresh token rotation on active session")
	}

	// 3. Reusing old token within 45s concurrent grace window succeeds without rotating cookie or revoking session.
	*now = now.Add(15 * time.Second)
	graceRes, err := svc.Refresh(t.Context(), RefreshInput{
		RefreshToken: loginRes.RefreshToken,
		ClientIP:     "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("grace-window Refresh() error = %v", err)
	}
	if graceRes.RotateCookie {
		t.Fatal("grace-window refresh must not overwrite rotated cookie")
	}

	// 4. Reusing old token AFTER grace window triggers replay detection and revokes the entire session.
	*now = now.Add(60 * time.Second)
	_, err = svc.Refresh(t.Context(), RefreshInput{
		RefreshToken: loginRes.RefreshToken,
		ClientIP:     "127.0.0.1",
	})
	appErr, ok := apperr.As(err)
	if !ok || appErr.Status != http.StatusUnauthorized || appErr.Code != ErrCodeRefreshReused {
		t.Fatalf("replay error = %v, want 401 %s", err, ErrCodeRefreshReused)
	}

	// 5. Even the latest rotated token (ref1.RefreshToken) is now invalid because the session was revoked.
	if _, err := svc.Refresh(t.Context(), RefreshInput{
		RefreshToken: ref1.RefreshToken,
		ClientIP:     "127.0.0.1",
	}); err == nil {
		t.Fatal("expected revoked session to reject previously rotated token")
	}
}

// TestRefreshGraceWindowBoundary pins the exact refreshGraceWindow edge so a future tuning
// that silently widens or narrows the window cannot slip past the suite.
func TestRefreshGraceWindowBoundary(t *testing.T) {
	cases := []struct {
		name       string
		offset     time.Duration
		wantReplay bool
	}{
		{"just inside window", refreshGraceWindow - time.Nanosecond, false},
		{"exactly at window", refreshGraceWindow, false},
		{"just past window", refreshGraceWindow + time.Nanosecond, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _, now := newTestAuthHarness(t)
			loginRes, err := svc.Login(t.Context(), LoginInput{
				Identifier: "admin",
				Password:   "correct-password-123",
				ClientIP:   "127.0.0.1",
			})
			if err != nil {
				t.Fatalf("Login() error = %v", err)
			}
			if _, err := svc.Refresh(t.Context(), RefreshInput{RefreshToken: loginRes.RefreshToken, ClientIP: "127.0.0.1"}); err != nil {
				t.Fatalf("first Refresh() error = %v", err)
			}

			*now = now.Add(tc.offset)
			_, err = svc.Refresh(t.Context(), RefreshInput{RefreshToken: loginRes.RefreshToken, ClientIP: "127.0.0.1"})
			appErr, ok := apperr.As(err)
			if tc.wantReplay {
				if !ok || appErr.Code != ErrCodeRefreshReused {
					t.Fatalf("err = %v, want %s", err, ErrCodeRefreshReused)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected grace-window success, got %v", err)
			}
		})
	}
}

func TestLoginSoftCooldownDoesNotDisableUser(t *testing.T) {
	svc, _, idProvider, now := newTestAuthHarness(t)

	for i := range maxLoginFailures {
		_, err := svc.Login(t.Context(), LoginInput{
			Identifier: "admin",
			Password:   "wrong-password-123",
			ClientIP:   "198.51.100.5",
		})
		appErr, ok := apperr.As(err)
		if !ok || appErr.Status != http.StatusUnauthorized || appErr.Code != ErrCodeInvalidCredentials {
			t.Fatalf("failure %d err = %v, want 401 %s", i+1, err, ErrCodeInvalidCredentials)
		}
	}

	// 11th attempt must hit 429 soft cooldown even with the correct password or email alias.
	for _, ident := range []string{"admin", "admin@example.com"} {
		_, err := svc.Login(t.Context(), LoginInput{
			Identifier: ident,
			Password:   "correct-password-123",
			ClientIP:   "198.51.100.5",
		})
		appErr, ok := apperr.As(err)
		if !ok || appErr.Status != http.StatusTooManyRequests || appErr.Code != apperr.CodeRateLimited {
			t.Fatalf("cooldown attempt (%s) err = %v, want 429 %s", ident, err, apperr.CodeRateLimited)
		}
		if retry, _ := appErr.Params["retry_after"].(int); retry < 800 {
			t.Fatalf("cooldown retry_after (%s) = %v, want ~900s", ident, appErr.Params["retry_after"])
		}
	}

	// User status must remain active (never permanently locked by attacker).
	for _, u := range idProvider.users {
		if u.User.Status != userStatusActive {
			t.Fatalf("user status mutated to %q during soft cooldown", u.User.Status)
		}
	}

	// After cooldown expires, a single wrong password must NOT immediately re-trigger 15m block.
	*now = now.Add(loginCooldownDuration + time.Second)
	_, err := svc.Login(t.Context(), LoginInput{
		Identifier: "admin",
		Password:   "wrong-password-123",
		ClientIP:   "198.51.100.5",
	})
	if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeInvalidCredentials {
		t.Fatalf("post-cooldown failure err = %v, want %s", err, ErrCodeInvalidCredentials)
	}
	// Successful login via email alias must clear failure counters for both email and username.
	if _, err := svc.Login(t.Context(), LoginInput{
		Identifier: "admin@example.com",
		Password:   "correct-password-123",
		ClientIP:   "198.51.100.5",
	}); err != nil {
		t.Fatalf("post-cooldown login via email error = %v", err)
	}
}

func TestLoginAliasCooldownAndPerAccountRateLimit(t *testing.T) {
	t.Run("username and email aliases share soft cooldown budget", func(t *testing.T) {
		svc, _, _, _ := newTestAuthHarness(t)
		for i := range 6 {
			_, err := svc.Login(t.Context(), LoginInput{
				Identifier: "admin",
				Password:   "wrong-password-123",
				ClientIP:   "198.51.100.10",
			})
			if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeInvalidCredentials {
				t.Fatalf("username fail %d: %v", i+1, err)
			}
		}
		for i := range 4 {
			_, err := svc.Login(t.Context(), LoginInput{
				Identifier: "admin@example.com",
				Password:   "wrong-password-123",
				ClientIP:   "198.51.100.11",
			})
			if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeInvalidCredentials {
				t.Fatalf("email fail %d: %v", i+1, err)
			}
		}
		// 11th attempt from another IP using username must be blocked by canonical account cooldown.
		_, err := svc.Login(t.Context(), LoginInput{
			Identifier: "admin",
			Password:   "correct-password-123",
			ClientIP:   "198.51.100.99",
		})
		appErr, ok := apperr.As(err)
		if !ok || appErr.Status != http.StatusTooManyRequests || appErr.Code != apperr.CodeRateLimited {
			t.Fatalf("expected 429 cooldown across aliases, got %v", err)
		}
	})

	t.Run("per-account 10 req/min rate limit blocks rotating IPs and aliases", func(t *testing.T) {
		svc, _, _, now := newTestAuthHarness(t)
		for i := range maxLoginAccountRequestsPerMin {
			ident := "admin"
			if i%2 == 1 {
				ident = "admin@example.com"
			}
			if _, err := svc.Login(t.Context(), LoginInput{
				Identifier: ident,
				Password:   "correct-password-123",
				ClientIP:   "198.51.100." + string(rune('1'+i)),
			}); err != nil {
				t.Fatalf("attempt %d (%s) error = %v", i+1, ident, err)
			}
		}
		// 11th request within 1 minute hits per-account rate limit with 1..60s Retry-After.
		_, err := svc.Login(t.Context(), LoginInput{
			Identifier: "admin@example.com",
			Password:   "correct-password-123",
			ClientIP:   "203.0.113.50",
		})
		appErr, ok := apperr.As(err)
		if !ok || appErr.Status != http.StatusTooManyRequests || appErr.Code != apperr.CodeRateLimited {
			t.Fatalf("11th attempt err = %v, want 429 %s", err, apperr.CodeRateLimited)
		}
		if retry, _ := appErr.Params["retry_after"].(int); retry <= 0 || retry > 60 {
			t.Fatalf("rate-limit retry_after = %v, want 1..60", appErr.Params["retry_after"])
		}

		// Advancing 1 minute resets the window.
		*now = now.Add(time.Minute)
		if _, err := svc.Login(t.Context(), LoginInput{
			Identifier: "admin",
			Password:   "correct-password-123",
			ClientIP:   "203.0.113.51",
		}); err != nil {
			t.Fatalf("login after window reset error = %v", err)
		}
	})
}

func TestLoginValidationAndEdgeCases(t *testing.T) {
	t.Run("whitespace identifier and overlong password return 422", func(t *testing.T) {
		svc, _, _, _ := newTestAuthHarness(t)
		cases := []struct {
			name       string
			identifier string
			password   string
			wantFields []string
		}{
			{"whitespace identifier", "   ", "correct-password-123", []string{"identifier"}},
			{"empty password", "admin", "", []string{"password"}},
			{"overlong password", "admin", strings.Repeat("a", security.MaxPasswordLength+1), []string{"password"}},
			{"both invalid", "  ", strings.Repeat("x", security.MaxPasswordLength+5), []string{"identifier", "password"}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := svc.Login(t.Context(), LoginInput{Identifier: tc.identifier, Password: tc.password})
				appErr, ok := apperr.As(err)
				if !ok || appErr.Status != http.StatusUnprocessableEntity || appErr.Code != apperr.CodeValidationFailed {
					t.Fatalf("err = %v, want 422 %s", err, apperr.CodeValidationFailed)
				}
				if len(appErr.Fields) != len(tc.wantFields) {
					t.Fatalf("fields = %+v, want %v", appErr.Fields, tc.wantFields)
				}
				for i, want := range tc.wantFields {
					if appErr.Fields[i].Field != want {
						t.Fatalf("field[%d] = %q, want %q", i, appErr.Fields[i].Field, want)
					}
				}
			})
		}
	})

	t.Run("non-existent user runs VerifyDummy and emits nil-actor failure audit", func(t *testing.T) {
		svc, _, _, _, auditRec := newTestAuthHarnessWithAudit(t)
		spy := &spyPasswordVerifier{inner: svc.hasher}
		svc.hasher = spy

		_, err := svc.Login(t.Context(), LoginInput{
			Identifier: "ghost-user",
			Password:   "some-password-123",
			ClientIP:   "10.1.1.1",
		})
		appErr, ok := apperr.As(err)
		if !ok || appErr.Code != ErrCodeInvalidCredentials {
			t.Fatalf("err = %v, want %s", err, ErrCodeInvalidCredentials)
		}
		if spy.dummyCalls != 1 {
			t.Fatalf("VerifyDummy calls = %d, want 1", spy.dummyCalls)
		}
		if len(auditRec.entries) != 1 || auditRec.entries[0].ActorID != nil || auditRec.entries[0].ActorUsername != "ghost-user" {
			t.Fatalf("unexpected audit entries: %+v", auditRec.entries)
		}
	})

	t.Run("corrupt password hash fails cleanly with 401", func(t *testing.T) {
		svc, _, _, _, auditRec := newTestAuthHarnessWithAudit(t)
		svc.hasher = &spyPasswordVerifier{inner: svc.hasher, verifyErr: security.ErrInvalidHashFormat}

		_, err := svc.Login(t.Context(), LoginInput{
			Identifier: "admin",
			Password:   "correct-password-123",
			ClientIP:   "10.1.1.2",
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeInvalidCredentials {
			t.Fatalf("err = %v, want %s", err, ErrCodeInvalidCredentials)
		}
		if len(auditRec.entries) != 1 || auditRec.entries[0].Status != "failure" {
			t.Fatalf("expected failure audit entry, got %+v", auditRec.entries)
		}
	})

	t.Run("transparent rehash upgrades hash atomically inside CompleteLogin", func(t *testing.T) {
		svc, _, idProv, _ := newTestAuthHarness(t)
		spy := &spyPasswordVerifier{inner: svc.hasher, forceNeedsRehash: true}
		svc.hasher = spy

		if _, err := svc.Login(t.Context(), LoginInput{
			Identifier: "admin",
			Password:   "correct-password-123",
			ClientIP:   "10.1.1.3",
		}); err != nil {
			t.Fatalf("Login() error = %v", err)
		}
		if idProv.lastUpgradeHash == "" {
			t.Fatal("expected transparent rehash to persist upgraded hash via CompleteLogin")
		}
	})

	t.Run("rehash error logs warning and still completes login", func(t *testing.T) {
		svc, _, idProv, _ := newTestAuthHarness(t)
		svc.hasher = &spyPasswordVerifier{
			inner:            svc.hasher,
			forceNeedsRehash: true,
			hashErr:          errors.New("entropy failure"),
		}
		if _, err := svc.Login(t.Context(), LoginInput{
			Identifier: "admin",
			Password:   "correct-password-123",
			ClientIP:   "10.1.1.4",
		}); err != nil {
			t.Fatalf("Login() should succeed when rehash fails, got %v", err)
		}
		if idProv.lastUpgradeHash != "" {
			t.Fatalf("unexpected upgraded hash %q", idProv.lastUpgradeHash)
		}
	})
}

func TestLoginTOCTOURacesAndRollback(t *testing.T) {
	t.Run("concurrent password change prevents stale rehash rollback and aborts session creation", func(t *testing.T) {
		svc, st, idProv, _ := newTestAuthHarness(t)
		svc.hasher = &spyPasswordVerifier{inner: svc.hasher, forceNeedsRehash: true}
		const newHash = "$argon2id$v=19$m=16384,t=1,p=1$bmV3c2FsdA$bmV3a2V5"
		idProv.completeHook = func(_ context.Context, input CompleteLoginInput) error {
			u := idProv.users[input.UserID]
			u.PasswordHash = newHash
			idProv.users[input.UserID] = u
			return nil
		}

		_, err := svc.Login(t.Context(), LoginInput{
			Identifier: "admin",
			Password:   "correct-password-123",
			ClientIP:   "10.2.0.1",
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeInvalidCredentials {
			t.Fatalf("err = %v, want %s", err, ErrCodeInvalidCredentials)
		}
		if len(st.sessions) != 0 {
			t.Fatalf("expected 0 sessions created on concurrent password change, got %d", len(st.sessions))
		}
		for _, u := range idProv.users {
			if u.PasswordHash != newHash {
				t.Fatalf("stale rehash clobbered new password hash: got %q, want %q", u.PasswordHash, newHash)
			}
		}
	})

	t.Run("concurrent disable during Argon2 rejects login with 403 and creates no session", func(t *testing.T) {
		svc, st, idProv, _, auditRec := newTestAuthHarnessWithAudit(t)
		idProv.completeHook = func(_ context.Context, input CompleteLoginInput) error {
			u := idProv.users[input.UserID]
			u.User.Status = userStatusDisabled
			idProv.users[input.UserID] = u
			return nil
		}

		_, err := svc.Login(t.Context(), LoginInput{
			Identifier: "admin",
			Password:   "correct-password-123",
			ClientIP:   "10.2.0.2",
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusForbidden || appErr.Code != ErrCodeAccountDisabled {
			t.Fatalf("err = %v, want 403 %s", err, ErrCodeAccountDisabled)
		}
		if len(st.sessions) != 0 {
			t.Fatalf("expected 0 sessions created for concurrently disabled user, got %d", len(st.sessions))
		}
		if len(auditRec.entries) != 1 || auditRec.entries[0].Metadata["reason"] != "account_disabled" {
			t.Fatalf("expected account_disabled audit, got %+v", auditRec.entries)
		}
	})

	t.Run("GetUserAccess failure aborts before committing session or success audit", func(t *testing.T) {
		svc, st, _, _, auditRec := newTestAuthHarnessWithAudit(t)
		svc.access = &fakeAccessProvider{err: apperr.Internal(errors.New("rbac down"))}

		_, err := svc.Login(t.Context(), LoginInput{
			Identifier: "admin",
			Password:   "correct-password-123",
			ClientIP:   "10.2.0.3",
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusInternalServerError {
			t.Fatalf("err = %v, want 500", err)
		}
		if len(st.sessions) != 0 || len(auditRec.entries) != 0 {
			t.Fatalf("expected 0 sessions and 0 audits, got sessions=%d audits=%d", len(st.sessions), len(auditRec.entries))
		}
	})

	t.Run("audit Record failure rolls back created session", func(t *testing.T) {
		svc, st, _, _, auditRec := newTestAuthHarnessWithAudit(t)
		auditRec.err = apperr.Internal(errors.New("audit db failure"))

		_, err := svc.Login(t.Context(), LoginInput{
			Identifier: "admin",
			Password:   "correct-password-123",
			ClientIP:   "10.2.0.4",
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusInternalServerError {
			t.Fatalf("err = %v, want 500", err)
		}
		if len(st.sessions) != 0 {
			t.Fatalf("expected session rollback on audit failure, got %d sessions", len(st.sessions))
		}
	})
}

func TestTruncateUASanitizesUTF8AndNUL(t *testing.T) {
	// NUL bytes and invalid UTF-8 sequences must be stripped.
	got := truncateUA("Mozilla/5.0\x00\xff\xfe Android")
	if strings.ContainsRune(got, 0) || !utf8.ValidString(got) {
		t.Fatalf("truncateUA produced invalid UTF-8/NUL: %q", got)
	}

	// Multi-byte UTF-8 > 512 bytes must be truncated on a valid rune boundary.
	longCJK := strings.Repeat("天", 300) // 900 bytes (3 bytes/rune)
	truncated := truncateUA(longCJK)
	if len(truncated) > maxUserAgentBytes || !utf8.ValidString(truncated) {
		t.Fatalf("truncated len=%d valid=%v", len(truncated), utf8.ValidString(truncated))
	}
	if len(truncated) != 510 { // floor(512/3)*3
		t.Fatalf("truncated len = %d, want 510", len(truncated))
	}
}

func TestRefreshHistoricalTokenErrorAndLifecycleBranches(t *testing.T) {
	t.Run("transient GetByID error inside grace window returns 500 without revoking session", func(t *testing.T) {
		svc, st, idProv, now := newTestAuthHarness(t)
		loginRes, err := svc.Login(t.Context(), LoginInput{Identifier: "admin", Password: "correct-password-123"})
		if err != nil {
			t.Fatalf("Login: %v", err)
		}
		if _, err := svc.Refresh(t.Context(), RefreshInput{RefreshToken: loginRes.RefreshToken}); err != nil {
			t.Fatalf("Refresh: %v", err)
		}

		*now = now.Add(10 * time.Second)
		idProv.getByIDErr = apperr.Internal(errors.New("pg connection reset"))
		_, err = svc.Refresh(t.Context(), RefreshInput{RefreshToken: loginRes.RefreshToken})
		appErr, ok := apperr.As(err)
		if !ok || appErr.Status != http.StatusInternalServerError {
			t.Fatalf("transient grace Refresh err = %v, want 500", err)
		}
		for _, sess := range st.sessions {
			if sess.RevokedAt != nil {
				t.Fatal("transient error must not revoke session")
			}
		}
	})

	t.Run("deleted user inside grace window revokes session and returns 401", func(t *testing.T) {
		svc, st, idProv, now := newTestAuthHarness(t)
		loginRes, err := svc.Login(t.Context(), LoginInput{Identifier: "admin", Password: "correct-password-123"})
		if err != nil {
			t.Fatalf("Login: %v", err)
		}
		if _, err := svc.Refresh(t.Context(), RefreshInput{RefreshToken: loginRes.RefreshToken}); err != nil {
			t.Fatalf("Refresh: %v", err)
		}

		*now = now.Add(10 * time.Second)
		clear(idProv.users)
		_, err = svc.Refresh(t.Context(), RefreshInput{RefreshToken: loginRes.RefreshToken})
		appErr, ok := apperr.As(err)
		if !ok || appErr.Status != http.StatusUnauthorized || appErr.Code != apperr.CodeUnauthorized {
			t.Fatalf("deleted user grace Refresh err = %v, want 401 %s", err, apperr.CodeUnauthorized)
		}
		for _, sess := range st.sessions {
			if sess.RevokedAt == nil {
				t.Fatal("deleted user session must be revoked")
			}
		}
	})

	t.Run("natural expiry of historical token returns AUTH_UNAUTHORIZED not AUTH_REFRESH_REUSED", func(t *testing.T) {
		svc, _, _, now := newTestAuthHarness(t)
		loginRes, err := svc.Login(t.Context(), LoginInput{Identifier: "admin", Password: "correct-password-123"})
		if err != nil {
			t.Fatalf("Login: %v", err)
		}
		if _, err := svc.Refresh(t.Context(), RefreshInput{RefreshToken: loginRes.RefreshToken}); err != nil {
			t.Fatalf("Refresh: %v", err)
		}

		*now = now.Add(8 * 24 * time.Hour)
		_, err = svc.Refresh(t.Context(), RefreshInput{RefreshToken: loginRes.RefreshToken})
		appErr, ok := apperr.As(err)
		if !ok || appErr.Status != http.StatusUnauthorized || appErr.Code != apperr.CodeUnauthorized {
			t.Fatalf("expired historical token err = %v, want 401 %s", err, apperr.CodeUnauthorized)
		}
	})

	t.Run("revoked session inside grace returns AUTH_UNAUTHORIZED; outside grace emits replay audit", func(t *testing.T) {
		svc, _, _, now, auditRec := newTestAuthHarnessWithAudit(t)
		loginRes, err := svc.Login(t.Context(), LoginInput{Identifier: "admin", Password: "correct-password-123"})
		if err != nil {
			t.Fatalf("Login: %v", err)
		}
		ref1, err := svc.Refresh(t.Context(), RefreshInput{RefreshToken: loginRes.RefreshToken})
		if err != nil {
			t.Fatalf("Refresh: %v", err)
		}
		// Normal user logout right after rotation.
		if err := svc.Logout(t.Context(), LogoutInput{RefreshToken: ref1.RefreshToken}); err != nil {
			t.Fatalf("Logout: %v", err)
		}

		// Delayed in-flight tab within grace window gets AUTH_UNAUTHORIZED, not theft alert.
		*now = now.Add(10 * time.Second)
		_, err = svc.Refresh(t.Context(), RefreshInput{RefreshToken: loginRes.RefreshToken})
		if appErr, ok := apperr.As(err); !ok || appErr.Code != apperr.CodeUnauthorized {
			t.Fatalf("post-logout grace err = %v, want %s", err, apperr.CodeUnauthorized)
		}

		// Replay outside grace window against already-revoked session still flags AUTH_REFRESH_REUSED and audits.
		*now = now.Add(60 * time.Second)
		beforeAudits := len(auditRec.entries)
		_, err = svc.Refresh(t.Context(), RefreshInput{RefreshToken: loginRes.RefreshToken})
		if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeRefreshReused {
			t.Fatalf("post-revoke replay err = %v, want %s", err, ErrCodeRefreshReused)
		}
		if len(auditRec.entries) != beforeAudits+1 || auditRec.entries[len(auditRec.entries)-1].Action != "auth.refresh_replay" {
			t.Fatalf("expected auth.refresh_replay audit, got %+v", auditRec.entries[beforeAudits:])
		}
	})
}

func TestLogoutHistoricalCookieIdempotencyAndAuditHygiene(t *testing.T) {
	svc, st, _, _, auditRec := newTestAuthHarnessWithAudit(t)
	loginRes, err := svc.Login(t.Context(), LoginInput{Identifier: "admin", Password: "correct-password-123"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	// Rotate token so loginRes.RefreshToken moves to refresh_token_history.
	ref1, err := svc.Refresh(t.Context(), RefreshInput{RefreshToken: loginRes.RefreshToken})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// Logout with stale historical cookie must still revoke the underlying session.
	if err := svc.Logout(t.Context(), LogoutInput{RefreshToken: loginRes.RefreshToken}); err != nil {
		t.Fatalf("Logout(historical): %v", err)
	}
	if _, err := svc.Refresh(t.Context(), RefreshInput{RefreshToken: ref1.RefreshToken}); err == nil {
		t.Fatal("expected session revoked via historical cookie to reject latest token")
	}

	// Repeating logout with valid JWT or already-revoked cookie must NOT emit duplicate auth.logout audit logs.
	before := len(auditRec.entries)
	if err := svc.Logout(t.Context(), LogoutInput{
		RefreshToken: ref1.RefreshToken,
		AccessToken:  ref1.AccessToken,
	}); err != nil {
		t.Fatalf("repeat Logout: %v", err)
	}
	if len(auditRec.entries) != before {
		t.Fatalf("repeat logout emitted %d extra audit entries, want 0", len(auditRec.entries)-before)
	}

	// Normal web logout with both current cookie and matching AccessToken must not call RevokeSession twice.
	login2, err := svc.Login(t.Context(), LoginInput{Identifier: "admin", Password: "correct-password-123"})
	if err != nil {
		t.Fatalf("Login(2): %v", err)
	}
	st.revokeByIDCalls = 0
	if err := svc.Logout(t.Context(), LogoutInput{
		RefreshToken: login2.RefreshToken,
		AccessToken:  login2.AccessToken,
	}); err != nil {
		t.Fatalf("Logout(2): %v", err)
	}
	if st.revokeByIDCalls != 0 {
		t.Fatalf("RevokeSession called %d times when RefreshToken already revoked same session, want 0", st.revokeByIDCalls)
	}

	// Logout with AccessToken alone revokes the session.
	login3, err := svc.Login(t.Context(), LoginInput{Identifier: "admin", Password: "correct-password-123"})
	if err != nil {
		t.Fatalf("Login(3): %v", err)
	}
	if err := svc.Logout(t.Context(), LogoutInput{AccessToken: login3.AccessToken}); err != nil {
		t.Fatalf("Logout(bearer-only): %v", err)
	}
	if _, err := svc.Refresh(t.Context(), RefreshInput{RefreshToken: login3.RefreshToken}); err == nil {
		t.Fatal("expected bearer-only logout to revoke session")
	}
}

// TestLogoutRevokeFailureRollsBack verifies a store-level revocation failure aborts the transaction,
// surfaces a 500, and writes no audit entry (no partial success).
func TestLogoutRevokeFailureRollsBack(t *testing.T) {
	svc, st, _, _ := newTestAuthHarness(t)
	auditRec := &fakeAuditRecorder{}
	svc.audit = auditRec

	loginRes, err := svc.Login(t.Context(), LoginInput{Identifier: "admin", Password: "correct-password-123"})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	st.revokeErr = errors.New("simulated db failure")
	err = svc.Logout(t.Context(), LogoutInput{RefreshToken: loginRes.RefreshToken})
	appErr, ok := apperr.As(err)
	if !ok || appErr.Status != http.StatusInternalServerError {
		t.Fatalf("Logout() err = %v, want 500", err)
	}
	// No audit entry should be emitted for a rolled-back logout.
	for _, e := range auditRec.entries {
		if e.Action == "auth.logout" {
			t.Fatalf("unexpected auth.logout audit on rolled-back logout: %+v", e)
		}
	}
	// Session must remain active because the transaction rolled back.
	st.revokeErr = nil
	if _, err := svc.Refresh(t.Context(), RefreshInput{RefreshToken: loginRes.RefreshToken}); err != nil {
		t.Fatalf("session should still be active after rolled-back logout, got %v", err)
	}
}

func TestMeAndServiceErrorWrappingEdgeCases(t *testing.T) {
	svc, st, idProv, now := newTestAuthHarness(t)
	var adminID uuid.UUID
	for id := range idProv.users {
		adminID = id
	}

	// Establish a real session so Me()'s active-session guard passes for the happy path.
	login, err := svc.Login(t.Context(), LoginInput{Identifier: "admin", Password: "correct-password-123", ClientIP: "10.0.0.1"})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	claims, err := svc.jwt.Verify(login.AccessToken)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	sessionID := claims.SessionID

	// Nil roles/permissions normalize to non-nil empty slices, and avatar signing failure degrades to nil.
	avatarID := uuid.Must(uuid.NewV7())
	u := idProv.users[adminID]
	u.User.AvatarFileID = &avatarID
	idProv.users[adminID] = u
	svc.access = &fakeAccessProvider{access: UserAccess{Roles: nil, Permissions: nil}}
	svc.avatarSigner = fakeAvatarSigner{err: errors.New("signer down")}

	meRes, err := svc.Me(t.Context(), adminID, sessionID)
	if err != nil {
		t.Fatalf("Me() error = %v", err)
	}
	if meRes.Roles == nil || meRes.Permissions == nil || meRes.User.AvatarURL != nil {
		t.Fatalf("unexpected Me result: %+v", meRes)
	}

	// Revoked session returns 401 AUTH_UNAUTHORIZED even for an active user.
	if _, _, revErr := st.RevokeSession(t.Context(), sessionID); revErr != nil {
		t.Fatalf("RevokeSession() error = %v", revErr)
	}
	if _, err := svc.Me(t.Context(), adminID, sessionID); err == nil {
		t.Fatal("expected 401 for revoked session")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusUnauthorized {
		t.Fatalf("Me(revoked session) err = %v, want 401", err)
	}
	// Re-establish an active session for the remaining assertions.
	login, err = svc.Login(t.Context(), LoginInput{Identifier: "admin", Password: "correct-password-123", ClientIP: "10.0.0.2"})
	if err != nil {
		t.Fatalf("Login(2) error = %v", err)
	}
	claims, err = svc.jwt.Verify(login.AccessToken)
	if err != nil {
		t.Fatalf("Verify(2) error = %v", err)
	}
	sessionID = claims.SessionID

	// Disabled user returns 403 AUTH_ACCOUNT_DISABLED.
	u.User.Status = userStatusDisabled
	idProv.users[adminID] = u
	if _, err := svc.Me(t.Context(), adminID, sessionID); err == nil {
		t.Fatal("expected 403 for disabled user")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeAccountDisabled {
		t.Fatalf("Me(disabled) err = %v, want %s", err, ErrCodeAccountDisabled)
	}

	// Deleted/missing user returns 401 AUTH_UNAUTHORIZED.
	if _, err := svc.Me(t.Context(), uuid.Must(uuid.NewV7()), sessionID); err == nil {
		t.Fatal("expected 401 for missing user")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusUnauthorized {
		t.Fatalf("Me(missing) err = %v, want 401", err)
	}

	// uuid.Nil fast paths on session active checks.
	if ok, err := svc.IsSessionActive(t.Context(), uuid.Nil, uuid.Must(uuid.NewV7())); err != nil || ok {
		t.Fatalf("IsSessionActive(nil) = (%v, %v), want (false, nil)", ok, err)
	}
	if ok, err := svc.LockActiveSessionForShare(t.Context(), adminID, uuid.Nil); err != nil || ok {
		t.Fatalf("LockActiveSessionForShare(nil) = (%v, %v), want (false, nil)", ok, err)
	}

	// PurgeExpiredSessions multi-batch pagination and wrapped context cancellation error.
	svc.purgeBatchSize = 2
	for i := range 5 {
		sid := uuid.Must(uuid.NewV7())
		st.sessions[sid] = sessionRecord{
			ID:        sid,
			UserID:    adminID,
			ExpiresAt: now.Add(-10 * 24 * time.Hour),
		}
		st.history[sid.String()] = refreshHistoryRecord{
			TokenHash:  sid.String(),
			SessionID:  sid,
			ReplacedAt: now.Add(-11 * 24 * time.Hour),
			ExpiresAt:  now.Add(-10 * 24 * time.Hour),
		}
		_ = i
	}
	deleted, err := svc.PurgeExpiredSessions(t.Context(), 7*24*time.Hour)
	if err != nil || deleted != 10 {
		t.Fatalf("PurgeExpiredSessions multi-batch = (%d, %v), want (10, nil)", deleted, err)
	}

	canceledCtx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = svc.PurgeExpiredSessions(canceledCtx, 7*24*time.Hour)
	if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusInternalServerError || !errors.Is(err, context.Canceled) {
		t.Fatalf("PurgeExpiredSessions(canceled) = %v, want *apperr.Error wrapping context.Canceled", err)
	}

	// withinTx wraps raw TxRunner errors into *apperr.Error.
	svc.tx = fakeTx{st: st, failOnOuter: true, outerErr: errors.New("begin tx failed")}
	err = svc.withinTx(t.Context(), func(context.Context) error { return nil })
	if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusInternalServerError {
		t.Fatalf("withinTx outer error = %v, want 500 *apperr.Error", err)
	}
}

func TestLoginDisabledUserReturns403(t *testing.T) {
	svc, _, idProvider, _ := newTestAuthHarness(t)
	for id, u := range idProvider.users {
		u.User.Status = userStatusDisabled
		idProvider.users[id] = u
	}

	_, err := svc.Login(t.Context(), LoginInput{
		Identifier: "admin",
		Password:   "correct-password-123",
		ClientIP:   "127.0.0.1",
	})
	appErr, ok := apperr.As(err)
	if !ok || appErr.Status != http.StatusForbidden || appErr.Code != ErrCodeAccountDisabled {
		t.Fatalf("disabled login err = %v, want 403 %s", err, ErrCodeAccountDisabled)
	}
}

func TestAuthEmitsAuditEvents(t *testing.T) {
	svc, _, _, now, auditRec := newTestAuthHarnessWithAudit(t)

	// Failed login writes failure audit without exposing password.
	_, err := svc.Login(t.Context(), LoginInput{
		Identifier: "admin",
		Password:   "wrong-password-123",
		ClientIP:   "10.0.0.1",
		UserAgent:  "ua-test",
	})
	if err == nil {
		t.Fatal("expected login error")
	}

	// Successful login writes success audit.
	res, err := svc.Login(t.Context(), LoginInput{
		Identifier: "admin",
		Password:   "correct-password-123",
		ClientIP:   "10.0.0.1",
		UserAgent:  "ua-test",
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	// Rotate once, advance past grace window, then replay -> emits auth.refresh_replay failure audit.
	_, err = svc.Refresh(t.Context(), RefreshInput{RefreshToken: res.RefreshToken, ClientIP: "10.0.0.1"})
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	*now = now.Add(60 * time.Second)
	_, _ = svc.Refresh(t.Context(), RefreshInput{RefreshToken: res.RefreshToken, ClientIP: "10.0.0.2"})

	// Logout on a fresh session emits auth.logout success audit.
	res2, err := svc.Login(t.Context(), LoginInput{
		Identifier: "admin",
		Password:   "correct-password-123",
		ClientIP:   "10.0.0.3",
	})
	if err != nil {
		t.Fatalf("second Login() error = %v", err)
	}
	if err := svc.Logout(t.Context(), LogoutInput{
		RefreshToken: res2.RefreshToken,
		ClientIP:     "10.0.0.3",
		UserAgent:    "ua-logout",
	}); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}

	var gotActions []string
	for _, entry := range auditRec.entries {
		gotActions = append(gotActions, entry.Action+":"+entry.Status)
		if entry.Metadata != nil {
			if _, hasPassword := entry.Metadata["password"]; hasPassword {
				t.Fatalf("audit metadata must never include password: %+v", entry.Metadata)
			}
		}
	}
	wantPrefix := []string{
		"auth.login:failure",
		"auth.login:success",
		"auth.refresh_replay:failure",
		"auth.login:success",
		"auth.logout:success",
	}
	if len(gotActions) != len(wantPrefix) {
		t.Fatalf("audit actions = %v, want %v", gotActions, wantPrefix)
	}
	for i := range wantPrefix {
		if gotActions[i] != wantPrefix[i] {
			t.Fatalf("audit[%d] = %q, want %q (all=%v)", i, gotActions[i], wantPrefix[i], gotActions)
		}
	}
	// Replay audit must still have ActorUsername populated post-commit.
	if auditRec.entries[2].ActorUsername != "admin" {
		t.Fatalf("replay audit ActorUsername = %q, want admin", auditRec.entries[2].ActorUsername)
	}
}

func TestAuthLocalesComplete(t *testing.T) {
	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle() error = %v", err)
	}
	if err := bundle.RegisterFS("auth", LocalesFS, "locales"); err != nil {
		t.Fatalf("RegisterFS(auth) error = %v", err)
	}
	if err := bundle.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	// Every declared auth error code must resolve to a real translation in both shipped languages;
	// a missing key would otherwise silently fall back to the raw code at runtime.
	for _, lang := range []string{"zh-CN", "en-US"} {
		for _, code := range authErrorCodes {
			got := bundle.Translate(lang, code, nil)
			if got == "" || got == code {
				t.Fatalf("auth error code %q missing %s translation (got %q)", code, lang, got)
			}
		}
	}
}

type fakeAvatarSigner struct {
	urls map[uuid.UUID]string
	err  error
}

func (f fakeAvatarSigner) SignAvatarURL(_ context.Context, fileID uuid.UUID) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.urls[fileID], nil
}

func TestActiveSessionsRevokePurgeAndAvatarURL(t *testing.T) {
	svc, st, idProv, clock := newTestAuthHarness(t)

	var adminID uuid.UUID
	avatarID := uuid.Must(uuid.NewV7())
	for id, u := range idProv.users {
		adminID = id
		u.User.AvatarFileID = &avatarID
		idProv.users[id] = u
	}
	svc.avatarSigner = fakeAvatarSigner{
		urls: map[uuid.UUID]string{
			avatarID: "/api/v1/files/signed/avatar-token",
		},
	}

	login1, err := svc.Login(t.Context(), LoginInput{
		Identifier: "admin",
		Password:   "correct-password-123",
		ClientIP:   "10.0.0.1",
		UserAgent:  "BrowserA/1.0",
	})
	if err != nil {
		t.Fatalf("Login(1) error = %v", err)
	}
	if login1.User.AvatarURL == nil || *login1.User.AvatarURL != "/api/v1/files/signed/avatar-token" {
		t.Fatalf("Login(1) AvatarURL = %v, want signed URL", login1.User.AvatarURL)
	}
	claims1, err := svc.jwt.Verify(login1.AccessToken)
	if err != nil {
		t.Fatalf("Verify(1): %v", err)
	}

	meRes, err := svc.Me(t.Context(), adminID, claims1.SessionID)
	if err != nil {
		t.Fatalf("Me() error = %v", err)
	}
	if meRes.User.AvatarURL == nil || *meRes.User.AvatarURL != "/api/v1/files/signed/avatar-token" {
		t.Fatalf("Me() AvatarURL = %v, want signed URL", meRes.User.AvatarURL)
	}

	login2, err := svc.Login(t.Context(), LoginInput{
		Identifier: "admin",
		Password:   "correct-password-123",
		ClientIP:   "10.0.0.2",
		UserAgent:  "BrowserB/2.0",
	})
	if err != nil {
		t.Fatalf("Login(2) error = %v", err)
	}
	claims2, err := svc.jwt.Verify(login2.AccessToken)
	if err != nil {
		t.Fatalf("Verify(2): %v", err)
	}

	login3, err := svc.Login(t.Context(), LoginInput{
		Identifier: "admin",
		Password:   "correct-password-123",
		ClientIP:   "10.0.0.3",
		UserAgent:  "BrowserC/3.0",
	})
	if err != nil {
		t.Fatalf("Login(3) error = %v", err)
	}

	sessions, err := svc.ListActiveUserSessions(t.Context(), adminID)
	if err != nil {
		t.Fatalf("ListActiveUserSessions: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("active sessions = %d, want 3", len(sessions))
	}

	// Cross-user revocation must return false and leave session untouched.
	ok, err := svc.RevokeUserSession(t.Context(), uuid.Must(uuid.NewV7()), claims2.SessionID)
	if err != nil || ok {
		t.Fatalf("cross-user RevokeUserSession = (%v, %v), want (false, nil)", ok, err)
	}

	// Revoking session 2 immediately invalidates session 2 refresh token.
	ok, err = svc.RevokeUserSession(t.Context(), adminID, claims2.SessionID)
	if err != nil || !ok {
		t.Fatalf("RevokeUserSession(2) = (%v, %v), want (true, nil)", ok, err)
	}
	if _, err := svc.Refresh(t.Context(), RefreshInput{RefreshToken: login2.RefreshToken}); err == nil {
		t.Fatal("expected revoked session 2 refresh to fail")
	}

	// RevokeOtherUserSessions keeps session 1 and revokes session 3.
	revokedOthers, err := svc.RevokeOtherUserSessions(t.Context(), adminID, claims1.SessionID)
	if err != nil || revokedOthers != 1 {
		t.Fatalf("RevokeOtherUserSessions = (%d, %v), want (1, nil)", revokedOthers, err)
	}
	if _, err := svc.Refresh(t.Context(), RefreshInput{RefreshToken: login3.RefreshToken}); err == nil {
		t.Fatal("expected session 3 refresh to fail after RevokeOtherUserSessions")
	}
	if _, err := svc.Refresh(t.Context(), RefreshInput{RefreshToken: login1.RefreshToken}); err != nil {
		t.Fatalf("expected session 1 refresh to stay valid, got %v", err)
	}

	// RevokeAllUserSessions revokes remaining active sessions.
	if err := svc.RevokeAllUserSessions(t.Context(), adminID); err != nil {
		t.Fatalf("RevokeAllUserSessions: %v", err)
	}
	remaining, err := svc.ListActiveUserSessions(t.Context(), adminID)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("remaining active sessions = (%d, %v), want (0, nil)", len(remaining), err)
	}

	// PurgeExpiredSessions removes only records expired past retention.
	expiredID := uuid.Must(uuid.NewV7())
	st.sessions[expiredID] = sessionRecord{
		ID:               expiredID,
		UserID:           adminID,
		RefreshTokenHash: "old-hash",
		ExpiresAt:        clock.Add(-10 * 24 * time.Hour),
		CreatedAt:        clock.Add(-17 * 24 * time.Hour),
		LastUsedAt:       clock.Add(-17 * 24 * time.Hour),
	}
	st.history["old-hist-hash"] = refreshHistoryRecord{
		TokenHash:  "old-hist-hash",
		SessionID:  expiredID,
		ReplacedAt: clock.Add(-11 * 24 * time.Hour),
		ExpiresAt:  clock.Add(-10 * 24 * time.Hour),
	}
	// Retention <= 0 disables cleanup without deleting any rows.
	if deleted, err := svc.PurgeExpiredSessions(t.Context(), 0); err != nil || deleted != 0 {
		t.Fatalf("PurgeExpiredSessions(0) = (%d, %v), want (0, nil)", deleted, err)
	}
	if _, exists := st.sessions[expiredID]; !exists {
		t.Fatal("expected expired session to remain when retention is 0")
	}

	deleted, err := svc.PurgeExpiredSessions(t.Context(), 7*24*time.Hour)
	if err != nil || deleted != 2 {
		t.Fatalf("PurgeExpiredSessions = (%d, %v), want (2, nil)", deleted, err)
	}
	if _, exists := st.sessions[expiredID]; exists {
		t.Fatal("expected expired session to be purged")
	}
}

// TestAccountRateLimiterDeterministicEviction ensures capacity eviction drops the oldest window,
// not an arbitrary bucket, so identifier spraying cannot randomly clear a target's counter.
func TestAccountRateLimiterDeterministicEviction(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	// minCap keeps the test cheap while exercising the eviction branch at the configured cap.
	minCap := 3
	lim := newAccountRateLimiter(2, time.Minute)
	lim.capOverride = minCap

	// Seed distinct keys with strictly increasing reset times; "oldest" resets earliest.
	for i := range minCap {
		lim.buckets["ident:user-"+strconv.Itoa(i)] = accountRateBucket{
			count:   1,
			resetAt: now.Add(time.Duration(i+1) * time.Second),
		}
	}

	// Admitting a new key at capacity must evict the earliest-resetting bucket (ident:user-0).
	lim.allow("ident:new-key", now)
	if _, exists := lim.buckets["ident:user-0"]; exists {
		t.Fatal("expected oldest bucket (ident:user-0) to be evicted deterministically")
	}
	if _, exists := lim.buckets["ident:new-key"]; !exists {
		t.Fatal("expected new key to be admitted after eviction")
	}
	if len(lim.buckets) != minCap {
		t.Fatalf("bucket count = %d, want %d after eviction", len(lim.buckets), minCap)
	}
}
