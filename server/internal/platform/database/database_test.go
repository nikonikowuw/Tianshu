package database_test

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/platform/config"
	"tianshu/internal/platform/database"
)

func TestAfterCommitSemantics(t *testing.T) {
	t.Run("runs immediately outside transaction", func(t *testing.T) {
		called := false
		database.AfterCommit(t.Context(), func() { called = true })
		if !called {
			t.Fatal("expected immediate execution outside tx")
		}
	})

	t.Run("defers until outermost tx commits in FIFO order", func(t *testing.T) {
		var order []string
		err := database.WithSimulatedTx(t.Context(), func(outerCtx context.Context) error {
			order = append(order, "outer-start")
			err := database.WithSimulatedTx(outerCtx, func(innerCtx context.Context) error {
				database.AfterCommit(innerCtx, func() { order = append(order, "hook-1") })
				order = append(order, "inner-done")
				return nil
			})
			if err != nil {
				return err
			}
			// Must NOT have executed before outer tx commits.
			if slices.Contains(order, "hook-1") {
				t.Fatal("hook-1 ran before outer transaction committed")
			}
			database.AfterCommit(outerCtx, func() { order = append(order, "hook-2") })
			order = append(order, "outer-done")
			return nil
		})
		if err != nil {
			t.Fatalf("WithSimulatedTx() error = %v", err)
		}

		want := []string{"outer-start", "inner-done", "outer-done", "hook-1", "hook-2"}
		if !slices.Equal(order, want) {
			t.Fatalf("order = %v, want %v", order, want)
		}
	})

	t.Run("discards hooks on rollback", func(t *testing.T) {
		called := false
		err := database.WithSimulatedTx(t.Context(), func(txCtx context.Context) error {
			database.AfterCommit(txCtx, func() { called = true })
			return errors.New("boom")
		})
		if err == nil {
			t.Fatal("expected error")
		}
		if called {
			t.Fatal("hook must not run when transaction rolls back")
		}
	})
}

func TestNewPoolValidationAndFallbackHelpers(t *testing.T) {
	if _, err := database.NewPool(t.Context(), config.DatabaseConfig{}); err == nil {
		t.Fatal("expected error for empty DSN")
	}
	if _, err := database.NewPool(t.Context(), config.DatabaseConfig{DSN: "://invalid-dsn"}); err == nil {
		t.Fatal("expected error for invalid DSN")
	}

	if _, ok := database.TxFromContext(t.Context()); ok {
		t.Fatal("expected no tx in plain context")
	}
	if err := database.AcquireAdvisoryXactLock(t.Context(), 123); err != nil {
		t.Fatalf("AcquireAdvisoryXactLock outside tx error = %v, want nil", err)
	}
	if exec := database.Executor(t.Context(), nil); exec != nil {
		t.Fatalf("Executor with nil pool outside tx = %v, want nil", exec)
	}
}

func openIsolatedTestPool(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()

	dsn, dsnExplicit := os.LookupEnv("TIANSHU_TEST_DSN")
	if !dsnExplicit {
		dsn = config.Default().Database.DSN
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	cfg := config.Default().Database
	cfg.DSN = dsn
	adminPool, err := database.NewPool(ctx, cfg)
	if err != nil {
		if dsnExplicit {
			t.Fatalf("postgres unavailable: %v", err)
		}
		t.Skipf("postgres unavailable: %v", err)
	}

	schema := "db_it_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	poolCfg.MaxConns = 4

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		t.Fatalf("open schema pool: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool, schema
}

func TestPostgresTxManagerAndMigrateIntegration(t *testing.T) {
	pool, _ := openIsolatedTestPool(t)
	ctx := t.Context()

	migrations := fstest.MapFS{
		"00001_create_items.sql": {
			Data: []byte(`-- +goose Up
CREATE TABLE items (name text PRIMARY KEY);
-- +goose Down
DROP TABLE items;
`),
		},
	}
	if err := database.Migrate(ctx, pool, migrations); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	// Idempotent re-run must succeed cleanly.
	if err := database.Migrate(ctx, pool, migrations); err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}

	txMgr := database.NewTxManager(pool)

	t.Run("commit, nested reuse, and AfterCommit", func(t *testing.T) {
		committedHook := false
		err := txMgr.WithinTx(ctx, func(outerCtx context.Context) error {
			outerTx, ok := database.TxFromContext(outerCtx)
			if !ok {
				t.Fatal("expected active tx in outerCtx")
			}
			return txMgr.WithinTx(outerCtx, func(innerCtx context.Context) error {
				innerTx, ok := database.TxFromContext(innerCtx)
				if !ok || innerTx != outerTx {
					t.Fatal("nested WithinTx must reuse outer pgx.Tx")
				}
				database.AfterCommit(innerCtx, func() { committedHook = true })
				_, err := database.Executor(innerCtx, pool).Exec(innerCtx, "INSERT INTO items(name) VALUES ($1)", "committed")
				return err
			})
		})
		if err != nil {
			t.Fatalf("WithinTx() error = %v", err)
		}
		if !committedHook {
			t.Fatal("expected AfterCommit hook to execute after commit")
		}

		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM items WHERE name = $1", "committed").Scan(&count); err != nil {
			t.Fatalf("query committed row: %v", err)
		}
		if count != 1 {
			t.Fatalf("committed count = %d, want 1", count)
		}
	})

	t.Run("rollback on canceled context preserves original error and cleans up", func(t *testing.T) {
		reqCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		hookCalled := false
		err := txMgr.WithinTx(reqCtx, func(txCtx context.Context) error {
			database.AfterCommit(txCtx, func() { hookCalled = true })
			if _, err := database.Executor(txCtx, pool).Exec(txCtx, "INSERT INTO items(name) VALUES ($1)", "canceled-row"); err != nil {
				return err
			}
			cancel()
			return reqCtx.Err()
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("WithinTx() error = %v, want context.Canceled", err)
		}
		if hookCalled {
			t.Fatal("AfterCommit must not run on canceled rollback")
		}

		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM items WHERE name = $1", "canceled-row").Scan(&count); err != nil {
			t.Fatalf("query canceled row: %v", err)
		}
		if count != 0 {
			t.Fatalf("canceled row count = %d, want 0", count)
		}
	})

	t.Run("panic inside WithinTx rolls back and releases pooled connection", func(t *testing.T) {
		beforeAcquired := pool.Stat().AcquiredConns()
		hookCalled := false

		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("expected panic to propagate after rollback")
				}
			}()
			_ = txMgr.WithinTx(ctx, func(txCtx context.Context) error {
				database.AfterCommit(txCtx, func() { hookCalled = true })
				if err := database.AcquireAdvisoryXactLock(txCtx, 987654); err != nil {
					return err
				}
				if _, err := database.Executor(txCtx, pool).Exec(txCtx, "INSERT INTO items(name) VALUES ($1)", "panicked-row"); err != nil {
					return err
				}
				panic("unexpected panic inside tx")
			})
		}()

		if hookCalled {
			t.Fatal("AfterCommit must not run when transaction panics")
		}
		if afterAcquired := pool.Stat().AcquiredConns(); afterAcquired != beforeAcquired {
			t.Fatalf("leaked connection on panic: acquired before=%d after=%d", beforeAcquired, afterAcquired)
		}

		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM items WHERE name = $1", "panicked-row").Scan(&count); err != nil {
			t.Fatalf("query panicked row: %v", err)
		}
		if count != 0 {
			t.Fatalf("panicked row count = %d, want 0", count)
		}
	})

	t.Run("AcquireAdvisoryXactLock serializes concurrent transactions", func(t *testing.T) {
		var mu sync.Mutex
		var events []string
		enteredFirst := make(chan struct{})
		releaseFirst := make(chan struct{})

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			err := txMgr.WithinTx(ctx, func(txCtx context.Context) error {
				if err := database.AcquireAdvisoryXactLock(txCtx, 424242); err != nil {
					return err
				}
				mu.Lock()
				events = append(events, "tx1-enter")
				mu.Unlock()
				close(enteredFirst)
				<-releaseFirst
				mu.Lock()
				events = append(events, "tx1-exit")
				mu.Unlock()
				return nil
			})
			if err != nil {
				t.Errorf("tx1 error = %v", err)
			}
		}()

		go func() {
			defer wg.Done()
			<-enteredFirst
			err := txMgr.WithinTx(ctx, func(txCtx context.Context) error {
				if err := database.AcquireAdvisoryXactLock(txCtx, 424242); err != nil {
					return err
				}
				mu.Lock()
				events = append(events, "tx2-enter")
				mu.Unlock()
				return nil
			})
			if err != nil {
				t.Errorf("tx2 error = %v", err)
			}
		}()

		<-enteredFirst
		close(releaseFirst)
		wg.Wait()

		want := []string{"tx1-enter", "tx1-exit", "tx2-enter"}
		if !slices.Equal(events, want) {
			t.Fatalf("advisory lock sequence = %v, want %v", events, want)
		}
	})
}
