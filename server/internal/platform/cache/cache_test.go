package cache_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"tianshu/internal/platform/cache"
)

func TestMemoryCacheLifecycleAndIsolation(t *testing.T) {
	c := cache.NewMemory()
	defer func() { _ = c.Close() }()

	ctx := context.Background()
	src := []byte("permissions:read")
	if err := c.Set(ctx, "user:1", src, time.Minute); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	// Mutating caller slice must not corrupt cached bytes.
	src[0] = 'X'

	got, err := c.Get(ctx, "user:1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !bytes.Equal(got, []byte("permissions:read")) {
		t.Fatalf("Get() = %q, want %q", got, "permissions:read")
	}

	if err := c.Delete(ctx, "user:1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := c.Get(ctx, "user:1"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("Get() after delete error = %v, want ErrNotFound", err)
	}
}

func TestMemoryCacheConcurrency(t *testing.T) {
	c := cache.NewMemory()
	defer func() { _ = c.Close() }()

	ctx := t.Context()
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for range 50 {
				key := "k"
				_ = c.Set(ctx, key, []byte{byte(id)}, time.Second)
				_, _ = c.Get(ctx, key)
				_ = c.Delete(ctx, key)
			}
		}(i)
	}
	wg.Wait()
}
