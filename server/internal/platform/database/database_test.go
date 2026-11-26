package database_test

import (
	"context"
	"errors"
	"slices"
	"testing"

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
