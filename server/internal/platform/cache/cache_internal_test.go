package cache

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

func TestMemoryCacheExpirationSweepEvictionAndClose(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	m := NewMemory()
	m.now = func() time.Time { return now }
	m.maxEntries = 3

	ctx := t.Context()

	// Positive TTL expires after deadline.
	if err := m.Set(ctx, "short", []byte("v1"), 10*time.Second); err != nil {
		t.Fatalf("Set(short) error = %v", err)
	}
	// Non-positive TTL never expires.
	if err := m.Set(ctx, "forever", []byte("v0"), 0); err != nil {
		t.Fatalf("Set(forever) error = %v", err)
	}

	now = now.Add(11 * time.Second)
	if _, err := m.Get(ctx, "short"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(short) after expiry = %v, want ErrNotFound", err)
	}
	got, err := m.Get(ctx, "forever")
	if err != nil || !bytes.Equal(got, []byte("v0")) {
		t.Fatalf("Get(forever) = (%q, %v), want (v0, nil)", got, err)
	}

	// Capacity bound + throttled sweep + LRU eviction.
	// Reset with maxEntries=3: insert k1 (expires in 5s), k2 (expires in 1m), k3 (expires in 1m).
	_ = m.Close()
	if _, err := m.Get(ctx, "forever"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Close = %v, want ErrNotFound", err)
	}

	if err := m.Set(ctx, "k1", []byte("1"), 5*time.Second); err != nil {
		t.Fatalf("Set(k1): %v", err)
	}
	if err := m.Set(ctx, "k2", []byte("2"), time.Minute); err != nil {
		t.Fatalf("Set(k2): %v", err)
	}
	if err := m.Set(ctx, "k3", []byte("3"), time.Minute); err != nil {
		t.Fatalf("Set(k3): %v", err)
	}

	// Advance past k1 expiry and past sweep interval so Set(k4) sweeps k1 instead of evicting k2.
	now = now.Add(memorySweepInterval + time.Second)
	if err := m.Set(ctx, "k4", []byte("4"), 5*time.Minute); err != nil {
		t.Fatalf("Set(k4): %v", err)
	}
	if _, err := m.Get(ctx, "k1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected expired k1 to be swept, got err=%v", err)
	}
	// k2, k3, k4 are active; updating k2 refreshes its LRU order so k3 becomes oldest.
	if err := m.Set(ctx, "k2", []byte("2b"), 5*time.Minute); err != nil {
		t.Fatalf("Set(k2 update): %v", err)
	}
	// Adding k5 while at maxEntries=3 with no expired entries must evict oldest (k3).
	if err := m.Set(ctx, "k5", []byte("5"), 5*time.Minute); err != nil {
		t.Fatalf("Set(k5): %v", err)
	}
	if _, err := m.Get(ctx, "k3"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected LRU entry k3 to be evicted, got err=%v", err)
	}
	for _, key := range []string{"k2", "k4", "k5"} {
		if _, err := m.Get(ctx, key); err != nil {
			t.Fatalf("expected %s to remain cached, got %v", key, err)
		}
	}

	// Empty Delete is a no-op; multi-key Delete removes all named keys.
	if err := m.Delete(ctx); err != nil {
		t.Fatalf("empty Delete(): %v", err)
	}
	if err := m.Delete(ctx, "k2", "k4", "missing"); err != nil {
		t.Fatalf("multi Delete(): %v", err)
	}
	if _, err := m.Get(ctx, "k2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(k2) after Delete = %v, want ErrNotFound", err)
	}
	if _, err := m.Get(ctx, "k4"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(k4) after Delete = %v, want ErrNotFound", err)
	}
}
