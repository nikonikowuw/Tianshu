package storage_test

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"tianshu/internal/platform/storage"
)

func TestRegistryRoutesByPersistedDriver(t *testing.T) {
	localStore, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal() error = %v", err)
	}

	srv := httptest.NewServer(newFakeS3Server())
	defer srv.Close()

	s3Store, err := storage.NewS3(context.Background(), storage.S3Config{
		Endpoint:         srv.URL,
		Region:           "us-east-1",
		Bucket:           "tianshu",
		AccessKey:        "ak",
		SecretKey:        "sk",
		UsePathStyle:     true,
		AutoCreateBucket: true,
		HTTPClient:       srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewS3() error = %v", err)
	}

	// Upload a file while local was active, then switch default to s3.
	if _, err := localStore.Put(context.Background(), "legacy.txt", strings.NewReader("from-local")); err != nil {
		t.Fatalf("local Put() error = %v", err)
	}

	reg, err := storage.NewRegistry(storage.DriverS3, localStore, s3Store)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	if reg.Default().Driver() != storage.DriverS3 {
		t.Fatalf("Default().Driver() = %q, want %q", reg.Default().Driver(), storage.DriverS3)
	}

	// Write uses default (s3).
	if _, err := reg.Default().Put(context.Background(), "new.txt", strings.NewReader("from-s3")); err != nil {
		t.Fatalf("s3 Put() error = %v", err)
	}

	// Read routes legacy file to local and new file to s3.
	legacyDriver, err := reg.Driver(storage.DriverLocal)
	if err != nil {
		t.Fatalf("Driver(local) error = %v", err)
	}
	rc, err := legacyDriver.Get(context.Background(), "legacy.txt")
	if err != nil {
		t.Fatalf("local Get() error = %v", err)
	}
	body, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(body) != "from-local" {
		t.Fatalf("legacy content = %q, want from-local", string(body))
	}

	if _, err := reg.Driver("unknown"); !errors.Is(err, storage.ErrDriverNotConfigured) {
		t.Fatalf("Driver(unknown) error = %v, want ErrDriverNotConfigured", err)
	}
}
