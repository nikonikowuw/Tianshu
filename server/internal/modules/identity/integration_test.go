package identity_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/modules/access"
	"tianshu/internal/modules/audit"
	"tianshu/internal/modules/auth"
	"tianshu/internal/modules/files"
	"tianshu/internal/modules/identity"
	"tianshu/internal/platform/cache"
	"tianshu/internal/platform/config"
	"tianshu/internal/platform/database"
	"tianshu/internal/platform/i18n"
	"tianshu/internal/platform/logger"
	"tianshu/internal/platform/security"
	"tianshu/internal/platform/storage"
	"tianshu/internal/shared/apperr"
	"tianshu/migrations"
)

type accessAdapter struct {
	svc *access.Service
}

func (a *accessAdapter) ResolveRoles(ctx context.Context, ids []uuid.UUID) ([]identity.RoleRef, error) {
	refs, err := a.svc.ResolveRoles(ctx, ids)
	if err != nil {
		return nil, err
	}
	return mapRoleRefs(refs), nil
}

func (a *accessAdapter) ListRolesByUserIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID][]identity.RoleRef, error) {
	m, err := a.svc.ListRolesByUserIDs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID][]identity.RoleRef, len(m))
	for uid, refs := range m {
		out[uid] = mapRoleRefs(refs)
	}
	return out, nil
}

func (a *accessAdapter) ListUserIDsByRoleIDs(ctx context.Context, roleIDs []uuid.UUID) ([]uuid.UUID, error) {
	return a.svc.ListUserIDsByRoleIDs(ctx, roleIDs)
}

func (a *accessAdapter) ListUserIDsByRoleCode(ctx context.Context, code string) ([]uuid.UUID, error) {
	return a.svc.ListUserIDsByRoleCode(ctx, code)
}

func (a *accessAdapter) ReplaceUserRoles(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID) error {
	return a.svc.ReplaceUserRoles(ctx, userID, roleIDs)
}

func mapRoleRefs(in []access.RoleRef) []identity.RoleRef {
	out := make([]identity.RoleRef, 0, len(in))
	for _, r := range in {
		out = append(out, identity.RoleRef{
			ID:       r.ID,
			Code:     r.Code,
			Name:     r.Name,
			NameKey:  r.NameKey,
			IsSystem: r.IsSystem,
		})
	}
	return out
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
			t.Fatalf("postgres ping failed: %v", err)
		}
		t.Skipf("postgres ping failed: %v", err)
	}

	schema := "identity_it_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
		t.Fatalf("migrate schema: %v", err)
	}
	return pool
}

func TestIdentityConcurrentLastSuperAdminAndSoftDeleteRebuild(t *testing.T) {
	pool := openIsolatedTestPool(t)
	tx := database.NewTxManager(pool)
	mem := cache.NewMemory()
	defer func() { _ = mem.Close() }()

	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle: %v", err)
	}
	_ = bundle.RegisterFS("access", access.LocalesFS, "locales")
	_ = bundle.RegisterFS("identity", identity.LocalesFS, "locales")

	hasher, err := security.NewPasswordHasher(security.DefaultArgon2Params())
	if err != nil {
		t.Fatalf("NewPasswordHasher: %v", err)
	}

	accessSvc := access.NewService(pool, access.ServiceDeps{
		Tx:    tx,
		Cache: mem,
		I18n:  bundle,
		Log:   logger.New("test", "error", io.Discard),
	})
	if err := accessSvc.SyncCatalog(t.Context()); err != nil {
		t.Fatalf("SyncCatalog: %v", err)
	}

	idSvc := identity.NewService(pool, hasher, identity.ServiceDeps{
		Tx:    tx,
		Roles: &accessAdapter{svc: accessSvc},
		I18n:  bundle,
		Log:   logger.New("test", "error", io.Discard),
	})

	u1, err := idSvc.CreateInitialAdmin(t.Context(), identity.CreateInitialAdminInput{
		Username: "admin1",
		Email:    "admin1@example.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("CreateInitialAdmin(1): %v", err)
	}
	if err := accessSvc.AssignRoleByCode(t.Context(), u1.ID, access.RoleCodeSuperAdmin); err != nil {
		t.Fatalf("AssignRoleByCode(1): %v", err)
	}

	opts, err := accessSvc.ListRoleOptions(t.Context())
	if err != nil {
		t.Fatalf("ListRoleOptions: %v", err)
	}
	var superRoleID uuid.UUID
	for _, o := range opts {
		if o.Code == access.RoleCodeSuperAdmin {
			superRoleID = o.ID
		}
	}

	u2, err := idSvc.CreateUser(t.Context(), identity.CreateUserInput{
		Username: "admin2",
		Email:    "admin2@example.com",
		Password: "password123",
		RoleIDs:  []uuid.UUID{superRoleID},
		ActorID:  &u1.ID,
	})
	if err != nil {
		t.Fatalf("CreateUser(admin2): %v", err)
	}

	// Concurrent disable: u2 disables u1, u1 disables u2 -> exactly one succeeds, one gets USERS_LAST_SUPER_ADMIN.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	disabled := identity.StatusDisabled
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = idSvc.UpdateUser(t.Context(), identity.UpdateUserInput{UserID: u1.ID, Status: &disabled, ActorID: &u2.ID})
	}()
	go func() {
		defer wg.Done()
		_, errs[1] = idSvc.UpdateUser(t.Context(), identity.UpdateUserInput{UserID: u2.ID, Status: &disabled, ActorID: &u1.ID})
	}()
	wg.Wait()

	var okCount, lastSuperCount int
	for _, e := range errs {
		if e == nil {
			okCount++
		} else if appErr, ok := apperr.As(e); ok && appErr.Code == identity.ErrCodeLastSuperAdmin {
			lastSuperCount++
		} else {
			t.Fatalf("unexpected concurrent error: %v", e)
		}
	}
	if okCount != 1 || lastSuperCount != 1 {
		t.Fatalf("expected 1 success and 1 %s, got ok=%d lastSuper=%d", identity.ErrCodeLastSuperAdmin, okCount, lastSuperCount)
	}

	// Soft-delete an ordinary user and recreate with the same username & email.
	alice, err := idSvc.CreateUser(t.Context(), identity.CreateUserInput{
		Username: "alice",
		Email:    "alice@example.com",
		Password: "password123",
		ActorID:  &u1.ID,
	})
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	if err := idSvc.DeleteUser(t.Context(), identity.DeleteUserInput{UserID: alice.ID, ActorID: &u1.ID}); err != nil {
		t.Fatalf("delete alice: %v", err)
	}
	alice2, err := idSvc.CreateUser(t.Context(), identity.CreateUserInput{
		Username: "alice",
		Email:    "alice@example.com",
		Password: "password123",
		ActorID:  &u1.ID,
	})
	if err != nil {
		t.Fatalf("recreate alice after soft delete: %v", err)
	}
	if alice2.ID == alice.ID {
		t.Fatalf("expected new UUIDv7 for recreated user")
	}
}

type authSessionAdapter struct {
	svc *auth.Service
}

func (a *authSessionAdapter) RevokeAllUserSessions(ctx context.Context, userID uuid.UUID) error {
	return a.svc.RevokeAllUserSessions(ctx, userID)
}

func (a *authSessionAdapter) ListActiveUserSessions(ctx context.Context, userID uuid.UUID) ([]identity.UserSession, error) {
	items, err := a.svc.ListActiveUserSessions(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]identity.UserSession, 0, len(items))
	for _, s := range items {
		out = append(out, identity.UserSession{
			ID:         s.ID,
			IPAddress:  s.IPAddress,
			UserAgent:  s.UserAgent,
			LastUsedAt: s.LastUsedAt,
			CreatedAt:  s.CreatedAt,
			ExpiresAt:  s.ExpiresAt,
		})
	}
	return out, nil
}

func (a *authSessionAdapter) IsSessionActive(ctx context.Context, userID, sessionID uuid.UUID) (bool, error) {
	return a.svc.IsSessionActive(ctx, userID, sessionID)
}

func (a *authSessionAdapter) LockActiveSessionForShare(ctx context.Context, userID, sessionID uuid.UUID) (bool, error) {
	return a.svc.LockActiveSessionForShare(ctx, userID, sessionID)
}

func (a *authSessionAdapter) RevokeUserSession(ctx context.Context, userID, sessionID uuid.UUID) (bool, error) {
	return a.svc.RevokeUserSession(ctx, userID, sessionID)
}

func (a *authSessionAdapter) RevokeOtherUserSessions(ctx context.Context, userID, keepSessionID uuid.UUID) (int64, error) {
	return a.svc.RevokeOtherUserSessions(ctx, userID, keepSessionID)
}

type filesAvatarAdapter struct {
	svc *files.Service
}

func (a *filesAvatarAdapter) ValidateAndLockAvatarFile(ctx context.Context, fileID, ownerID uuid.UUID) error {
	return a.svc.ValidateAndLockAvatarFile(ctx, fileID, ownerID)
}

func (a *filesAvatarAdapter) SignAvatarURL(ctx context.Context, fileID uuid.UUID) (string, error) {
	return a.svc.SignAvatarURL(ctx, fileID)
}

type authIdentityBridge struct {
	svc *identity.Service
}

func (b *authIdentityBridge) GetAuthCredentials(ctx context.Context, identifier string) (auth.UserCredentials, error) {
	creds, err := b.svc.GetAuthCredentials(ctx, identifier)
	if err != nil {
		return auth.UserCredentials{}, err
	}
	return auth.UserCredentials{
		User: auth.User{
			ID:           creds.ID,
			Username:     creds.Username,
			Email:        creds.Email,
			DisplayName:  creds.DisplayName,
			AvatarFileID: creds.AvatarFileID,
			Status:       creds.Status,
		},
		PasswordHash: creds.PasswordHash,
	}, nil
}

func (b *authIdentityBridge) GetByID(ctx context.Context, userID uuid.UUID) (auth.User, error) {
	u, err := b.svc.GetByID(ctx, userID)
	if err != nil {
		return auth.User{}, err
	}
	return auth.User{
		ID:           u.ID,
		Username:     u.Username,
		Email:        u.Email,
		DisplayName:  u.DisplayName,
		AvatarFileID: u.AvatarFileID,
		Status:       u.Status,
	}, nil
}

func (b *authIdentityBridge) RecordLogin(ctx context.Context, userID uuid.UUID) error {
	return b.svc.RecordLogin(ctx, userID)
}

func (b *authIdentityBridge) UpdatePasswordHash(ctx context.Context, userID uuid.UUID, newHash string) error {
	return b.svc.UpdatePasswordHash(ctx, userID, newHash)
}

type authAccessBridge struct {
	svc *access.Service
}

func (b *authAccessBridge) GetUserAccess(ctx context.Context, userID uuid.UUID) (auth.UserAccess, error) {
	ua, err := b.svc.GetUserAccess(ctx, userID)
	if err != nil {
		return auth.UserAccess{}, err
	}
	return auth.UserAccess{Roles: ua.Roles, Permissions: ua.Permissions}, nil
}

type filesIdentityBridge struct {
	svc *identity.Service
}

func (b *filesIdentityBridge) GetUsername(ctx context.Context, userID uuid.UUID) (string, error) {
	u, err := b.svc.GetByID(ctx, userID)
	if err != nil {
		return "", err
	}
	return u.Username, nil
}

func (b *filesIdentityBridge) ListUsernamesByIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]string, error) {
	return b.svc.ListUsernamesByIDs(ctx, userIDs)
}

func (b *filesIdentityBridge) IsAvatarFileInUse(ctx context.Context, fileID uuid.UUID) (bool, error) {
	return b.svc.IsAvatarFileInUse(ctx, fileID)
}

type identityAuditBridge struct {
	svc *audit.Service
}

func (b *identityAuditBridge) Record(ctx context.Context, entry identity.AuditEntry) error {
	return b.svc.Record(ctx, audit.RecordInput{
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

func makeSmallPNGBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 16 {
			img.Set(x, y, color.RGBA{R: 64, G: 128, B: 220, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestProfileAvatarLockPasswordAndSessionInvalidationPostgres(t *testing.T) {
	pool := openIsolatedTestPool(t)
	ctx := t.Context()
	log := logger.New("test", "error", io.Discard)
	tx := database.NewTxManager(pool)
	mem := cache.NewMemory()
	defer func() { _ = mem.Close() }()

	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle: %v", err)
	}
	_ = bundle.RegisterFS("access", access.LocalesFS, "locales")
	_ = bundle.RegisterFS("identity", identity.LocalesFS, "locales")
	_ = bundle.RegisterFS("auth", auth.LocalesFS, "locales")
	_ = bundle.RegisterFS("files", files.LocalesFS, "locales")

	hasher, err := security.NewPasswordHasher(security.DefaultArgon2Params())
	if err != nil {
		t.Fatalf("NewPasswordHasher: %v", err)
	}
	jwtMgr, err := security.NewJWTManager("integration-test-jwt-secret-at-least-32-bytes", 15*time.Minute)
	if err != nil {
		t.Fatalf("NewJWTManager: %v", err)
	}

	auditSvc := audit.NewService(pool, log)
	sessAdapter := &authSessionAdapter{}
	filesAdapter := &filesAvatarAdapter{}
	idSvc := identity.NewService(pool, hasher, identity.ServiceDeps{
		Tx:       tx,
		Sessions: sessAdapter,
		Files:    filesAdapter,
		Audit:    &identityAuditBridge{svc: auditSvc},
		I18n:     bundle,
		Log:      log,
	})

	accessSvc := access.NewService(pool, access.ServiceDeps{
		Tx:    tx,
		Cache: mem,
		I18n:  bundle,
		Log:   log,
	})
	if err := accessSvc.SyncCatalog(ctx); err != nil {
		t.Fatalf("SyncCatalog: %v", err)
	}

	localStore, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	reg, err := storage.NewRegistry(storage.DriverLocal, localStore)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	fileBridge := &filesIdentityBridge{svc: idSvc}
	fileSvc := files.NewService(pool, reg, files.ServiceConfig{
		SignedURLSecret: "integration-test-signed-secret-at-least-32b",
		SignedURLTTL:    5 * time.Minute,
	}, files.ServiceDeps{
		Tx:        tx,
		Actors:    fileBridge,
		AvatarRef: fileBridge,
		Log:       log,
	})
	filesAdapter.svc = fileSvc

	authSvc := auth.NewService(
		config.Default().Auth,
		pool,
		auth.ServiceDeps{
			Identity:     &authIdentityBridge{svc: idSvc},
			Access:       &authAccessBridge{svc: accessSvc},
			Hasher:       hasher,
			JWT:          jwtMgr,
			Tx:           tx,
			Cache:        mem,
			AvatarSigner: filesAdapter,
			Log:          log,
		},
	)
	sessAdapter.svc = authSvc

	user, err := idSvc.CreateInitialAdmin(ctx, identity.CreateInitialAdminInput{
		Username:    "profileuser",
		Email:       "profile@example.com",
		DisplayName: "Profile User",
		Password:    "OldPassword123",
	})
	if err != nil {
		t.Fatalf("CreateInitialAdmin: %v", err)
	}

	// 1. Login device 1, upload avatar, bind to profile (FOR SHARE lock), verify FILES_IN_USE protection & auth /me signed URL.
	login1, err := authSvc.Login(ctx, auth.LoginInput{
		Identifier: "profileuser",
		Password:   "OldPassword123",
		ClientIP:   "10.0.0.1",
		UserAgent:  "DeviceOne",
	})
	if err != nil {
		t.Fatalf("Login(1): %v", err)
	}
	claims1, err := jwtMgr.Verify(login1.AccessToken)
	if err != nil {
		t.Fatalf("Verify(1): %v", err)
	}

	avatarFile, err := fileSvc.UploadAvatar(ctx, files.UploadInput{
		Filename:   "avatar.png",
		Reader:     bytes.NewReader(makeSmallPNGBytes(t)),
		UploaderID: &user.ID,
	})
	if err != nil {
		t.Fatalf("UploadAvatar: %v", err)
	}

	prof, err := idSvc.UpdateProfile(ctx, identity.UpdateProfileInput{
		UserID:           user.ID,
		CurrentSessionID: claims1.SessionID,
		AvatarFileID:     &avatarFile.ID,
	})
	if err != nil {
		t.Fatalf("UpdateProfile bind avatar: %v", err)
	}
	if prof.AvatarURL == nil || !strings.HasPrefix(*prof.AvatarURL, "/api/v1/files/signed/") {
		t.Fatalf("expected signed avatarUrl, got %v", prof.AvatarURL)
	}

	// Bound avatar file cannot be deleted while referenced.
	if err := fileSvc.Delete(ctx, files.DeleteInput{FileID: avatarFile.ID, ActorID: &user.ID}); err == nil {
		t.Fatal("expected FILES_IN_USE deleting bound avatar")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Code != files.ErrCodeInUse {
		t.Fatalf("expected %s, got %v", files.ErrCodeInUse, err)
	}

	// /auth/me includes avatarUrl.
	me, err := authSvc.Me(ctx, user.ID)
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if me.User.AvatarURL == nil || !strings.HasPrefix(*me.User.AvatarURL, "/api/v1/files/signed/") {
		t.Fatalf("Me() AvatarURL = %v, want signed URL", me.User.AvatarURL)
	}

	// Clear avatar releases reference so file deletion succeeds.
	if _, err := idSvc.UpdateProfile(ctx, identity.UpdateProfileInput{
		UserID:           user.ID,
		CurrentSessionID: claims1.SessionID,
		ClearAvatar:      true,
	}); err != nil {
		t.Fatalf("UpdateProfile clear avatar: %v", err)
	}
	if err := fileSvc.Delete(ctx, files.DeleteInput{FileID: avatarFile.ID, ActorID: &user.ID}); err != nil {
		t.Fatalf("Delete released avatar: %v", err)
	}

	// 2. Multi-device sessions: kick device 2 -> refresh fails AND device 2 gets 401 on all /profile* endpoints.
	login2, err := authSvc.Login(ctx, auth.LoginInput{
		Identifier: "profileuser",
		Password:   "OldPassword123",
		ClientIP:   "10.0.0.2",
		UserAgent:  "DeviceTwo",
	})
	if err != nil {
		t.Fatalf("Login(2): %v", err)
	}
	claims2, err := jwtMgr.Verify(login2.AccessToken)
	if err != nil {
		t.Fatalf("Verify(2): %v", err)
	}

	sessions, err := idSvc.ListProfileSessions(ctx, user.ID, claims1.SessionID)
	if err != nil || len(sessions) != 2 || !sessions[0].IsCurrent {
		t.Fatalf("ListProfileSessions = (%+v, %v), want 2 with current first", sessions, err)
	}

	if err := idSvc.RevokeProfileSession(ctx, identity.RevokeSessionInput{
		UserID:           user.ID,
		CurrentSessionID: claims1.SessionID,
		SessionID:        claims2.SessionID,
	}); err != nil {
		t.Fatalf("RevokeProfileSession(2): %v", err)
	}
	if _, err := authSvc.Refresh(ctx, auth.RefreshInput{RefreshToken: login2.RefreshToken}); err == nil {
		t.Fatal("expected kicked device 2 refresh to fail")
	}

	// Revoked device 2 must NOT be able to view/modify profile or retaliate against device 1.
	assert401 := func(op string, err error) {
		t.Helper()
		appErr, ok := apperr.As(err)
		if !ok || appErr.Status != 401 {
			t.Fatalf("%s with revoked session = %v, want 401", op, err)
		}
	}
	_, err = idSvc.GetProfile(ctx, user.ID, claims2.SessionID)
	assert401("GetProfile", err)
	hackedName := "Hijacked"
	_, err = idSvc.UpdateProfile(ctx, identity.UpdateProfileInput{
		UserID:           user.ID,
		CurrentSessionID: claims2.SessionID,
		DisplayName:      &hackedName,
	})
	assert401("UpdateProfile", err)
	_, err = idSvc.ListProfileSessions(ctx, user.ID, claims2.SessionID)
	assert401("ListProfileSessions", err)
	err = idSvc.RevokeProfileSession(ctx, identity.RevokeSessionInput{
		UserID:           user.ID,
		CurrentSessionID: claims2.SessionID,
		SessionID:        claims1.SessionID,
	})
	assert401("RevokeProfileSession(retaliation)", err)
	_, err = idSvc.RevokeOtherProfileSessions(ctx, identity.RevokeOtherSessionsInput{
		UserID:           user.ID,
		CurrentSessionID: claims2.SessionID,
	})
	assert401("RevokeOtherProfileSessions(retaliation)", err)
	_, err = idSvc.ChangePassword(ctx, identity.ChangePasswordInput{
		UserID:           user.ID,
		CurrentSessionID: claims2.SessionID,
		CurrentPassword:  "OldPassword123",
		NewPassword:      "HijackedPass999",
	})
	assert401("ChangePassword(revoked)", err)

	// Device 1 must still be active and untouched.
	remaining, err := idSvc.ListProfileSessions(ctx, user.ID, claims1.SessionID)
	if err != nil || len(remaining) != 1 || remaining[0].ID != claims1.SessionID {
		t.Fatalf("device 1 session must remain active after retaliation attempt, got (%+v, %v)", remaining, err)
	}

	// 3. ChangePassword with revokeOtherSessions=true invalidates device 3 and updates login credential.
	login3, err := authSvc.Login(ctx, auth.LoginInput{
		Identifier: "profileuser",
		Password:   "OldPassword123",
		ClientIP:   "10.0.0.3",
		UserAgent:  "DeviceThree",
	})
	if err != nil {
		t.Fatalf("Login(3): %v", err)
	}

	res, err := idSvc.ChangePassword(ctx, identity.ChangePasswordInput{
		UserID:           user.ID,
		CurrentSessionID: claims1.SessionID,
		CurrentPassword:  "OldPassword123",
		NewPassword:      "NewPassword456",
	})
	if err != nil || res.RevokedSessions != 1 {
		t.Fatalf("ChangePassword = (%+v, %v), want 1 revoked", res, err)
	}
	if _, err := authSvc.Refresh(ctx, auth.RefreshInput{RefreshToken: login3.RefreshToken}); err == nil {
		t.Fatal("expected device 3 refresh to fail after ChangePassword")
	}
	if _, err := authSvc.Login(ctx, auth.LoginInput{Identifier: "profileuser", Password: "OldPassword123"}); err == nil {
		t.Fatal("expected old password login to fail")
	}
	if _, err := authSvc.Login(ctx, auth.LoginInput{Identifier: "profileuser", Password: "NewPassword456"}); err != nil {
		t.Fatalf("expected new password login to succeed, got %v", err)
	}
}
