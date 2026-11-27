package storage_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"tianshu/internal/platform/storage"
)

type fakeS3Server struct {
	mu      sync.Mutex
	buckets map[string]bool
	objects map[string][]byte
}

func newFakeS3Server() *fakeS3Server {
	return &fakeS3Server{
		buckets: make(map[string]bool),
		objects: make(map[string][]byte),
	}
}

func (f *fakeS3Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/")
	bucket, key, hasKey := strings.Cut(trimmed, "/")

	f.mu.Lock()
	defer f.mu.Unlock()

	if !hasKey || key == "" {
		switch r.Method {
		case http.MethodHead:
			if !f.buckets[bucket] {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusOK)
		case http.MethodPut:
			f.buckets[bucket] = true
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}

	if !f.buckets[bucket] {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<Error><Code>NoSuchBucket</Code><Message>The specified bucket does not exist</Message></Error>`))
		return
	}

	fullKey := bucket + "/" + key
	switch r.Method {
	case http.MethodPut:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		f.objects[fullKey] = body
		w.Header().Set("ETag", `"fake-etag"`)
		w.WriteHeader(http.StatusOK)
	case http.MethodGet:
		data, ok := f.objects[fullKey]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	case http.MethodHead:
		data, ok := f.objects[fullKey]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
	case http.MethodDelete:
		delete(f.objects, fullKey)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

type nonSeekReader struct {
	r io.Reader
}

func (n nonSeekReader) Read(p []byte) (int, error) {
	return n.r.Read(p)
}

func TestS3StoragePutGetStatDeleteAndAutoCreateBucket(t *testing.T) {
	fake := newFakeS3Server()
	srv := httptest.NewServer(fake)
	defer srv.Close()

	ctx := t.Context()
	store, err := storage.NewS3(ctx, storage.S3Config{
		Endpoint:         srv.URL,
		Region:           "us-east-1",
		Bucket:           "tianshu-test",
		AccessKey:        "test-ak",
		SecretKey:        "test-sk",
		UsePathStyle:     true,
		AutoCreateBucket: true,
		HTTPClient:       srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewS3() error = %v", err)
	}
	if store.Driver() != storage.DriverS3 {
		t.Fatalf("Driver() = %q, want %q", store.Driver(), storage.DriverS3)
	}

	// Test both seekable and non-seekable streams.
	payload := "hello s3 storage"
	n, err := store.Put(ctx, "2026/11/object-1.txt", nonSeekReader{r: strings.NewReader(payload)})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if n != int64(len(payload)) {
		t.Fatalf("Put() bytes = %d, want %d", n, len(payload))
	}

	info, err := store.Stat(ctx, "2026/11/object-1.txt")
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Size != int64(len(payload)) {
		t.Fatalf("Stat() size = %d, want %d", info.Size, len(payload))
	}

	rc, err := store.Get(ctx, "2026/11/object-1.txt")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	got, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(got) != payload {
		t.Fatalf("Get() body = %q, want %q", string(got), payload)
	}

	for _, badKey := range []string{"", "../escape", "/abs/path", `a\b`} {
		if _, err := store.Put(ctx, badKey, strings.NewReader("x")); !errors.Is(err, storage.ErrInvalidKey) {
			t.Errorf("Put(%q) error = %v, want ErrInvalidKey", badKey, err)
		}
	}

	if err := store.Delete(ctx, "2026/11/object-1.txt"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := store.Stat(ctx, "2026/11/object-1.txt"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Stat() after delete error = %v, want ErrNotFound", err)
	}
	if _, err := store.Get(ctx, "2026/11/missing.txt"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Get() missing error = %v, want ErrNotFound", err)
	}

	// Missing bucket must NOT be masked as ErrNotFound on Get or silently ignored on Delete.
	missingBucketStore, err := storage.NewS3(ctx, storage.S3Config{
		Endpoint:     srv.URL,
		Region:       "ap-southeast-1",
		Bucket:       "non-existent-bucket",
		AccessKey:    "test-ak",
		SecretKey:    "test-sk",
		UsePathStyle: true,
		HTTPClient:   srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewS3 missing bucket: %v", err)
	}
	if _, err := missingBucketStore.Get(ctx, "a.txt"); err == nil || errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Get() on missing bucket error = %v, want non-ErrNotFound error", err)
	}
	if err := missingBucketStore.Delete(ctx, "a.txt"); err == nil {
		t.Fatal("Delete() on missing bucket must return error rather than silently succeeding")
	}
}
