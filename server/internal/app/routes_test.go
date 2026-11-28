package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"tianshu/internal/modules/access"
	"tianshu/internal/modules/audit"
	"tianshu/internal/modules/auth"
	"tianshu/internal/modules/dashboard"
	"tianshu/internal/modules/files"
	"tianshu/internal/modules/identity"
	"tianshu/internal/modules/setup"
	"tianshu/internal/platform/cache"
	"tianshu/internal/platform/config"
	"tianshu/internal/platform/httpserver"
	"tianshu/internal/platform/i18n"
	"tianshu/internal/platform/logger"
	"tianshu/internal/platform/security"
	"tianshu/internal/platform/storage"
	"tianshu/internal/shared/apperr"
)

func buildWiredTestApp(t *testing.T, mutateCfg func(*config.Config)) (*App, *cache.Memory) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	cfg := config.Default()
	cfg.App.Env = "test"
	cfg.Storage.LocalRoot = t.TempDir()
	if mutateCfg != nil {
		mutateCfg(&cfg)
	}

	log := logger.New("test", "error", io.Discard)
	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("i18n.NewBundle() error = %v", err)
	}
	if err := bundle.RegisterFS("access", access.LocalesFS, "locales"); err != nil {
		t.Fatalf("register access locales: %v", err)
	}
	if err := bundle.RegisterFS("identity", identity.LocalesFS, "locales"); err != nil {
		t.Fatalf("register identity locales: %v", err)
	}
	if err := bundle.RegisterFS("setup", setup.LocalesFS, "locales"); err != nil {
		t.Fatalf("register setup locales: %v", err)
	}
	if err := bundle.RegisterFS("auth", auth.LocalesFS, "locales"); err != nil {
		t.Fatalf("register auth locales: %v", err)
	}
	if err := bundle.RegisterFS("audit", audit.LocalesFS, "locales"); err != nil {
		t.Fatalf("register audit locales: %v", err)
	}
	if err := bundle.RegisterFS("files", files.LocalesFS, "locales"); err != nil {
		t.Fatalf("register files locales: %v", err)
	}
	if err := bundle.RegisterFS("dashboard", dashboard.LocalesFS, "locales"); err != nil {
		t.Fatalf("register dashboard locales: %v", err)
	}
	if err := bundle.Validate(); err != nil {
		t.Fatalf("bundle.Validate() error = %v", err)
	}

	hasher, err := security.NewPasswordHasher(security.DefaultArgon2Params())
	if err != nil {
		t.Fatalf("NewPasswordHasher() error = %v", err)
	}
	jwtMgr, err := security.NewJWTManager(cfg.Auth.JWTSecret, cfg.Auth.AccessTokenTTL)
	if err != nil {
		t.Fatalf("NewJWTManager() error = %v", err)
	}
	memCache := cache.NewMemory()
	storeReg, err := initStorage(t.Context(), cfg.Storage, log)
	if err != nil {
		t.Fatalf("initStorage() error = %v", err)
	}

	auditSvc := audit.NewService(nil)
	identitySvc := identity.NewService(nil, hasher, identity.ServiceDeps{Log: log})
	accessSvc := access.NewService(nil, access.ServiceDeps{Cache: memCache, I18n: bundle, Log: log})
	setupSvc := setup.NewService(&setupIdentityAdapter{svc: identitySvc}, accessSvc, nil, &setupAuditAdapter{svc: auditSvc}, log)
	filesSvc := files.NewService(nil, storeReg, files.ServiceConfig{
		MaxFileSizeBytes:   cfg.Storage.MaxFileSizeBytes,
		MaxAvatarSizeBytes: cfg.Storage.MaxAvatarSizeBytes,
		SignedURLSecret:    cfg.Auth.JWTSecret,
		SignedURLTTL:       cfg.Storage.SignedURLTTL,
	}, files.ServiceDeps{Log: log})
	authSvc := auth.NewService(cfg.Auth, nil, auth.ServiceDeps{
		Identity: &authIdentityAdapter{svc: identitySvc},
		Access:   &authAccessAdapter{svc: accessSvc},
		Hasher:   hasher,
		JWT:      jwtMgr,
		Cache:    memCache,
		Log:      log,
	})
	dashboardSvc := dashboard.NewService(nil)

	engine, err := httpserver.NewEngine(httpserver.Options{
		Env:                cfg.App.Env,
		TrustedProxies:     cfg.Server.TrustedProxies,
		CORS:               cfg.CORS,
		Logger:             log,
		I18n:               bundle,
		RedactPathPrefixes: []string{"/api/v1/files/signed/"},
	})
	if err != nil {
		t.Fatalf("httpserver.NewEngine() error = %v", err)
	}

	application := &App{
		cfg:              cfg,
		log:              log,
		i18n:             bundle,
		hasher:           hasher,
		jwt:              jwtMgr,
		cache:            memCache,
		storage:          storeReg,
		access:           accessSvc,
		accessHandler:    access.NewHandler(accessSvc, bundle),
		identity:         identitySvc,
		identityHandler:  identity.NewHandler(identitySvc, bundle),
		audit:            auditSvc,
		auditHandler:     audit.NewHandler(auditSvc),
		files:            filesSvc,
		filesHandler:     files.NewHandler(filesSvc),
		auth:             authSvc,
		authHandler:      auth.NewHandler(authSvc, nil, apiV1Prefix+"/auth"),
		dashboardHandler: dashboard.NewHandler(dashboardSvc),
		setupHandler:     setup.NewHandler(setupSvc),
		engine:           engine,
	}
	application.registerRoutes()
	t.Cleanup(func() { _ = application.Close(t.Context()) })
	return application, memCache
}

func materializeRoutePath(pattern string) string {
	sampleID := uuid.NewString()
	parts := strings.Split(pattern, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") {
			parts[i] = sampleID
		}
	}
	return strings.Join(parts, "/")
}

func TestAllAPIRoutesEnforceAuthAndPermissionAllowlist(t *testing.T) {
	application, memCache := buildWiredTestApp(t, nil)

	// Explicit allowlist of intentionally unauthenticated /api/v1/* endpoints.
	publicAllowlist := map[string]struct{}{
		"GET /api/v1/setup/status":        {},
		"POST /api/v1/setup/init":         {},
		"POST /api/v1/auth/login":         {},
		"POST /api/v1/auth/refresh":       {},
		"POST /api/v1/auth/logout":        {},
		"GET /api/v1/files/signed/:token": {},
	}

	// Authenticated self-service routes that intentionally require JWT without a specific RBAC permission code.
	selfServiceAllowlist := map[string]struct{}{
		"GET /api/v1/auth/me":                 {},
		"GET /api/v1/menus/user":              {},
		"GET /api/v1/profile":                 {},
		"PATCH /api/v1/profile":               {},
		"PUT /api/v1/profile/password":        {},
		"GET /api/v1/profile/sessions":        {},
		"DELETE /api/v1/profile/sessions":     {},
		"DELETE /api/v1/profile/sessions/:id": {},
		"GET /api/v1/files/config":            {},
		"POST /api/v1/files/avatar":           {},
	}

	// Seed an authenticated user with zero permissions in the RBAC cache.
	userID := uuid.New()
	sessionID := uuid.New()
	token, _, err := application.jwt.Sign(userID, sessionID)
	if err != nil {
		t.Fatalf("jwt.Sign() error = %v", err)
	}
	emptyAccess, _ := json.Marshal(access.UserAccess{Roles: []string{}, Permissions: []string{}})
	if err := memCache.Set(t.Context(), "access:user:"+userID.String(), emptyAccess, time.Minute); err != nil {
		t.Fatalf("memCache.Set() error = %v", err)
	}

	routes := application.Engine().Routes()
	sawHealthz := false
	sawReadyz := false
	seenPublic := make(map[string]bool)

	for _, r := range routes {
		key := r.Method + " " + r.Path
		switch key {
		case "GET /healthz":
			sawHealthz = true
			continue
		case "GET /readyz":
			sawReadyz = true
			continue
		}

		if !strings.HasPrefix(r.Path, "/api/v1/") {
			t.Fatalf("unexpected non-/api/v1 route registered: %s", key)
		}

		if _, isPublic := publicAllowlist[key]; isPublic {
			seenPublic[key] = true
			continue
		}

		target := materializeRoutePath(r.Path)

		// 1. Missing Bearer token must always fail with 401 UNAUTHORIZED.
		unauthReq := httptest.NewRequest(r.Method, target, nil)
		unauthRec := httptest.NewRecorder()
		application.Engine().ServeHTTP(unauthRec, unauthReq)
		if unauthRec.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s returned %d, want 401 (body=%s)", key, unauthRec.Code, unauthRec.Body.String())
		}

		// 2. Non-self-service endpoints must fail with 403 PERMISSION_DENIED when user has zero permissions.
		if _, isSelfService := selfServiceAllowlist[key]; !isSelfService {
			authedReq := httptest.NewRequest(r.Method, target, nil)
			authedReq.Header.Set("Authorization", "Bearer "+token)
			authedRec := httptest.NewRecorder()
			application.Engine().ServeHTTP(authedRec, authedReq)
			if authedRec.Code != http.StatusForbidden {
				t.Fatalf("unauthorized RBAC %s returned %d, want 403 (body=%s)", key, authedRec.Code, authedRec.Body.String())
			}
		}
	}

	if !sawHealthz || !sawReadyz {
		t.Fatalf("missing health routes: healthz=%v readyz=%v", sawHealthz, sawReadyz)
	}
	for expectedPublic := range publicAllowlist {
		if !seenPublic[expectedPublic] {
			t.Fatalf("expected public route %s was not registered", expectedPublic)
		}
	}

	// Verify /healthz and /readyz responses.
	for _, path := range []string{"/healthz", "/readyz"} {
		rec := httptest.NewRecorder()
		application.Engine().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", path, rec.Code)
		}
	}
}

func TestGlobalAPIRateLimitAndBodyLimit(t *testing.T) {
	t.Run("enforces configurable global API rate limit on public and authed routes", func(t *testing.T) {
		application, _ := buildWiredTestApp(t, func(cfg *config.Config) {
			cfg.Server.APIRateLimit = 2
		})

		for i := range 2 {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/files/signed/invalid-token", nil)
			req.RemoteAddr = "198.51.100.10:12345"
			application.Engine().ServeHTTP(rec, req)
			if rec.Code == http.StatusTooManyRequests {
				t.Fatalf("request %d unexpectedly hit rate limit", i+1)
			}
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/files/signed/invalid-token", nil)
		req.RemoteAddr = "198.51.100.10:12345"
		application.Engine().ServeHTTP(rec, req)
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("3rd request status = %d, want 429 (body=%s)", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Retry-After") == "" {
			t.Fatal("missing Retry-After header on 429 response")
		}

		// Authenticated route rate-limits independently per user ID.
		token, _, err := application.jwt.Sign(uuid.New(), uuid.New())
		if err != nil {
			t.Fatalf("jwt.Sign() error = %v", err)
		}
		for i := range 2 {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/files/config", nil)
			req.RemoteAddr = "198.51.100.10:12345"
			req.Header.Set("Authorization", "Bearer "+token)
			application.Engine().ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("authed request %d status = %d, want 200", i+1, rec.Code)
			}
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/api/v1/files/config", nil)
		req.RemoteAddr = "198.51.100.10:12345"
		req.Header.Set("Authorization", "Bearer "+token)
		application.Engine().ServeHTTP(rec, req)
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("3rd authed request status = %d, want 429", rec.Code)
		}
	})

	t.Run("enforces MaxRequestBodyBytes on JSON API routes without blocking file uploads", func(t *testing.T) {
		application, memCache := buildWiredTestApp(t, func(cfg *config.Config) {
			cfg.Server.MaxRequestBodyBytes = 64
		})

		oversizedJSON := `{"identifier":"` + strings.Repeat("a", 128) + `","password":"secret-password"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(oversizedJSON))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		application.Engine().ServeHTTP(rec, req)

		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("oversized JSON body status = %d, want 413 (body=%s)", rec.Code, rec.Body.String())
		}
		var errBody map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
			t.Fatalf("unmarshal 413 response: %v", err)
		}
		if errBody["code"] != apperr.CodeTooLarge {
			t.Fatalf("413 code = %v, want %s", errBody["code"], apperr.CodeTooLarge)
		}

		// File upload route must NOT be capped at 64 bytes by the outer JSON limit middleware.
		uid := uuid.New()
		sid := uuid.New()
		token, _, err := application.jwt.Sign(uid, sid)
		if err != nil {
			t.Fatalf("jwt.Sign() error = %v", err)
		}
		permsJSON, _ := json.Marshal(access.UserAccess{Permissions: []string{access.PermFilesUpload}})
		if err := memCache.Set(t.Context(), "access:user:"+uid.String(), permsJSON, time.Minute); err != nil {
			t.Fatalf("memCache.Set() error = %v", err)
		}

		uploadReq := httptest.NewRequest(http.MethodPost, "/api/v1/files", strings.NewReader(strings.Repeat("x", 256)))
		uploadReq.Header.Set("Authorization", "Bearer "+token)
		uploadReq.Header.Set("Content-Type", "multipart/form-data; boundary=missing")
		uploadRec := httptest.NewRecorder()
		application.Engine().ServeHTTP(uploadRec, uploadReq)
		if uploadRec.Code == http.StatusRequestEntityTooLarge {
			t.Fatalf("upload route was wrongly capped by MaxRequestBodyBytes=64: %s", uploadRec.Body.String())
		}
	})
}

func TestInitStorageRollbackAndAppCloseReleasesStorage(t *testing.T) {
	t.Run("secondary S3 failure under local driver logs warning and falls back cleanly", func(t *testing.T) {
		cfg := config.Default().Storage
		cfg.Driver = storage.DriverLocal
		cfg.LocalRoot = t.TempDir()
		cfg.S3 = config.S3StorageConfig{
			Endpoint:         "http://127.0.0.1:1",
			Region:           "us-east-1",
			Bucket:           "tianshu",
			AccessKey:        "ak",
			SecretKey:        "sk",
			AutoCreateBucket: true,
		}

		reg, err := initStorage(t.Context(), cfg, logger.New("test", "error", io.Discard))
		if err != nil {
			t.Fatalf("initStorage() unexpected error = %v", err)
		}
		defer func() { _ = reg.Close() }()

		if reg.DefaultDriverName() != storage.DriverLocal {
			t.Fatalf("DefaultDriverName() = %q, want local", reg.DefaultDriverName())
		}
	})

	t.Run("primary S3 failure closes already-opened local driver", func(t *testing.T) {
		localRoot := filepath.Join(t.TempDir(), "uploads")
		cfg := config.Default().Storage
		cfg.Driver = storage.DriverS3
		cfg.LocalRoot = localRoot
		cfg.S3 = config.S3StorageConfig{
			Endpoint:         "http://127.0.0.1:1",
			Region:           "us-east-1",
			Bucket:           "tianshu",
			AccessKey:        "ak",
			SecretKey:        "sk",
			AutoCreateBucket: true,
		}

		if _, err := initStorage(t.Context(), cfg, logger.New("test", "error", io.Discard)); err == nil {
			t.Fatal("expected initStorage() error when primary S3 fails")
		}
	})

	t.Run("App.Close releases local storage root handle", func(t *testing.T) {
		application, _ := buildWiredTestApp(t, nil)
		localDriver, err := application.storage.Driver(storage.DriverLocal)
		if err != nil {
			t.Fatalf("Driver(local) error = %v", err)
		}

		if err := application.Close(t.Context()); err != nil {
			t.Fatalf("App.Close() error = %v", err)
		}

		// After Close(), os.Root inside storage.Local is closed, so Stat fails with os.ErrClosed.
		_, statErr := localDriver.Stat(t.Context(), "non-existent-key")
		if !errors.Is(statErr, os.ErrClosed) {
			t.Fatalf("Stat() after Close() err = %v, want os.ErrClosed", statErr)
		}
	})
}
