package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// DriverLocal and DriverS3 are the canonical storage driver identifiers persisted in files.storage_driver.
const (
	DriverLocal = "local"
	DriverS3    = "s3"
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
// It uses os.Root to enforce kernel-level path and symlink containment within baseDir.
type Local struct {
	baseDir string
	root    *os.Root
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
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, fmt.Errorf("open local storage root: %w", err)
	}
	return &Local{baseDir: abs, root: root}, nil
}

// Close releases the underlying directory handle held by the local driver.
func (l *Local) Close() error {
	if l.root == nil {
		return nil
	}
	return l.root.Close()
}

// Driver returns "local".
func (l *Local) Driver() string {
	return DriverLocal
}

// Put writes r atomically to key under baseDir.
func (l *Local) Put(_ context.Context, key string, r io.Reader) (int64, error) {
	relTarget, err := l.resolveRelativePath(key)
	if err != nil {
		return 0, err
	}
	relDir := filepath.Dir(relTarget)
	if relDir != "." {
		if err := l.root.MkdirAll(relDir, 0o750); err != nil {
			return 0, fmt.Errorf("create object dir: %w", err)
		}
	}

	tmpName := ".upload-" + uuid.NewString()
	tmpRel := tmpName
	if relDir != "." {
		tmpRel = filepath.Join(relDir, tmpName)
	}
	tmp, err := l.root.OpenFile(tmpRel, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 0, fmt.Errorf("create temp file: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = l.root.Remove(tmpRel)
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
	if err := l.root.Rename(tmpRel, relTarget); err != nil {
		return 0, fmt.Errorf("commit object: %w", err)
	}
	cleanup = false
	return written, nil
}

// Get opens key for reading.
func (l *Local) Get(_ context.Context, key string) (io.ReadCloser, error) {
	relTarget, err := l.resolveRelativePath(key)
	if err != nil {
		return nil, err
	}
	f, err := l.root.Open(relTarget)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("open object: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("stat opened object: %w", err)
	}
	if info.IsDir() {
		_ = f.Close()
		return nil, ErrNotFound
	}
	return f, nil
}

// Delete removes key from disk, ignoring missing files.
func (l *Local) Delete(_ context.Context, key string) error {
	relTarget, err := l.resolveRelativePath(key)
	if err != nil {
		return err
	}
	if err := l.root.Remove(relTarget); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete object: %w", err)
	}
	return nil
}

// Stat returns size metadata for key.
func (l *Local) Stat(_ context.Context, key string) (ObjectInfo, error) {
	relTarget, err := l.resolveRelativePath(key)
	if err != nil {
		return ObjectInfo{}, err
	}
	info, err := l.root.Stat(relTarget)
	if errors.Is(err, os.ErrNotExist) {
		return ObjectInfo{}, ErrNotFound
	}
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("stat object: %w", err)
	}
	if info.IsDir() {
		return ObjectInfo{}, ErrNotFound
	}
	return ObjectInfo{Key: key, Size: info.Size()}, nil
}

func validateKey(key string) (string, error) {
	if key == "" || strings.HasPrefix(key, "/") || strings.Contains(key, "\\") || strings.ContainsRune(key, '\x00') {
		return "", ErrInvalidKey
	}
	cleaned := filepath.ToSlash(filepath.Clean(key))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return "", ErrInvalidKey
	}
	return cleaned, nil
}

func (l *Local) resolveRelativePath(key string) (string, error) {
	cleaned, err := validateKey(key)
	if err != nil {
		return "", err
	}
	rel := filepath.FromSlash(cleaned)
	full := filepath.Join(l.baseDir, rel)
	relCheck, err := filepath.Rel(l.baseDir, full)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
		return "", ErrInvalidKey
	}
	return rel, nil
}
