package auth

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

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
	sessions map[uuid.UUID]sessionRecord
	history  map[string]refreshHistoryRecord
	now      func() time.Time
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
	expiresAt time.Time,
) error {
	f.history[tokenHash] = refreshHistoryRecord{
		TokenHash:  tokenHash,
		SessionID:  sessionID,
		ReplacedAt: f.now(),
		ExpiresAt:  expiresAt,
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

func (f *fakeAuthStore) RevokeSession(_ context.Context, sessionID uuid.UUID) error {
	if s, ok := f.sessions[sessionID]; ok && s.RevokedAt == nil {
		t := f.now()
		s.RevokedAt = &t
		f.sessions[sessionID] = s
	}
	return nil
}

func (f *fakeAuthStore) RevokeSessionByRefreshHash(_ context.Context, tokenHash string) error {
	for id, s := range f.sessions {
		if s.RefreshTokenHash == tokenHash && s.RevokedAt == nil {
			t := f.now()
			s.RevokedAt = &t
			f.sessions[id] = s
		}
	}
	return nil
}

func (f *fakeAuthStore) RevokeAllUserSessions(_ context.Context, userID uuid.UUID) error {
	for id, s := range f.sessions {
		if s.UserID == userID && s.RevokedAt == nil {
			t := f.now()
			s.RevokedAt = &t
			f.sessions[id] = s
		}
	}
	return nil
}

type fakeIdentityProvider struct {
	users map[uuid.UUID]UserCredentials
}

func (f *fakeIdentityProvider) GetAuthCredentials(_ context.Context, identifier string) (UserCredentials, error) {
	lower := strings.ToLower(strings.TrimSpace(identifier))
	for _, u := range f.users {
		if strings.EqualFold(u.User.Username, lower) || strings.EqualFold(u.User.Email, lower) {
			return u, nil
		}
	}
	return UserCredentials{}, apperr.NotFound("IDENTITY_USER_NOT_FOUND")
}

func (f *fakeIdentityProvider) GetByID(_ context.Context, userID uuid.UUID) (User, error) {
	u, ok := f.users[userID]
	if !ok {
		return User{}, apperr.NotFound("IDENTITY_USER_NOT_FOUND")
	}
	return u.User, nil
}

func (f *fakeIdentityProvider) RecordLogin(_ context.Context, _ uuid.UUID) error { return nil }

func (f *fakeIdentityProvider) UpdatePasswordHash(_ context.Context, userID uuid.UUID, newHash string) error {
	if u, ok := f.users[userID]; ok {
		u.PasswordHash = newHash
		f.users[userID] = u
	}
	return nil
}

type fakeAccessProvider struct {
	access UserAccess
}

func (f *fakeAccessProvider) GetUserAccess(_ context.Context, _ uuid.UUID) (UserAccess, error) {
	return f.access, nil
}

type fakeTx struct {
	st *fakeAuthStore
}

func (f fakeTx) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
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

func newTestAuthHarness(t *testing.T) (*Service, *fakeAuthStore, *fakeIdentityProvider, *time.Time) {
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
	svc := newServiceWithStore(
		config.AuthConfig{
			JWTSecret:       "test-secret-for-auth-service-32-bytes!!",
			AccessTokenTTL:  15 * time.Minute,
			RefreshTokenTTL: 7 * 24 * time.Hour,
		},
		st,
		idProvider,
		&fakeAccessProvider{access: UserAccess{
			Roles:       []string{"super_admin"},
			Permissions: []string{"dashboard:read", "users:read"},
		}},
		hasher,
		jwtMgr,
		fakeTx{st: st},
		mem,
		logger.New("test", "error", io.Discard),
	)
	svc.now = nowFn
	return svc, st, idProvider, &now
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

func TestLoginSoftCooldownDoesNotDisableUser(t *testing.T) {
	svc, _, idProvider, _ := newTestAuthHarness(t)

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

	// 11th attempt must hit 429 soft cooldown even with the correct password.
	_, err := svc.Login(t.Context(), LoginInput{
		Identifier: "admin",
		Password:   "correct-password-123",
		ClientIP:   "198.51.100.5",
	})
	appErr, ok := apperr.As(err)
	if !ok || appErr.Status != http.StatusTooManyRequests || appErr.Code != apperr.CodeRateLimited {
		t.Fatalf("11th attempt err = %v, want 429 %s", err, apperr.CodeRateLimited)
	}

	// User status must remain active (never permanently locked by attacker).
	for _, u := range idProvider.users {
		if u.User.Status != userStatusActive {
			t.Fatalf("user status mutated to %q during soft cooldown", u.User.Status)
		}
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
}
