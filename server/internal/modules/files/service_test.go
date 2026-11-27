package files

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"tianshu/internal/platform/storage"
	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/pagination"
)

type fakeFileStore struct {
	mu            sync.Mutex
	rows          map[uuid.UUID]File
	failMarkReady error
	failDeleteRow error
	now           func() time.Time
}

func newFakeFileStore() *fakeFileStore {
	return &fakeFileStore{
		rows: make(map[uuid.UUID]File),
		now:  time.Now,
	}
}

func (f *fakeFileStore) InsertPendingFile(_ context.Context, rec File) (File, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.now().UTC()
	rec.Status = StatusPending
	rec.CreatedAt = now
	rec.UpdatedAt = now
	f.rows[rec.ID] = rec
	return rec, nil
}

func (f *fakeFileStore) MarkFileReady(_ context.Context, id uuid.UUID) (File, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failMarkReady != nil {
		return File{}, f.failMarkReady
	}
	rec, ok := f.rows[id]
	if !ok || rec.Status != StatusPending {
		return File{}, errRecordNotFound
	}
	rec.Status = StatusReady
	rec.UpdatedAt = f.now().UTC()
	f.rows[id] = rec
	return rec, nil
}

func (f *fakeFileStore) MarkFileStatus(_ context.Context, id uuid.UUID, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.rows[id]
	if !ok {
		return nil
	}
	rec.Status = status
	rec.UpdatedAt = f.now().UTC()
	f.rows[id] = rec
	return nil
}

func (f *fakeFileStore) GetReadyFileByID(_ context.Context, id uuid.UUID) (File, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.rows[id]
	if !ok || rec.Status != StatusReady {
		return File{}, errRecordNotFound
	}
	return rec, nil
}

func (f *fakeFileStore) GetFileByIDForUpdate(_ context.Context, id uuid.UUID) (File, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.rows[id]
	if !ok {
		return File{}, errRecordNotFound
	}
	return rec, nil
}

func (f *fakeFileStore) GetFileByIDForShare(ctx context.Context, id uuid.UUID) (File, error) {
	return f.GetFileByIDForUpdate(ctx, id)
}

func (f *fakeFileStore) ClaimExpiredPendingFile(_ context.Context, id uuid.UUID, pendingCutoff time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.rows[id]
	if !ok || rec.Status != StatusPending || !rec.UpdatedAt.Before(pendingCutoff) {
		return false, nil
	}
	rec.Status = StatusDeleting
	rec.UpdatedAt = f.now().UTC()
	f.rows[id] = rec
	return true, nil
}

func (f *fakeFileStore) TouchCleanupCandidate(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.rows[id]
	if ok && (rec.Status == StatusFailed || rec.Status == StatusDeleting) {
		rec.UpdatedAt = f.now().UTC()
		f.rows[id] = rec
	}
	return nil
}

func (f *fakeFileStore) DeleteFileRow(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failDeleteRow != nil {
		return f.failDeleteRow
	}
	rec, ok := f.rows[id]
	if ok && (rec.Status == StatusFailed || rec.Status == StatusDeleting) {
		delete(f.rows, id)
	}
	return nil
}

func (f *fakeFileStore) filterReady(filter ListFilter) []File {
	var out []File
	needle := strings.ToLower(filter.Q)
	for _, rec := range f.rows {
		if rec.Status != StatusReady {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(rec.OriginalName), needle) {
			continue
		}
		if len(filter.MIMETypes) > 0 && !slices.Contains(filter.MIMETypes, rec.MIMEType) {
			continue
		}
		if len(filter.Drivers) > 0 && !slices.Contains(filter.Drivers, rec.StorageDriver) {
			continue
		}
		if len(filter.Categories) > 0 && !slices.Contains(filter.Categories, rec.Category()) {
			continue
		}
		out = append(out, rec)
	}
	slices.SortFunc(out, func(a, b File) int {
		return cmp.Compare(b.CreatedAt.UnixNano(), a.CreatedAt.UnixNano())
	})
	return out
}

func (f *fakeFileStore) CountReadyFiles(_ context.Context, filter ListFilter) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return int64(len(f.filterReady(filter))), nil
}

func (f *fakeFileStore) ListReadyFiles(_ context.Context, filter ListFilter) ([]File, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rows := f.filterReady(filter)
	start := min(int(filter.Pagination.Offset()), len(rows))
	end := min(start+int(filter.Pagination.Limit()), len(rows))
	return slices.Clone(rows[start:end]), nil
}

func (f *fakeFileStore) ListCleanupCandidates(_ context.Context, drivers []string, pendingCutoff time.Time, limit int32) ([]File, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []File
	for _, rec := range f.rows {
		if len(drivers) > 0 && !slices.Contains(drivers, rec.StorageDriver) {
			continue
		}
		if rec.Status == StatusFailed || rec.Status == StatusDeleting || (rec.Status == StatusPending && rec.UpdatedAt.Before(pendingCutoff)) {
			out = append(out, rec)
		}
	}
	slices.SortFunc(out, func(a, b File) int {
		return cmp.Compare(a.UpdatedAt.UnixNano(), b.UpdatedAt.UnixNano())
	})
	if len(out) > int(limit) {
		out = out[:limit]
	}
	return out, nil
}

type failingStorage struct {
	storage.Storage
	failPut    error
	failGet    error
	failDelete error
}

func (f *failingStorage) Put(ctx context.Context, key string, r io.Reader) (int64, error) {
	if f.failPut != nil {
		return 0, f.failPut
	}
	return f.Storage.Put(ctx, key, r)
}

func (f *failingStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if f.failGet != nil {
		return nil, f.failGet
	}
	return f.Storage.Get(ctx, key)
}

func (f *failingStorage) Delete(ctx context.Context, key string) error {
	if f.failDelete != nil {
		return f.failDelete
	}
	return f.Storage.Delete(ctx, key)
}

type fakeActors struct {
	names map[uuid.UUID]string
}

func (a fakeActors) GetUsername(_ context.Context, id uuid.UUID) (string, error) {
	return a.names[id], nil
}

func (a fakeActors) ListUsernamesByIDs(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := make(map[uuid.UUID]string, len(ids))
	for _, id := range ids {
		if name, ok := a.names[id]; ok {
			out[id] = name
		}
	}
	return out, nil
}

type fakeAvatarRefs struct {
	inUse map[uuid.UUID]bool
}

func (r fakeAvatarRefs) IsAvatarFileInUse(_ context.Context, id uuid.UUID) (bool, error) {
	return r.inUse[id], nil
}

type fakeAudit struct {
	entries []AuditEntry
	fail    error
}

func (a *fakeAudit) Record(_ context.Context, entry AuditEntry) error {
	if a.fail != nil {
		return a.fail
	}
	a.entries = append(a.entries, entry)
	return nil
}

func TestServiceUploadListPreviewDeleteAndAvatarGuard(t *testing.T) {
	localStore, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	reg, err := storage.NewRegistry(storage.DriverLocal, localStore)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	st := newFakeFileStore()
	uploaderID := uuid.Must(uuid.NewV7())
	avatarRefs := fakeAvatarRefs{inUse: make(map[uuid.UUID]bool)}
	aud := &fakeAudit{}

	svc := newServiceWithStore(st, reg, ServiceConfig{
		SignedURLSecret: "test-secret-key-at-least-32-bytes",
		SignedURLTTL:    5 * time.Minute,
	}, ServiceDeps{
		Actors:    fakeActors{names: map[uuid.UUID]string{uploaderID: "alice"}},
		AvatarRef: avatarRefs,
		Audit:     aud,
	})

	ctx := context.Background()
	pngData := makePNGBytes(t, 24, 24)
	uploaded, err := svc.Upload(ctx, UploadInput{
		Filename:   "banner.png",
		Reader:     bytes.NewReader(pngData),
		UploaderID: &uploaderID,
	})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if uploaded.Status != StatusReady || uploaded.MIMEType != mimePNG || uploaded.Uploader == nil || uploaded.Uploader.Username != "alice" {
		t.Fatalf("unexpected uploaded file: %+v", uploaded)
	}
	if len(aud.entries) != 1 || aud.entries[0].Action != auditActionUpload {
		t.Fatalf("expected upload audit entry, got %+v", aud.entries)
	}

	// List hydrates uploader usernames and supports filtering.
	res, err := svc.List(ctx, ListFilter{
		Pagination: pagination.Normalize(1, 20),
		Categories: []string{CategoryImage},
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if res.Total != 1 || len(res.Items) != 1 || res.Items[0].Uploader == nil || res.Items[0].Uploader.Username != "alice" {
		t.Fatalf("unexpected list result: %+v", res)
	}

	// Signed URL works for inline preview.
	signed, err := svc.CreateSignedURL(ctx, uploaded.ID, DispositionInline)
	if err != nil {
		t.Fatalf("CreateSignedURL() error = %v", err)
	}
	token := strings.TrimPrefix(signed.URL, "/api/v1/files/signed/")
	stream, err := svc.OpenSignedContent(ctx, token)
	if err != nil {
		t.Fatalf("OpenSignedContent() error = %v", err)
	}
	gotBytes, _ := io.ReadAll(stream.Reader)
	_ = stream.Reader.Close()
	if !bytes.Equal(gotBytes, pngData) || stream.Disposition != DispositionInline {
		t.Fatalf("unexpected signed stream: len=%d disp=%q", len(gotBytes), stream.Disposition)
	}

	// Document upload always forces attachment even if inline is requested.
	pdfUploaded, err := svc.Upload(ctx, UploadInput{
		Filename:   "manual.pdf",
		Reader:     strings.NewReader("%PDF-1.4\n%%EOF\n"),
		UploaderID: &uploaderID,
	})
	if err != nil {
		t.Fatalf("Upload pdf error = %v", err)
	}
	pdfStream, err := svc.OpenContent(ctx, pdfUploaded.ID, false)
	if err != nil {
		t.Fatalf("OpenContent pdf error = %v", err)
	}
	_ = pdfStream.Reader.Close()
	if pdfStream.Disposition != DispositionAttachment {
		t.Fatalf("pdf disposition = %q, want attachment", pdfStream.Disposition)
	}

	// File referenced as avatar cannot be deleted (409 FILES_IN_USE).
	avatarRefs.inUse[uploaded.ID] = true
	err = svc.Delete(ctx, DeleteInput{FileID: uploaded.ID, ActorID: &uploaderID})
	appErr, ok := apperr.As(err)
	if !ok || appErr.Code != ErrCodeInUse {
		t.Fatalf("Delete avatar-in-use error = %v, want %s", err, ErrCodeInUse)
	}

	// Once avatar reference is cleared, deletion removes both object and DB row.
	delete(avatarRefs.inUse, uploaded.ID)
	if err := svc.Delete(ctx, DeleteInput{FileID: uploaded.ID, ActorID: &uploaderID}); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := localStore.Stat(ctx, uploaded.ObjectKey); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("expected object deleted from disk, stat err = %v", err)
	}
	if _, err := svc.Get(ctx, uploaded.ID); err == nil {
		t.Fatal("expected deleted file to return not found")
	}
}

func TestServiceUploadAndDeleteCompensationAndOrphanCleanup(t *testing.T) {
	baseLocal, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	flaky := &failingStorage{Storage: baseLocal}
	reg, err := storage.NewRegistry(storage.DriverLocal, flaky)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	st := newFakeFileStore()
	svc := newServiceWithStore(st, reg, ServiceConfig{
		SignedURLSecret: "test-secret-key-at-least-32-bytes",
	}, ServiceDeps{})

	ctx := context.Background()

	// 1) MarkFileReady fails after Put succeeds -> object is compensated immediately and row marked failed.
	st.failMarkReady = errors.New("db write failed")
	if _, err := svc.Upload(ctx, UploadInput{
		Filename: "doc.txt",
		Reader:   strings.NewReader("payload"),
	}); err == nil {
		t.Fatal("expected upload error when MarkFileReady fails")
	}
	st.failMarkReady = nil

	if len(st.rows) != 1 {
		t.Fatalf("expected 1 failed row tracked for janitor, got %d", len(st.rows))
	}
	for _, rec := range st.rows {
		if rec.Status != StatusFailed {
			t.Fatalf("row status = %q, want failed", rec.Status)
		}
		if _, err := baseLocal.Stat(ctx, rec.ObjectKey); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("expected compensated object removed from storage, stat err = %v", err)
		}
	}

	cleaned, err := svc.CleanupOrphans(ctx, 15*time.Minute, 50)
	if err != nil || cleaned != 1 || len(st.rows) != 0 {
		t.Fatalf("CleanupOrphans() = (%d, %v), remaining rows = %d", cleaned, err, len(st.rows))
	}

	// 2) Delete when storage backend temporarily fails keeps row in deleting status until janitor retries.
	rec, err := svc.Upload(ctx, UploadInput{
		Filename: "retry.txt",
		Reader:   strings.NewReader("retry-body"),
	})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	flaky.failDelete = errors.New("s3 temporary timeout")
	if err := svc.Delete(ctx, DeleteInput{FileID: rec.ID}); err != nil {
		t.Fatalf("Delete() should succeed logically while deferring storage cleanup, got %v", err)
	}
	if _, err := svc.Get(ctx, rec.ID); err == nil {
		t.Fatal("deleting file must not be visible via Get")
	}
	if st.rows[rec.ID].Status != StatusDeleting {
		t.Fatalf("row status = %q, want deleting", st.rows[rec.ID].Status)
	}

	// Storage recovers -> janitor cleans both storage object and DB row.
	flaky.failDelete = nil
	cleaned, err = svc.CleanupOrphans(ctx, 15*time.Minute, 50)
	if err != nil || cleaned != 1 || len(st.rows) != 0 {
		t.Fatalf("CleanupOrphans() after recovery = (%d, %v), remaining = %d", cleaned, err, len(st.rows))
	}
	if _, err := baseLocal.Stat(ctx, rec.ObjectKey); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("expected object removed after janitor retry, err = %v", err)
	}
}

type racingCleanupStore struct {
	*fakeFileStore
	promoteOnList uuid.UUID
}

func (r *racingCleanupStore) ListCleanupCandidates(ctx context.Context, drivers []string, pendingCutoff time.Time, limit int32) ([]File, error) {
	items, err := r.fakeFileStore.ListCleanupCandidates(ctx, drivers, pendingCutoff, limit)
	if err != nil {
		return nil, err
	}
	// Simulate an upload finishing MarkFileReady after ListCleanupCandidates returns,
	// before CleanupOrphans processes the candidate.
	if r.promoteOnList != uuid.Nil {
		_, _ = r.MarkFileReady(ctx, r.promoteOnList)
	}
	return items, nil
}

func TestServiceCleanupOrphansDoesNotDeletePromotedReadyFile(t *testing.T) {
	localStore, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	reg, err := storage.NewRegistry(storage.DriverLocal, localStore)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	baseStore := newFakeFileStore()
	id := uuid.Must(uuid.NewV7())
	key := "2026/11/" + id.String() + ".txt"
	if _, err := localStore.Put(context.Background(), key, strings.NewReader("important-data")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	baseStore.rows[id] = File{
		ID:            id,
		StorageDriver: storage.DriverLocal,
		ObjectKey:     key,
		OriginalName:  "slow-upload.txt",
		MIMEType:      mimeTXT,
		SizeBytes:     14,
		SHA256:        strings.Repeat("a", 64),
		Status:        StatusPending,
		CreatedAt:     time.Now().UTC().Add(-2 * time.Hour),
		UpdatedAt:     time.Now().UTC().Add(-2 * time.Hour),
	}

	racing := &racingCleanupStore{fakeFileStore: baseStore, promoteOnList: id}
	svc := newServiceWithStore(racing, reg, ServiceConfig{
		SignedURLSecret: "test-secret-key-at-least-32-bytes",
	}, ServiceDeps{})

	cleaned, err := svc.CleanupOrphans(context.Background(), 15*time.Minute, 50)
	if err != nil {
		t.Fatalf("CleanupOrphans() error = %v", err)
	}
	if cleaned != 0 {
		t.Fatalf("cleaned = %d, want 0 for promoted ready file", cleaned)
	}
	if _, err := localStore.Stat(context.Background(), key); err != nil {
		t.Fatalf("expected promoted ready file object intact, stat err = %v", err)
	}
	got, err := svc.Get(context.Background(), id)
	if err != nil || got.Status != StatusReady {
		t.Fatalf("expected ready file row intact, got (%+v, %v)", got, err)
	}
}

func TestServiceUploadCompensatesWhenAuditFails(t *testing.T) {
	localStore, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	reg, err := storage.NewRegistry(storage.DriverLocal, localStore)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	st := newFakeFileStore()
	aud := &fakeAudit{fail: errors.New("audit insert failed")}
	svc := newServiceWithStore(st, reg, ServiceConfig{
		SignedURLSecret: "test-secret-key-at-least-32-bytes",
	}, ServiceDeps{Audit: aud})

	_, err = svc.Upload(context.Background(), UploadInput{
		Filename: "audit.txt",
		Reader:   strings.NewReader("audit-body"),
	})
	if err == nil {
		t.Fatal("expected upload to fail when audit recording fails")
	}
	if len(st.rows) != 1 {
		t.Fatalf("expected one compensated row, got %d", len(st.rows))
	}
	for _, rec := range st.rows {
		if rec.Status != StatusFailed {
			t.Fatalf("row status = %q, want failed after audit compensation", rec.Status)
		}
		if _, statErr := localStore.Stat(context.Background(), rec.ObjectKey); !errors.Is(statErr, storage.ErrNotFound) {
			t.Fatalf("expected object compensated after audit failure, stat err = %v", statErr)
		}
	}
}

func TestServiceStorageFailuresMapTo503(t *testing.T) {
	baseLocal, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	flaky := &failingStorage{Storage: baseLocal}
	reg, err := storage.NewRegistry(storage.DriverLocal, flaky)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	st := newFakeFileStore()
	svc := newServiceWithStore(st, reg, ServiceConfig{
		SignedURLSecret: "test-secret-key-at-least-32-bytes",
	}, ServiceDeps{})

	// Put failure is an infrastructure outage: 503 plus a failed row for janitor compensation.
	flaky.failPut = errors.New("s3 unavailable")
	_, err = svc.Upload(context.Background(), UploadInput{
		Filename: "doc.txt",
		Reader:   strings.NewReader("payload"),
	})
	appErr, ok := apperr.As(err)
	if !ok || appErr.Code != ErrCodeStorageUnavailable || appErr.Status != 503 {
		t.Fatalf("Upload storage failure = %v, want 503 %s", err, ErrCodeStorageUnavailable)
	}
	if len(st.rows) != 1 {
		t.Fatalf("expected failed row tracked for janitor, got %d", len(st.rows))
	}
	for _, rec := range st.rows {
		if rec.Status != StatusFailed {
			t.Fatalf("row status = %q, want failed", rec.Status)
		}
	}

	// Get failure is also 503, and the mapped error must keep the storage unavailable code.
	flaky.failPut = nil
	uploaded, err := svc.Upload(context.Background(), UploadInput{
		Filename: "preview.txt",
		Reader:   strings.NewReader("preview-body"),
	})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	flaky.failGet = errors.New("s3 read timeout")
	_, err = svc.OpenContent(context.Background(), uploaded.ID, false)
	appErr, ok = apperr.As(err)
	if !ok || appErr.Code != ErrCodeStorageUnavailable || appErr.Status != 503 {
		t.Fatalf("OpenContent storage failure = %v, want 503 %s", err, ErrCodeStorageUnavailable)
	}
}

func TestServiceOpenContentReturns503WhenDriverUnavailable(t *testing.T) {
	localStore, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	reg, err := storage.NewRegistry(storage.DriverLocal, localStore)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	st := newFakeFileStore()
	id := uuid.Must(uuid.NewV7())
	st.rows[id] = File{
		ID:            id,
		StorageDriver: storage.DriverS3,
		ObjectKey:     "2026/11/" + id.String() + ".pdf",
		OriginalName:  "archive.pdf",
		MIMEType:      mimePDF,
		SizeBytes:     128,
		SHA256:        strings.Repeat("b", 64),
		Status:        StatusReady,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}

	svc := newServiceWithStore(st, reg, ServiceConfig{
		SignedURLSecret: "test-secret-key-at-least-32-bytes",
	}, ServiceDeps{})

	_, err = svc.OpenContent(context.Background(), id, false)
	appErr, ok := apperr.As(err)
	if !ok || appErr.Code != ErrCodeStorageUnavailable || appErr.Status != 503 {
		t.Fatalf("OpenContent unconfigured driver error = %v, want 503 %s", err, ErrCodeStorageUnavailable)
	}
}

func TestValidateAndLockAvatarFileAndSignURL(t *testing.T) {
	localStore, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	reg, err := storage.NewRegistry(storage.DriverLocal, localStore)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	st := newFakeFileStore()
	svc := newServiceWithStore(st, reg, ServiceConfig{
		SignedURLSecret: "test-secret-key-at-least-32-bytes",
		SignedURLTTL:    5 * time.Minute,
	}, ServiceDeps{})

	ownerA := uuid.Must(uuid.NewV7())
	ownerB := uuid.Must(uuid.NewV7())

	avatarRec, err := svc.UploadAvatar(t.Context(), UploadInput{
		Filename:   "me.png",
		Reader:     bytes.NewReader(makePNGBytes(t, 64, 64)),
		UploaderID: &ownerA,
	})
	if err != nil {
		t.Fatalf("UploadAvatar: %v", err)
	}
	generalImg, err := svc.Upload(t.Context(), UploadInput{
		Filename:   "general.png",
		Reader:     bytes.NewReader(makePNGBytes(t, 64, 64)),
		UploaderID: &ownerA,
	})
	if err != nil {
		t.Fatalf("Upload general png: %v", err)
	}
	docRec, err := svc.Upload(t.Context(), UploadInput{
		Filename:   "doc.txt",
		Reader:     strings.NewReader("hello"),
		UploaderID: &ownerA,
	})
	if err != nil {
		t.Fatalf("Upload doc: %v", err)
	}

	if err := svc.ValidateAndLockAvatarFile(t.Context(), avatarRec.ID, ownerA); err != nil {
		t.Fatalf("ValidateAndLockAvatarFile(ownerA) = %v, want nil", err)
	}
	signedURL, err := svc.SignAvatarURL(t.Context(), avatarRec.ID)
	if err != nil || !strings.HasPrefix(signedURL, signedURLPathPrefix) {
		t.Fatalf("SignAvatarURL() = (%q, %v), want signed prefix", signedURL, err)
	}

	// Foreign owner must be rejected with 404 (no existence leak).
	if err := svc.ValidateAndLockAvatarFile(t.Context(), avatarRec.ID, ownerB); err == nil {
		t.Fatal("expected foreign owner avatar check to fail")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Status != 404 {
		t.Fatalf("foreign owner error = %v, want 404", err)
	}

	// Non-avatar prefix images and documents must be rejected.
	for _, badID := range []uuid.UUID{generalImg.ID, docRec.ID, uuid.Must(uuid.NewV7())} {
		if err := svc.ValidateAndLockAvatarFile(t.Context(), badID, ownerA); err == nil {
			t.Fatalf("expected invalid avatar %s to be rejected", badID)
		}
	}

	// Deleting avatar row transitions status away from ready -> rejected.
	if err := st.MarkFileStatus(t.Context(), avatarRec.ID, StatusDeleting); err != nil {
		t.Fatalf("MarkFileStatus: %v", err)
	}
	if err := svc.ValidateAndLockAvatarFile(t.Context(), avatarRec.ID, ownerA); err == nil {
		t.Fatal("expected deleting avatar file to be rejected")
	}
}
