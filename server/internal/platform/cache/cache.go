package cache

import (
	"container/list"
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

const (
	defaultMemoryMaxEntries = 4096
	memorySweepInterval     = time.Minute
)

type memoryEntry struct {
	data      []byte
	expiresAt time.Time
	elem      *list.Element
}

// Memory implements Cache using a bounded in-process map guarded by a mutex.
type Memory struct {
	mu         sync.RWMutex
	entries    map[string]memoryEntry
	order      *list.List
	maxEntries int
	lastSweep  time.Time
	now        func() time.Time
}

var _ Cache = (*Memory)(nil)

// NewMemory creates a new bounded in-process memory cache.
func NewMemory() *Memory {
	return &Memory{
		entries:    make(map[string]memoryEntry),
		order:      list.New(),
		maxEntries: defaultMemoryMaxEntries,
		now:        time.Now,
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
			m.removeEntryLocked(key, cur)
		}
		m.mu.Unlock()
		return nil, ErrNotFound
	}
	return slices.Clone(entry.data), nil
}

// Set stores a cloned copy of value under key with optional TTL (<= 0 means no expiration).
func (m *Memory) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	now := m.now()
	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = now.Add(ttl)
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := m.entries[key]; ok {
		m.order.MoveToBack(existing.elem)
		existing.data = slices.Clone(value)
		existing.expiresAt = expiresAt
		m.entries[key] = existing
		return nil
	}

	maxCap := m.maxEntries
	if maxCap <= 0 {
		maxCap = defaultMemoryMaxEntries
	}
	if len(m.entries) >= maxCap {
		if m.lastSweep.IsZero() || !now.Before(m.lastSweep.Add(memorySweepInterval)) {
			for k, entry := range m.entries {
				if !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
					m.removeEntryLocked(k, entry)
				}
			}
			m.lastSweep = now
		}
		for len(m.entries) >= maxCap {
			oldest := m.order.Front()
			if oldest == nil {
				break
			}
			oldestKey, _ := oldest.Value.(string)
			if entry, ok := m.entries[oldestKey]; ok {
				m.removeEntryLocked(oldestKey, entry)
			} else {
				m.order.Remove(oldest)
			}
		}
	}

	elem := m.order.PushBack(key)
	m.entries[key] = memoryEntry{
		data:      slices.Clone(value),
		expiresAt: expiresAt,
		elem:      elem,
	}
	return nil
}

// Delete removes one or more keys from the cache.
func (m *Memory) Delete(_ context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	m.mu.Lock()
	for _, k := range keys {
		if entry, ok := m.entries[k]; ok {
			m.removeEntryLocked(k, entry)
		}
	}
	m.mu.Unlock()
	return nil
}

// Close clears stored entries.
func (m *Memory) Close() error {
	m.mu.Lock()
	clear(m.entries)
	m.order.Init()
	m.mu.Unlock()
	return nil
}

func (m *Memory) removeEntryLocked(key string, entry memoryEntry) {
	delete(m.entries, key)
	if entry.elem != nil {
		m.order.Remove(entry.elem)
	}
}
