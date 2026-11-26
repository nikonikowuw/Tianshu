package storage_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"tianshu/internal/platform/storage"
)

func TestLocalStoragePutGetDeleteAndTraversalGuard(t *testing.T) {
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal() error = %v", err)
	}

	ctx := context.Background()
	n, err := store.Put(ctx, "2026/11/object-1", strings.NewReader("hello tianshu"))
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if n != int64(len("hello tianshu")) {
		t.Fatalf("Put() bytes = %d, want %d", n, len("hello tianshu"))
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

	if err := store.Delete(ctx, "2026/11/object-1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := store.Stat(ctx, "2026/11/object-1"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Stat() after delete error = %v, want ErrNotFound", err)
	}
}
