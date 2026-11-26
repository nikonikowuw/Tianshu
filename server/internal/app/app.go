package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/modules/access"
	"tianshu/internal/modules/audit"
	"tianshu/internal/modules/auth"
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
	cfg             config.Config
	log             *slog.Logger
	i18n            *i18n.Bundle
	hasher          *security.PasswordHasher
	jwt             *security.JWTManager
	db              *pgxpool.Pool
	tx              *database.TxManager
	cache           cache.Cache
	storage         storage.Storage
	access          *access.Service
	accessHandler   *access.Handler
	identity        *identity.Service
	identityHandler *identity.Handler
	audit           *audit.Service
	auditHandler    *audit.Handler
	authHandler     *auth.Handler
	setupHandler    *setup.Handler
	engine          *gin.Engine
	server          *http.Server
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

	var (
		accessSvc       *access.Service
		accessHandler   *access.Handler
		identitySvc     *identity.Service
		identityHandler *identity.Handler
		auditSvc        *audit.Service
		auditHandler    *audit.Handler
		authHandler     *auth.Handler
		setupHandler    *setup.Handler
	)
	if pool != nil {
		auditSvc = audit.NewService(pool, log)
		auditHandler = audit.NewHandler(auditSvc)

		identitySessionRevoker := &identitySessionAdapter{}
		identityAccess := &identityAccessAdapter{}
		identitySvc = identity.NewService(pool, hasher, identity.ServiceDeps{
			Tx:       tx,
			Roles:    identityAccess,
			Sessions: identitySessionRevoker,
			Audit:    &identityAuditAdapter{svc: auditSvc},
			I18n:     bundle,
			Log:      log,
		})

		accessSvc = access.NewService(pool, tx, memCache, log, access.ServiceDeps{
			I18n:   bundle,
			Audit:  &accessAuditAdapter{svc: auditSvc},
			Actors: &accessActorAdapter{svc: identitySvc},
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

		authSvc := auth.NewService(
			cfg.Auth,
			pool,
			&authIdentityAdapter{svc: identitySvc},
			&authAccessAdapter{svc: accessSvc},
			hasher,
			jwtMgr,
			tx,
			memCache,
			&authAuditAdapter{svc: auditSvc},
			log,
		)
		identitySessionRevoker.svc = authSvc
		authHandler = auth.NewHandler(authSvc, cfg.CORS.AllowedOrigins)
	}

	store, err := initStorage(cfg.Storage)
	if err != nil {
		if pool != nil {
			pool.Close()
		}
		_ = memCache.Close()
		return nil, err
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
		cfg:             cfg,
		log:             log,
		i18n:            bundle,
		hasher:          hasher,
		jwt:             jwtMgr,
		db:              pool,
		tx:              tx,
		cache:           memCache,
		storage:         store,
		access:          accessSvc,
		accessHandler:   accessHandler,
		identity:        identitySvc,
		identityHandler: identityHandler,
		audit:           auditSvc,
		auditHandler:    auditHandler,
		authHandler:     authHandler,
		setupHandler:    setupHandler,
		engine:          engine,
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
		_ = a.Close(context.Background())
		runErr = err
	case <-ctx.Done():
		janitorCancel()
		shutdownTimeout := a.cfg.Server.ShutdownTimeout
		if shutdownTimeout <= 0 {
			shutdownTimeout = 10 * time.Second
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		runErr = a.Close(shutdownCtx)
	}
	janitorWG.Wait()
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

// Close gracefully drains HTTP connections and releases platform resources.
func (a *App) Close(ctx context.Context) error {
	var errs []error
	if a.server != nil {
		if err := a.server.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs = append(errs, fmt.Errorf("shutdown http server: %w", err))
		}
	}
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

func initStorage(cfg config.StorageConfig) (storage.Storage, error) {
	switch cfg.Driver {
	case "", "local":
		return storage.NewLocal(cfg.LocalRoot)
	default:
		return nil, fmt.Errorf("unsupported storage driver %q (s3 driver pending module wiring)", cfg.Driver)
	}
}
