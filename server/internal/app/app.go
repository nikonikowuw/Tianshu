package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/modules/access"
	"tianshu/internal/modules/audit"
	"tianshu/internal/modules/auth"
	"tianshu/internal/modules/dashboard"
	"tianshu/internal/modules/files"
	"tianshu/internal/modules/identity"
	"tianshu/internal/modules/setup"
	"tianshu/internal/platform/cache"
	"tianshu/internal/platform/config"
	"tianshu/internal/platform/database"
	"tianshu/internal/platform/httpserver"
	"tianshu/internal/platform/i18n"
	"tianshu/internal/platform/logger"
	"tianshu/internal/platform/security"
	"tianshu/internal/platform/storage"
	"tianshu/migrations"
)

// App is the single composition root wiring platform infrastructure, modules, and HTTP server.
type App struct {
	cfg              config.Config
	log              *slog.Logger
	i18n             *i18n.Bundle
	hasher           *security.PasswordHasher
	jwt              *security.JWTManager
	db               *pgxpool.Pool
	tx               *database.TxManager
	cache            cache.Cache
	storage          *storage.Registry
	access           *access.Service
	accessHandler    *access.Handler
	identity         *identity.Service
	identityHandler  *identity.Handler
	audit            *audit.Service
	auditHandler     *audit.Handler
	files            *files.Service
	filesHandler     *files.Handler
	auth             *auth.Service
	authHandler      *auth.Handler
	dashboardHandler *dashboard.Handler
	setupHandler     *setup.Handler
	engine           *gin.Engine
	server           *http.Server
}

// Options customizes App assembly (primarily for tests).
type Options struct {
	SkipDB bool
}

// New initializes all platform components, validates i18n catalogs, and wires HTTP routes.
func New(ctx context.Context, cfg config.Config, opts Options) (*App, error) {
	log := logger.New(cfg.App.Env, cfg.Log.Level, nil)

	bundle, err := i18n.NewBundle()
	if err != nil {
		return nil, fmt.Errorf("init i18n bundle: %w", err)
	}
	if err := bundle.RegisterFS("access", access.LocalesFS, "locales"); err != nil {
		return nil, fmt.Errorf("register access locales: %w", err)
	}
	if err := bundle.RegisterFS("identity", identity.LocalesFS, "locales"); err != nil {
		return nil, fmt.Errorf("register identity locales: %w", err)
	}
	if err := bundle.RegisterFS("setup", setup.LocalesFS, "locales"); err != nil {
		return nil, fmt.Errorf("register setup locales: %w", err)
	}
	if err := bundle.RegisterFS("auth", auth.LocalesFS, "locales"); err != nil {
		return nil, fmt.Errorf("register auth locales: %w", err)
	}
	if err := bundle.RegisterFS("audit", audit.LocalesFS, "locales"); err != nil {
		return nil, fmt.Errorf("register audit locales: %w", err)
	}
	if err := bundle.RegisterFS("files", files.LocalesFS, "locales"); err != nil {
		return nil, fmt.Errorf("register files locales: %w", err)
	}
	if err := bundle.RegisterFS("dashboard", dashboard.LocalesFS, "locales"); err != nil {
		return nil, fmt.Errorf("register dashboard locales: %w", err)
	}
	if err := bundle.Validate(); err != nil {
		return nil, fmt.Errorf("validate i18n bundle: %w", err)
	}

	hasher, err := security.NewPasswordHasher(security.DefaultArgon2Params())
	if err != nil {
		return nil, fmt.Errorf("init password hasher: %w", err)
	}
	jwtMgr, err := security.NewJWTManager(cfg.Auth.JWTSecret, cfg.Auth.AccessTokenTTL)
	if err != nil {
		return nil, fmt.Errorf("init jwt manager: %w", err)
	}

	var (
		pool *pgxpool.Pool
		tx   *database.TxManager
	)
	if !opts.SkipDB {
		pool, err = database.NewPool(ctx, cfg.Database)
		if err != nil {
			return nil, err
		}
		if cfg.Database.AutoMigrate {
			if err := database.Migrate(ctx, pool, migrations.FS); err != nil {
				pool.Close()
				return nil, err
			}
		}
		tx = database.NewTxManager(pool)
	}

	memCache := cache.NewMemory()

	storeReg, err := initStorage(ctx, cfg.Storage, log)
	if err != nil {
		if pool != nil {
			pool.Close()
		}
		_ = memCache.Close()
		return nil, err
	}

	var (
		accessSvc        *access.Service
		accessHandler    *access.Handler
		identitySvc      *identity.Service
		identityHandler  *identity.Handler
		auditSvc         *audit.Service
		auditHandler     *audit.Handler
		filesSvc         *files.Service
		filesHandler     *files.Handler
		authSvc          *auth.Service
		authHandler      *auth.Handler
		dashboardHandler *dashboard.Handler
		setupHandler     *setup.Handler
	)
	if pool != nil {
		auditSvc = audit.NewService(pool, log)
		auditHandler = audit.NewHandler(auditSvc)

		identitySessionMgr := &identitySessionAdapter{}
		identityAccess := &identityAccessAdapter{}
		identityFiles := &identityFilesAdapter{}
		identitySvc = identity.NewService(pool, hasher, identity.ServiceDeps{
			Tx:       tx,
			Roles:    identityAccess,
			Sessions: identitySessionMgr,
			Files:    identityFiles,
			Audit:    &identityAuditAdapter{svc: auditSvc},
			I18n:     bundle,
			Log:      log,
		})

		accessSvc = access.NewService(pool, access.ServiceDeps{
			Tx:     tx,
			Cache:  memCache,
			I18n:   bundle,
			Audit:  &accessAuditAdapter{svc: auditSvc},
			Actors: &accessActorAdapter{svc: identitySvc},
			Log:    log,
		})
		identityAccess.svc = accessSvc
		if err := accessSvc.SyncCatalog(ctx); err != nil {
			pool.Close()
			_ = memCache.Close()
			return nil, fmt.Errorf("sync access catalog: %w", err)
		}
		accessHandler = access.NewHandler(accessSvc, bundle)
		identityHandler = identity.NewHandler(identitySvc, bundle)

		setupSvc := setup.NewService(
			&setupIdentityAdapter{svc: identitySvc},
			accessSvc,
			tx,
			&setupAuditAdapter{svc: auditSvc},
			log,
		)
		if err := setupSvc.BootstrapFromEnv(ctx, cfg.Bootstrap); err != nil {
			pool.Close()
			_ = memCache.Close()
			return nil, fmt.Errorf("bootstrap initial admin: %w", err)
		}
		setupHandler = setup.NewHandler(setupSvc)

		signedURLSecret := strings.TrimSpace(cfg.Storage.SignedURLSecret)
		if signedURLSecret == "" {
			// Empty falls back to the JWT secret so existing deployments keep working;
			// production clusters should set a dedicated secret for independent rotation.
			signedURLSecret = cfg.Auth.JWTSecret
		}
		filesSvc = files.NewService(
			pool,
			storeReg,
			files.ServiceConfig{
				MaxFileSizeBytes:   cfg.Storage.MaxFileSizeBytes,
				MaxAvatarSizeBytes: cfg.Storage.MaxAvatarSizeBytes,
				AllowedMIMETypes:   cfg.Storage.AllowedMIMETypes,
				SignedURLSecret:    signedURLSecret,
				SignedURLTTL:       cfg.Storage.SignedURLTTL,
			},
			files.ServiceDeps{
				Tx:        tx,
				Actors:    &filesActorAdapter{svc: identitySvc},
				AvatarRef: &filesAvatarRefAdapter{svc: identitySvc},
				Audit:     &filesAuditAdapter{svc: auditSvc},
				Log:       log,
			},
		)
		identityFiles.svc = filesSvc
		filesHandler = files.NewHandler(filesSvc)

		authSvc = auth.NewService(
			cfg.Auth,
			pool,
			auth.ServiceDeps{
				Identity:     &authIdentityAdapter{svc: identitySvc},
				Access:       &authAccessAdapter{svc: accessSvc},
				Hasher:       hasher,
				JWT:          jwtMgr,
				Tx:           tx,
				Cache:        memCache,
				Audit:        &authAuditAdapter{svc: auditSvc},
				AvatarSigner: &authAvatarSignerAdapter{svc: filesSvc},
				Log:          log,
			},
		)
		identitySessionMgr.svc = authSvc
		authHandler = auth.NewHandler(authSvc, cfg.CORS.AllowedOrigins)

		dashboardSvc := dashboard.NewService(pool)
		dashboardHandler = dashboard.NewHandler(dashboardSvc)
	}

	engine, err := httpserver.NewEngine(httpserver.Options{
		Env:            cfg.App.Env,
		TrustedProxies: cfg.Server.TrustedProxies,
		CORS:           cfg.CORS,
		Logger:         log,
		I18n:           bundle,
	})
	if err != nil {
		if pool != nil {
			pool.Close()
		}
		_ = memCache.Close()
		return nil, fmt.Errorf("init http engine: %w", err)
	}

	application := &App{
		cfg:              cfg,
		log:              log,
		i18n:             bundle,
		hasher:           hasher,
		jwt:              jwtMgr,
		db:               pool,
		tx:               tx,
		cache:            memCache,
		storage:          storeReg,
		access:           accessSvc,
		accessHandler:    accessHandler,
		identity:         identitySvc,
		identityHandler:  identityHandler,
		audit:            auditSvc,
		auditHandler:     auditHandler,
		files:            filesSvc,
		filesHandler:     filesHandler,
		auth:             authSvc,
		authHandler:      authHandler,
		dashboardHandler: dashboardHandler,
		setupHandler:     setupHandler,
		engine:           engine,
	}
	application.registerRoutes()

	application.server = &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           engine,
		ReadTimeout:       cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return application, nil
}

// Engine returns the wired Gin engine for HTTP contract tests.
func (a *App) Engine() *gin.Engine {
	return a.engine
}

// Run starts the HTTP server and blocks until ctx is canceled or a fatal serve error occurs.
func (a *App) Run(ctx context.Context) error {
	janitorCtx, janitorCancel := context.WithCancel(ctx)
	var janitorWG sync.WaitGroup
	if a.audit != nil && a.cfg.Audit.RetentionDays > 0 {
		janitorWG.Add(1)
		go func() {
			defer janitorWG.Done()
			a.runAuditRetentionJanitor(janitorCtx)
		}()
	}
	if a.files != nil {
		janitorWG.Add(1)
		go func() {
			defer janitorWG.Done()
			a.runFileOrphanJanitor(janitorCtx)
		}()
	}
	if a.auth != nil && a.cfg.Auth.SessionRetentionDays > 0 {
		janitorWG.Add(1)
		go func() {
			defer janitorWG.Done()
			a.runSessionCleanupJanitor(janitorCtx)
		}()
	}

	errCh := make(chan error, 1)
	go func() {
		a.log.Info("http server listening", "addr", a.cfg.Server.Addr, "env", a.cfg.App.Env)
		if err := a.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	var runErr error
	select {
	case err := <-errCh:
		janitorCancel()
		janitorWG.Wait()
		_ = a.Close(context.Background())
		runErr = err
	case <-ctx.Done():
		shutdownTimeout := a.cfg.Server.ShutdownTimeout
		if shutdownTimeout <= 0 {
			shutdownTimeout = 10 * time.Second
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		var errs []error
		if a.server != nil {
			if err := a.server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errs = append(errs, fmt.Errorf("shutdown http server: %w", err))
			}
		}
		janitorCancel()
		janitorWG.Wait()
		if err := a.closeResources(); err != nil {
			errs = append(errs, err)
		}
		runErr = errors.Join(errs...)
	}
	return runErr
}

func (a *App) runAuditRetentionJanitor(ctx context.Context) {
	const janitorInterval = 24 * time.Hour
	ticker := time.NewTicker(janitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			purgeCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			deleted, err := a.audit.PurgeExpired(purgeCtx, a.cfg.Audit.RetentionDays)
			cancel()
			if err != nil && !errors.Is(err, context.Canceled) {
				a.log.Warn("audit retention cleanup failed", "error", err)
			} else if deleted > 0 {
				a.log.Info("audit retention cleanup completed", "deleted", deleted, "retention_days", a.cfg.Audit.RetentionDays)
			}
		}
	}
}

func (a *App) runFileOrphanJanitor(ctx context.Context) {
	const (
		janitorInterval = 30 * time.Minute
		pendingTTL      = 15 * time.Minute
		batchLimit      = int32(100)
	)
	ticker := time.NewTicker(janitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			cleaned, err := a.files.CleanupOrphans(cleanCtx, pendingTTL, batchLimit)
			cancel()
			if err != nil && !errors.Is(err, context.Canceled) {
				a.log.Warn("file orphan cleanup failed", "error", err)
			} else if cleaned > 0 {
				a.log.Info("file orphan cleanup completed", "cleaned", cleaned)
			}
		}
	}
}

func (a *App) runSessionCleanupJanitor(ctx context.Context) {
	const janitorInterval = 6 * time.Hour
	retention := time.Duration(a.cfg.Auth.SessionRetentionDays) * 24 * time.Hour
	ticker := time.NewTicker(janitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			deleted, err := a.auth.PurgeExpiredSessions(cleanCtx, retention)
			cancel()
			if err != nil && !errors.Is(err, context.Canceled) {
				a.log.Warn("session cleanup failed", "error", err)
			} else if deleted > 0 {
				a.log.Info("session cleanup completed", "deleted", deleted)
			}
		}
	}
}

// Close gracefully drains HTTP connections and releases platform resources.
func (a *App) Close(ctx context.Context) error {
	var errs []error
	if a.server != nil {
		if err := a.server.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs = append(errs, fmt.Errorf("shutdown http server: %w", err))
		}
	}
	if err := a.closeResources(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (a *App) closeResources() error {
	var errs []error
	if a.cache != nil {
		if err := a.cache.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close cache: %w", err))
		}
	}
	if a.db != nil {
		a.db.Close()
	}
	return errors.Join(errs...)
}

func initStorage(ctx context.Context, cfg config.StorageConfig, log *slog.Logger) (*storage.Registry, error) {
	activeDriver := strings.ToLower(strings.TrimSpace(cfg.Driver))
	if activeDriver == "" {
		activeDriver = storage.DriverLocal
	}

	var drivers []storage.Storage
	if activeDriver == storage.DriverLocal || strings.TrimSpace(cfg.LocalRoot) != "" {
		localRoot := strings.TrimSpace(cfg.LocalRoot)
		if localRoot == "" {
			localRoot = "./data/uploads"
		}
		localStore, err := storage.NewLocal(localRoot)
		if err != nil {
			if activeDriver == storage.DriverLocal {
				return nil, fmt.Errorf("init local storage: %w", err)
			}
			if log != nil {
				log.Warn("skipping secondary local storage driver registration", "local_root", localRoot, "error", err)
			}
		} else {
			drivers = append(drivers, localStore)
		}
	}

	if activeDriver == storage.DriverS3 || cfg.S3.Configured() {
		s3Store, err := storage.NewS3(ctx, storage.S3Config{
			Endpoint:         cfg.S3.Endpoint,
			Region:           cfg.S3.Region,
			Bucket:           cfg.S3.Bucket,
			AccessKey:        cfg.S3.AccessKey,
			SecretKey:        cfg.S3.SecretKey,
			UsePathStyle:     cfg.S3.UsePathStyle,
			AutoCreateBucket: cfg.S3.AutoCreateBucket,
		})
		if err != nil {
			return nil, fmt.Errorf("init s3 storage: %w", err)
		}
		drivers = append(drivers, s3Store)
	}

	return storage.NewRegistry(activeDriver, drivers...)
}
