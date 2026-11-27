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

func (f *fakeAuthStore) RevokeSession(_ context.Context, sessionID uuid.UUID) (revokedSession, bool, error) {
	if s, ok := f.sessions[sessionID]; ok && s.RevokedAt == nil {
		t := f.now()
		s.RevokedAt = &t
		f.sessions[sessionID] = s
		return revokedSession{ID: s.ID, UserID: s.UserID}, true, nil
	}
	return revokedSession{}, false, nil
}

func (f *fakeAuthStore) RevokeSessionByRefreshHash(_ context.Context, tokenHash string) (revokedSession, bool, error) {
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
	users map[uuid.UUID]UserCredentials
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
	u, ok := f.users[userID]
	if !ok {
		return User{}, apperr.NotFound("USERS_NOT_FOUND")
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

type fakeAuditRecorder struct {
	entries []AuditEntry
}

func (f *fakeAuditRecorder) Record(_ context.Context, entry AuditEntry) error {
	f.entries = append(f.entries, entry)
	return nil
}

func newTestAuthHarness(t *testing.T) (*Service, *fakeAuthStore, *fakeIdentityProvider, *time.Time) {
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
	claims1, err := svc.JWTManager().Verify(login1.AccessToken)
	if err != nil {
		t.Fatalf("Verify(1): %v", err)
	}

	meRes, err := svc.Me(t.Context(), adminID)
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
	claims2, err := svc.JWTManager().Verify(login2.AccessToken)
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
