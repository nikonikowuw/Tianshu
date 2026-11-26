package cache

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"
)

// ErrNotFound is returned when a cache key does not exist or has expired.
var ErrNotFound = errors.New("cache key not found")

// Cache defines the unified caching interface backed by in-memory storage by default.
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, keys ...string) error
	Close() error
}

type memoryEntry struct {
	data      []byte
	expiresAt time.Time
}

// Memory implements Cache using an in-process map guarded by a mutex.
type Memory struct {
	mu      sync.RWMutex
	entries map[string]memoryEntry
	now     func() time.Time
}

var _ Cache = (*Memory)(nil)

// NewMemory creates a new in-process memory cache.
func NewMemory() *Memory {
	return &Memory{
		entries: make(map[string]memoryEntry),
		now:     time.Now,
	}
}

// Get retrieves a cloned copy of the cached value for key.
func (m *Memory) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	entry, ok := m.entries[key]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}
	if !entry.expiresAt.IsZero() && m.now().After(entry.expiresAt) {
		m.mu.Lock()
		if cur, exists := m.entries[key]; exists && !cur.expiresAt.IsZero() && m.now().After(cur.expiresAt) {
			delete(m.entries, key)
		}
		m.mu.Unlock()
		return nil, ErrNotFound
	}
	return slices.Clone(entry.data), nil
}

// Set stores a cloned copy of value under key with optional TTL (<= 0 means no expiration).
func (m *Memory) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = m.now().Add(ttl)
	}
	m.mu.Lock()
	m.entries[key] = memoryEntry{
		data:      slices.Clone(value),
		expiresAt: expiresAt,
	}
	m.mu.Unlock()
	return nil
}

// Delete removes one or more keys from the cache.
func (m *Memory) Delete(_ context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	m.mu.Lock()
	for _, k := range keys {
		delete(m.entries, k)
	}
	m.mu.Unlock()
	return nil
}

// Close clears stored entries.
func (m *Memory) Close() error {
	m.mu.Lock()
	clear(m.entries)
	m.mu.Unlock()
	return nil
}
