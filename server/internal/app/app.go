package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/modules/access"
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
	cfg          config.Config
	log          *slog.Logger
	i18n         *i18n.Bundle
	hasher       *security.PasswordHasher
	db           *pgxpool.Pool
	tx           *database.TxManager
	cache        cache.Cache
	storage      storage.Storage
	access       *access.Service
	identity     *identity.Service
	setupHandler *setup.Handler
	engine       *gin.Engine
	server       *http.Server
}

// Options customizes App assembly (primarily for tests).
type Options struct {
	SkipDB bool
}

// New initializes all platform components, validates i18n catalogs, and wires HTTP routes.
func New(ctx context.Context, cfg config.Config, opts Options) (*App, error) {
	log := logger.New(cfg.App.Env, cfg.Log.Level, nil)

	if cfg.Setup.Mode == "open" {
		log.Warn("setup.mode=open is explicitly enabled; disable or complete setup immediately")
	}

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
	if err := bundle.Validate(); err != nil {
		return nil, fmt.Errorf("validate i18n bundle: %w", err)
	}

	hasher, err := security.NewPasswordHasher(security.DefaultArgon2Params())
	if err != nil {
		return nil, fmt.Errorf("init password hasher: %w", err)
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
		accessSvc    *access.Service
		identitySvc  *identity.Service
		setupHandler *setup.Handler
	)
	if pool != nil {
		accessSvc = access.NewService(pool, tx, memCache, log)
		if err := accessSvc.SyncCatalog(ctx); err != nil {
			pool.Close()
			_ = memCache.Close()
			return nil, fmt.Errorf("sync access catalog: %w", err)
		}

		identitySvc = identity.NewService(pool, hasher)
		setupSvc, err := setup.NewService(
			ctx,
			cfg.Setup,
			&setupIdentityAdapter{svc: identitySvc},
			accessSvc,
			tx,
			log,
		)
		if err != nil {
			pool.Close()
			_ = memCache.Close()
			return nil, fmt.Errorf("init setup service: %w", err)
		}
		setupHandler = setup.NewHandler(setupSvc)
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
		cfg:          cfg,
		log:          log,
		i18n:         bundle,
		hasher:       hasher,
		db:           pool,
		tx:           tx,
		cache:        memCache,
		storage:      store,
		access:       accessSvc,
		identity:     identitySvc,
		setupHandler: setupHandler,
		engine:       engine,
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
	errCh := make(chan error, 1)
	go func() {
		a.log.Info("http server listening", "addr", a.cfg.Server.Addr, "env", a.cfg.App.Env)
		if err := a.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		_ = a.Close(context.Background())
		return err
	case <-ctx.Done():
		shutdownTimeout := a.cfg.Server.ShutdownTimeout
		if shutdownTimeout <= 0 {
			shutdownTimeout = 10 * time.Second
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return a.Close(shutdownCtx)
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
