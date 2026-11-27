package files

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"io"
	"maps"
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
	mu              sync.Mutex
	rows            map[uuid.UUID]File
	failMarkReady   error
	failDeleteRow   error
	failListCleanup error
	touchedIDs      []uuid.UUID
	now             func() time.Time
}

func newFakeFileStore() *fakeFileStore {
	return &fakeFileStore{
		rows: make(map[uuid.UUID]File),
		now:  time.Now,
	}
}

func (f *fakeFileStore) snapshot() map[uuid.UUID]File {
	f.mu.Lock()
	defer f.mu.Unlock()
	return maps.Clone(f.rows)
}

func (f *fakeFileStore) restore(snap map[uuid.UUID]File) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = snap
}

type fakeTxRunner struct {
	store *fakeFileStore
	audit *fakeAudit
}

func (r *fakeTxRunner) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	rowSnap := r.store.snapshot()
	var auditLen int
	if r.audit != nil {
		auditLen = len(r.audit.entries)
	}
	if err := fn(ctx); err != nil {
		r.store.restore(rowSnap)
		if r.audit != nil {
			r.audit.entries = r.audit.entries[:auditLen]
		}
		return err
	}
	return nil
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
		f.touchedIDs = append(f.touchedIDs, id)
	}
	return nil
}

func (f *fakeFileStore) DeleteFileRow(_ context.Context, id uuid.UUID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failDeleteRow != nil {
		return false, f.failDeleteRow
	}
	rec, ok := f.rows[id]
	if ok && (rec.Status == StatusFailed || rec.Status == StatusDeleting) {
		delete(f.rows, id)
		return true, nil
	}
	return false, nil
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

func (f *fakeFileStore) ListCleanupCandidates(_ context.Context, drivers []string, pendingCutoff, deletingCutoff time.Time, limit int32) ([]File, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failListCleanup != nil {
		return nil, f.failListCleanup
	}
	var out []File
	for _, rec := range f.rows {
		if len(drivers) > 0 && !slices.Contains(drivers, rec.StorageDriver) {
			continue
		}
		eligible := rec.Status == StatusFailed ||
			(rec.Status == StatusDeleting && rec.UpdatedAt.Before(deletingCutoff)) ||
			(rec.Status == StatusPending && rec.UpdatedAt.Before(pendingCutoff))
		if eligible {
			out = append(out, rec)
		}
	}
	slices.SortFunc(out, func(a, b File) int {
		if c := cmp.Compare(a.UpdatedAt.UnixNano(), b.UpdatedAt.UnixNano()); c != 0 {
			return c
		}
		return strings.Compare(a.ID.String(), b.ID.String())
	})
	if len(out) > int(limit) {
		out = out[:limit]
	}
	return out, nil
}

type failingStorage struct {
	storage.Storage
	driverOverride string
	failPut        error
	failGet        error
	failDelete     error
	failDeleteKey  string
}

func (f *failingStorage) Driver() string {
	if f.driverOverride != "" {
		return f.driverOverride
	}
	return f.Storage.Driver()
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
	if f.failDelete != nil && (f.failDeleteKey == "" || f.failDeleteKey == key) {
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

type fakeAvatarCaller struct {
	err error
}

func (c fakeAvatarCaller) EnsureActiveAvatarCaller(_ context.Context, _, _ uuid.UUID) error {
	return c.err
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
		Tx:        &fakeTxRunner{store: st, audit: aud},
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

	// List hydrates uploader usernames and supports filtering (including case/whitespace normalization).
	res, err := svc.List(ctx, ListFilter{
		Pagination: pagination.Normalize(1, 20),
		Categories: []string{" IMAGE ", "image"},
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
	gotBytes, err := io.ReadAll(stream.Reader)
	_ = stream.Reader.Close()
	if err != nil {
		t.Fatalf("read signed stream: %v", err)
	}
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
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	st := newFakeFileStore()
	st.now = func() time.Time { return now }
	svc := newServiceWithStore(st, reg, ServiceConfig{
		SignedURLSecret: "test-secret-key-at-least-32-bytes",
	}, ServiceDeps{
		Tx: &fakeTxRunner{store: st},
	})
	svc.now = func() time.Time { return now }

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

	// 2) Delete when storage backend temporarily fails keeps row in deleting status until janitor retries past grace window.
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

	// Within grace period (< 1m), janitor does not race with an in-flight Delete().
	flaky.failDelete = nil
	cleanedEarly, err := svc.CleanupOrphans(ctx, 15*time.Minute, 50)
	if err != nil || cleanedEarly != 0 || len(st.rows) != 1 {
		t.Fatalf("CleanupOrphans() within deleting grace window = (%d, %v), rows = %d, want (0, nil, 1)", cleanedEarly, err, len(st.rows))
	}

	// After grace period passes, janitor cleans both storage object and DB row.
	now = now.Add(2 * time.Minute)
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

func (r *racingCleanupStore) ListCleanupCandidates(ctx context.Context, drivers []string, pendingCutoff, deletingCutoff time.Time, limit int32) ([]File, error) {
	items, err := r.fakeFileStore.ListCleanupCandidates(ctx, drivers, pendingCutoff, deletingCutoff, limit)
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

func TestServiceUploadAndDeleteRollbackWhenAuditFails(t *testing.T) {
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
	}, ServiceDeps{
		Tx:    &fakeTxRunner{store: st, audit: aud},
		Audit: aud,
	})

	// Upload audit failure rolls back MarkFileReady and triggers compensation to failed + object removal.
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

	// Upload succeeds when audit works, then Delete fails on audit -> tx rolls back, row stays ready and object intact.
	aud.fail = nil
	ready, err := svc.Upload(context.Background(), UploadInput{
		Filename: "keep.txt",
		Reader:   strings.NewReader("keep-body"),
	})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	aud.fail = errors.New("audit delete failed")
	if err := svc.Delete(context.Background(), DeleteInput{FileID: ready.ID}); err == nil {
		t.Fatal("expected Delete to fail when audit recording fails")
	}
	if got, err := svc.Get(context.Background(), ready.ID); err != nil || got.Status != StatusReady {
		t.Fatalf("expected file to remain ready after rolled-back Delete, got (%+v, %v)", got, err)
	}
	if _, err := localStore.Stat(context.Background(), ready.ObjectKey); err != nil {
		t.Fatalf("expected storage object intact after rolled-back Delete, stat err = %v", err)
	}
}

func TestServiceMultiDriverRoutingAndNotFoundMapping(t *testing.T) {
	localStore, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal(local): %v", err)
	}
	s3Backing, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal(s3-fake): %v", err)
	}
	fakeS3 := &failingStorage{Storage: s3Backing, driverOverride: storage.DriverS3}
	reg, err := storage.NewRegistry(storage.DriverLocal, localStore, fakeS3)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	st := newFakeFileStore()
	svc := newServiceWithStore(st, reg, ServiceConfig{
		SignedURLSecret: "test-secret-key-at-least-32-bytes",
	}, ServiceDeps{
		Tx: &fakeTxRunner{store: st},
	})

	ctx := context.Background()
	localRec, err := svc.Upload(ctx, UploadInput{
		Filename: "local.txt",
		Reader:   strings.NewReader("from-local"),
	})
	if err != nil {
		t.Fatalf("Upload local: %v", err)
	}

	// Seed an existing S3-backed ready record while default driver is local.
	s3ID := uuid.Must(uuid.NewV7())
	s3Key := "2026/11/" + s3ID.String() + ".txt"
	if _, err := fakeS3.Put(ctx, s3Key, strings.NewReader("from-s3")); err != nil {
		t.Fatalf("fakeS3.Put: %v", err)
	}
	st.rows[s3ID] = File{
		ID:            s3ID,
		StorageDriver: storage.DriverS3,
		ObjectKey:     s3Key,
		OriginalName:  "remote.txt",
		MIMEType:      mimeTXT,
		SizeBytes:     7,
		SHA256:        strings.Repeat("c", 64),
		Status:        StatusReady,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}

	// OpenContent routes to s3 driver and reads s3 payload.
	stream, err := svc.OpenContent(ctx, s3ID, false)
	if err != nil {
		t.Fatalf("OpenContent(s3) error = %v", err)
	}
	body, err := io.ReadAll(stream.Reader)
	_ = stream.Reader.Close()
	if err != nil || string(body) != "from-s3" {
		t.Fatalf("OpenContent(s3) body = (%q, %v), want from-s3", string(body), err)
	}

	// Deleting s3 record removes only the s3 object while keeping local object intact.
	if err := svc.Delete(ctx, DeleteInput{FileID: s3ID}); err != nil {
		t.Fatalf("Delete(s3) error = %v", err)
	}
	if _, err := fakeS3.Stat(ctx, s3Key); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("expected s3 object deleted, stat err = %v", err)
	}
	if _, err := localStore.Stat(ctx, localRec.ObjectKey); err != nil {
		t.Fatalf("expected local object untouched, stat err = %v", err)
	}

	// Missing physical object for a ready row maps storage.ErrNotFound -> 404 FILES_NOT_FOUND.
	if err := localStore.Delete(ctx, localRec.ObjectKey); err != nil {
		t.Fatalf("remove local object: %v", err)
	}
	_, err = svc.OpenContent(ctx, localRec.ID, false)
	appErr, ok := apperr.As(err)
	if !ok || appErr.Status != 404 || appErr.Code != ErrCodeNotFound {
		t.Fatalf("OpenContent missing object = %v, want 404 %s", err, ErrCodeNotFound)
	}
}

func TestServiceCleanupOrphansErrorsBackoffAndCancellation(t *testing.T) {
	baseLocal, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	flaky := &failingStorage{Storage: baseLocal}
	reg, err := storage.NewRegistry(storage.DriverLocal, flaky)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	now := time.Date(2026, 11, 27, 13, 0, 0, 0, time.UTC)
	st := newFakeFileStore()
	st.now = func() time.Time { return now }
	svc := newServiceWithStore(st, reg, ServiceConfig{
		SignedURLSecret: "test-secret-key-at-least-32-bytes",
	}, ServiceDeps{})
	svc.now = func() time.Time { return now }

	// 1) Store query failure returns *apperr.Error (500 INTERNAL_ERROR).
	st.failListCleanup = errors.New("db down")
	_, err = svc.CleanupOrphans(context.Background(), 15*time.Minute, 10)
	appErr, ok := apperr.As(err)
	if !ok || appErr.Status != 500 || appErr.Code != apperr.CodeInternal {
		t.Fatalf("CleanupOrphans DB error = %v, want 500 %s", err, apperr.CodeInternal)
	}
	st.failListCleanup = nil

	// 2) Storage delete failure on first candidate touches it (back-off) and still cleans second candidate.
	idFail := uuid.Must(uuid.NewV7())
	keyFail := "2026/11/" + idFail.String() + ".txt"
	idOK := uuid.Must(uuid.NewV7())
	keyOK := "2026/11/" + idOK.String() + ".txt"
	if _, err := baseLocal.Put(context.Background(), keyFail, strings.NewReader("a")); err != nil {
		t.Fatalf("put fail: %v", err)
	}
	if _, err := baseLocal.Put(context.Background(), keyOK, strings.NewReader("b")); err != nil {
		t.Fatalf("put ok: %v", err)
	}
	st.rows[idFail] = File{
		ID: idFail, StorageDriver: storage.DriverLocal, ObjectKey: keyFail,
		Status: StatusFailed, UpdatedAt: now.Add(-10 * time.Minute),
	}
	st.rows[idOK] = File{
		ID: idOK, StorageDriver: storage.DriverLocal, ObjectKey: keyOK,
		Status: StatusFailed, UpdatedAt: now.Add(-5 * time.Minute),
	}

	flaky.failDelete = errors.New("permission denied")
	flaky.failDeleteKey = keyFail
	cleaned, err := svc.CleanupOrphans(context.Background(), 15*time.Minute, 10)
	if err != nil || cleaned != 1 {
		t.Fatalf("CleanupOrphans partial = (%d, %v), want (1, nil)", cleaned, err)
	}
	if !slices.Contains(st.touchedIDs, idFail) {
		t.Fatalf("expected failed candidate %s to be touched for backoff, touched=%v", idFail, st.touchedIDs)
	}
	if _, exists := st.rows[idOK]; exists {
		t.Fatal("expected second candidate to be cleaned despite first candidate failing")
	}

	// 3) Context cancellation returns *apperr.Error unwrapping context.Canceled.
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = svc.CleanupOrphans(cancelCtx, 15*time.Minute, 10)
	appErr, ok = apperr.As(err)
	if !ok || appErr.Status != 500 || appErr.Code != apperr.CodeInternal || !errors.Is(err, context.Canceled) {
		t.Fatalf("CleanupOrphans canceled = %v, want 500 %s wrapping context.Canceled", err, apperr.CodeInternal)
	}
}

func TestServiceListFilterValidationAndWeakSigner(t *testing.T) {
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
		SignedURLSecret: "short", // intentionally < 32 bytes to verify fail-closed signer
	}, ServiceDeps{})

	// Multiple invalid fields directly at service layer are all collected and sorted.
	tooMany := make([]string, maxMultiFilterLen+1)
	for i := range tooMany {
		tooMany[i] = "application/x-type-" + strings.Repeat("a", i+1)
	}
	_, err = svc.List(context.Background(), ListFilter{
		Q:          "bad\x00query",
		Categories: []string{"video"},
		Drivers:    []string{"gcs"},
		MIMETypes:  tooMany,
		Sort:       "bad",
		Order:      "sideways",
	})
	appErr, ok := apperr.As(err)
	if !ok || appErr.Status != 422 || len(appErr.Fields) != 6 {
		t.Fatalf("direct List invalid filter = %v, want 422 with 6 sorted errors", err)
	}
	for i := 1; i < len(appErr.Fields); i++ {
		if appErr.Fields[i-1].Field >= appErr.Fields[i].Field {
			t.Fatalf("errors not sorted ascending: %+v", appErr.Fields)
		}
	}

	// Invalid UTF-8 and overlong Q / mimeType item rejected with 422.
	for _, bad := range []ListFilter{
		{Q: string([]byte{0xff, 0xfe})},
		{Q: strings.Repeat("a", maxSearchQueryLen+1)},
		{MIMETypes: []string{"image/png\x00"}},
		{MIMETypes: []string{strings.Repeat("x", maxMIMETypeFilterLen+1)}},
	} {
		_, err := svc.List(context.Background(), bad)
		if appErr, ok := apperr.As(err); !ok || appErr.Status != 422 {
			t.Fatalf("List(%+v) = %v, want 422", bad, err)
		}
	}

	// Weak/empty signing secret fails closed on both CreateSignedURL and OpenSignedContent.
	rec, err := svc.Upload(context.Background(), UploadInput{
		Filename: "a.txt",
		Reader:   strings.NewReader("abc"),
	})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if _, err := svc.CreateSignedURL(context.Background(), rec.ID, DispositionAttachment); err == nil {
		t.Fatal("expected CreateSignedURL with weak secret to fail")
	}
	if _, err := svc.OpenSignedContent(context.Background(), "abc.def"); err == nil {
		t.Fatal("expected OpenSignedContent with weak secret to fail")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeInvalidSignedURL {
		t.Fatalf("OpenSignedContent weak secret err = %v, want %s", err, ErrCodeInvalidSignedURL)
	}
}

func TestServiceUploadAvatarEnforcesActiveCaller(t *testing.T) {
	localStore, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	reg, err := storage.NewRegistry(storage.DriverLocal, localStore)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	st := newFakeFileStore()
	caller := &fakeAvatarCaller{}
	svc := newServiceWithStore(st, reg, ServiceConfig{
		SignedURLSecret: "test-secret-key-at-least-32-bytes",
	}, ServiceDeps{
		AvatarCaller: caller,
	})

	pngData := makePNGBytes(t, 16, 16)

	// Missing UploaderID is rejected with 401.
	if _, err := svc.UploadAvatar(context.Background(), UploadInput{
		Filename: "me.png",
		Reader:   bytes.NewReader(pngData),
	}); err == nil {
		t.Fatal("expected UploadAvatar with nil UploaderID to fail")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Status != 401 {
		t.Fatalf("nil UploaderID err = %v, want 401", err)
	}

	// Revoked session / disabled user rejected before creating any DB row.
	uid := uuid.Must(uuid.NewV7())
	caller.err = apperr.Forbidden("AUTH_ACCOUNT_DISABLED")
	if _, err := svc.UploadAvatar(context.Background(), UploadInput{
		Filename:   "me.png",
		Reader:     bytes.NewReader(pngData),
		UploaderID: &uid,
	}); err == nil {
		t.Fatal("expected disabled caller to be rejected")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Status != 403 || appErr.Code != "AUTH_ACCOUNT_DISABLED" {
		t.Fatalf("disabled caller err = %v, want 403 AUTH_ACCOUNT_DISABLED", err)
	}
	if len(st.rows) != 0 {
		t.Fatalf("expected zero rows created when caller check fails, got %d", len(st.rows))
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
