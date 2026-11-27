package database

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"tianshu/internal/platform/config"
)

type txCtxKey struct{}

type txState struct {
	tx        pgx.Tx
	mu        sync.Mutex
	afterDone []func()
}

func (s *txState) addAfterCommit(fn func()) {
	s.mu.Lock()
	s.afterDone = append(s.afterDone, fn)
	s.mu.Unlock()
}

func (s *txState) runAfterCommit() {
	s.mu.Lock()
	hooks := s.afterDone
	s.afterDone = nil
	s.mu.Unlock()

	for _, fn := range hooks {
		fn()
	}
}

// DBTX matches the sqlc-generated query interface implemented by both *pgxpool.Pool and pgx.Tx.
type DBTX interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// TxManager coordinates database transactions propagated through context.Context.
type TxManager struct {
	pool *pgxpool.Pool
}

// NewPool initializes a PostgreSQL connection pool and verifies connectivity.
func NewPool(ctx context.Context, cfg config.DatabaseConfig) (*pgxpool.Pool, error) {
	if cfg.DSN == "" {
		return nil, errors.New("database.dsn must not be empty")
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse database dsn: %w", err)
	}
	if cfg.MaxConns > 0 {
		poolCfg.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		poolCfg.MinConns = cfg.MinConns
	}
	if cfg.MaxConnLifetime > 0 {
		poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pgx pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}

// NewTxManager creates a TxManager bound to pool.
func NewTxManager(pool *pgxpool.Pool) *TxManager {
	return &TxManager{pool: pool}
}

// WithinTx executes fn inside a transaction stored in ctx.
// If ctx already carries an active transaction, fn reuses it without starting a nested tx.
// Callbacks registered via AfterCommit run in FIFO order only after the outermost transaction commits.
func (m *TxManager) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := txStateFromContext(ctx); ok {
		return fn(ctx)
	}

	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	committed := false
	defer func() {
		if committed {
			return
		}
		rbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rbCtx)
	}()

	state := &txState{tx: tx}
	txCtx := context.WithValue(ctx, txCtxKey{}, state)
	if err := fn(txCtx); err != nil {
		rbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		rbErr := tx.Rollback(rbCtx)
		cancel()
		if rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			return fmt.Errorf("rollback transaction after %w: %v", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	committed = true

	state.runAfterCommit()
	return nil
}

// AfterCommit schedules fn to run after the active transaction in ctx commits.
// If ctx has no active transaction, fn runs immediately.
func AfterCommit(ctx context.Context, fn func()) {
	if fn == nil {
		return
	}
	if state, ok := txStateFromContext(ctx); ok {
		state.addAfterCommit(fn)
		return
	}
	fn()
}

// WithSimulatedTx runs fn with a simulated transaction context for unit tests that verify AfterCommit ordering.
// If fn returns nil, registered AfterCommit hooks run; otherwise they are discarded.
func WithSimulatedTx(ctx context.Context, fn func(txCtx context.Context) error) error {
	if _, ok := txStateFromContext(ctx); ok {
		return fn(ctx)
	}
	state := &txState{}
	txCtx := context.WithValue(ctx, txCtxKey{}, state)
	if err := fn(txCtx); err != nil {
		return err
	}
	state.runAfterCommit()
	return nil
}

func txStateFromContext(ctx context.Context) (*txState, bool) {
	if ctx == nil {
		return nil, false
	}
	state, ok := ctx.Value(txCtxKey{}).(*txState)
	return state, ok && state != nil
}

// AcquireAdvisoryXactLock acquires a PostgreSQL transaction-scoped advisory lock.
// When called inside a real pgx transaction it executes pg_advisory_xact_lock(key);
// outside a real pgx transaction (e.g. unit tests using WithSimulatedTx) it is a no-op.
func AcquireAdvisoryXactLock(ctx context.Context, key int64) error {
	tx, ok := TxFromContext(ctx)
	if !ok {
		return nil
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", key); err != nil {
		return fmt.Errorf("acquire advisory lock %d: %w", key, err)
	}
	return nil
}

// TxFromContext returns the active pgx.Tx carried in ctx, if any.
func TxFromContext(ctx context.Context) (pgx.Tx, bool) {
	state, ok := txStateFromContext(ctx)
	if !ok || state.tx == nil {
		return nil, false
	}
	return state.tx, true
}

// Executor returns the active transaction from ctx or falls back to the connection pool.
func Executor(ctx context.Context, pool *pgxpool.Pool) DBTX {
	if tx, ok := TxFromContext(ctx); ok {
		return tx
	}
	if pool == nil {
		return nil
	}
	return pool
}

// Migrate runs embedded Goose SQL migrations up to the latest version.
func Migrate(ctx context.Context, pool *pgxpool.Pool, migrationsFS fs.FS) error {
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer func() { _ = sqlDB.Close() }()

	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrationsFS)
	if err != nil {
		return fmt.Errorf("init goose provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("apply database migrations: %w", err)
	}
	return nil
}
