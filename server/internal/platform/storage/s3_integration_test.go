package storage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"tianshu/internal/platform/storage"
)

// TestS3IntegrationPutGetDelete exercises a real S3-compatible endpoint (MinIO in
// docker-compose). It is skipped unless TIANSHU_TEST_S3_ENDPOINT is set, so ordinary
// unit runs stay hermetic.
func TestS3IntegrationPutGetDelete(t *testing.T) {
	endpoint := os.Getenv("TIANSHU_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("set TIANSHU_TEST_S3_ENDPOINT to run real S3/MinIO integration test")
	}
	bucket := os.Getenv("TIANSHU_TEST_S3_BUCKET")
	if bucket == "" {
		bucket = "tianshu-e2e"
	}
	accessKey := os.Getenv("TIANSHU_TEST_S3_ACCESS_KEY")
	secretKey := os.Getenv("TIANSHU_TEST_S3_SECRET_KEY")
	if accessKey == "" || secretKey == "" {
		t.Fatal("TIANSHU_TEST_S3_ACCESS_KEY and TIANSHU_TEST_S3_SECRET_KEY are required")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	store, err := storage.NewS3(ctx, storage.S3Config{
		Endpoint:         endpoint,
		Region:           "us-east-1",
		Bucket:           bucket,
		AccessKey:        accessKey,
		SecretKey:        secretKey,
		UsePathStyle:     true,
		AutoCreateBucket: true,
	})
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}

	key := "integration/" + uuid.NewString() + ".txt"
	payload := []byte("minio round trip")
	if _, err := store.Put(ctx, key, bytes.NewReader(payload)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = store.Delete(cleanupCtx, key)
	})

	rc, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("Get payload = %q, %v", got, err)
	}

	info, err := store.Stat(ctx, key)
	if err != nil || info.Size != int64(len(payload)) {
		t.Fatalf("Stat = (%+v, %v), want size %d", info, err, len(payload))
	}

	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(ctx, key); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
	}
}
