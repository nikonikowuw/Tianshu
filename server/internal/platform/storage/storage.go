package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var (
	// ErrNotFound is returned when an object key does not exist.
	ErrNotFound = errors.New("storage object not found")
	// ErrInvalidKey is returned when an object key contains unsafe path components.
	ErrInvalidKey = errors.New("invalid storage object key")
)

// ObjectInfo describes metadata returned by Stat.
type ObjectInfo struct {
	Key  string
	Size int64
}

// Storage defines the unified object storage contract implemented by local and s3 drivers.
type Storage interface {
	Driver() string
	Put(ctx context.Context, key string, r io.Reader) (int64, error)
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	Stat(ctx context.Context, key string) (ObjectInfo, error)
}

// Local implements Storage on the local filesystem rooted at baseDir.
type Local struct {
	baseDir string
}

var _ Storage = (*Local)(nil)

// NewLocal initializes a local filesystem storage driver rooted at baseDir.
func NewLocal(baseDir string) (*Local, error) {
	abs, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, fmt.Errorf("resolve local storage root: %w", err)
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, fmt.Errorf("create local storage root: %w", err)
	}
	return &Local{baseDir: abs}, nil
}

// Driver returns "local".
func (l *Local) Driver() string {
	return "local"
}

// Put writes r atomically to key under baseDir.
func (l *Local) Put(_ context.Context, key string, r io.Reader) (int64, error) {
	target, err := l.resolvePath(key)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return 0, fmt.Errorf("create object dir: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), ".upload-*")
	if err != nil {
		return 0, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	written, err := io.Copy(tmp, r)
	if err != nil {
		_ = tmp.Close()
		return 0, fmt.Errorf("write object: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return 0, fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		return 0, fmt.Errorf("commit object: %w", err)
	}
	cleanup = false
	return written, nil
}

// Get opens key for reading.
func (l *Local) Get(_ context.Context, key string) (io.ReadCloser, error) {
	target, err := l.resolvePath(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("open object: %w", err)
	}
	return f, nil
}

// Delete removes key from disk, ignoring missing files.
func (l *Local) Delete(_ context.Context, key string) error {
	target, err := l.resolvePath(key)
	if err != nil {
		return err
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete object: %w", err)
	}
	return nil
}

// Stat returns size metadata for key.
func (l *Local) Stat(_ context.Context, key string) (ObjectInfo, error) {
	target, err := l.resolvePath(key)
	if err != nil {
		return ObjectInfo{}, err
	}
	info, err := os.Stat(target)
	if errors.Is(err, os.ErrNotExist) {
		return ObjectInfo{}, ErrNotFound
	}
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("stat object: %w", err)
	}
	return ObjectInfo{Key: key, Size: info.Size()}, nil
}

func (l *Local) resolvePath(key string) (string, error) {
	if key == "" || strings.HasPrefix(key, "/") || strings.Contains(key, "\\") {
		return "", ErrInvalidKey
	}
	cleaned := filepath.Clean(key)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", ErrInvalidKey
	}
	full := filepath.Join(l.baseDir, cleaned)
	rel, err := filepath.Rel(l.baseDir, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrInvalidKey
	}
	return full, nil
}
