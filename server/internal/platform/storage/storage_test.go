package storage_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tianshu/internal/platform/storage"
)

func TestLocalStoragePutGetDeleteAndTraversalGuard(t *testing.T) {
	baseDir := t.TempDir()
	store, err := storage.NewLocal(baseDir)
	if err != nil {
		t.Fatalf("NewLocal() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := t.Context()
	n, err := store.Put(ctx, "2026/11/object-1", strings.NewReader("hello tianshu"))
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if n != int64(len("hello tianshu")) {
		t.Fatalf("Put() bytes = %d, want %d", n, len("hello tianshu"))
	}

	// Directory keys must behave like missing objects (S3 parity).
	if _, err := store.Stat(ctx, "2026"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Stat(directory) error = %v, want ErrNotFound", err)
	}
	if _, err := store.Get(ctx, "2026"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Get(directory) error = %v, want ErrNotFound", err)
	}

	rc, err := store.Get(ctx, "2026/11/object-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	data, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(data) != "hello tianshu" {
		t.Fatalf("Get() content = %q, want %q", string(data), "hello tianshu")
	}

	for _, badKey := range []string{"", "../escape", "/etc/passwd", "a/../../b", `a\b`} {
		if _, err := store.Put(ctx, badKey, strings.NewReader("x")); !errors.Is(err, storage.ErrInvalidKey) {
			t.Errorf("Put(%q) error = %v, want ErrInvalidKey", badKey, err)
		}
	}

	// Symlink escape inside baseDir must be blocked by os.Root.
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("outside-secret"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outsideDir, filepath.Join(baseDir, "escape-dir")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if _, err := store.Get(ctx, "escape-dir/secret.txt"); err == nil {
		t.Fatal("expected symlink escape Get to fail")
	}
	if _, err := store.Stat(ctx, "escape-dir/secret.txt"); err == nil {
		t.Fatal("expected symlink escape Stat to fail")
	}
	if _, err := store.Put(ctx, "escape-dir/pwn.txt", strings.NewReader("pwn")); err == nil {
		t.Fatal("expected symlink escape Put to fail")
	}

	if err := store.Delete(ctx, "2026/11/object-1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := store.Stat(ctx, "2026/11/object-1"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Stat() after delete error = %v, want ErrNotFound", err)
	}
}
