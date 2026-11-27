package files

import (
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"tianshu/internal/shared/pagination"
)

// File lifecycle statuses persisted in files.status.
const (
	StatusPending  = "pending"
	StatusReady    = "ready"
	StatusFailed   = "failed"
	StatusDeleting = "deleting"
)

// File category filter values exposed by the list API.
const (
	CategoryImage    = "image"
	CategoryDocument = "document"
)

// Disposition modes accepted when issuing signed URLs or serving file content.
const (
	DispositionInline     = "inline"
	DispositionAttachment = "attachment"
)

// Allowed sort columns and directions for GET /api/v1/files.
const (
	SortCreatedAt    = "created_at"
	SortOriginalName = "original_name"
	SortSizeBytes    = "size_bytes"

	OrderAsc  = "asc"
	OrderDesc = "desc"
)

// UploaderRef is a lightweight projection of the user who uploaded a file.
type UploaderRef struct {
	ID       uuid.UUID
	Username string
}

// File is the domain model for a persisted file object and its metadata.
type File struct {
	ID            uuid.UUID
	UploaderID    *uuid.UUID
	Uploader      *UploaderRef
	StorageDriver string
	ObjectKey     string
	OriginalName  string
	MIMEType      string
	SizeBytes     int64
	SHA256        string
	Status        string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// IsImage reports whether the file's detected MIME type belongs to the safe image category.
func (f File) IsImage() bool {
	return isAllowedImageMIME(f.MIMEType)
}

// Category returns CategoryImage for images and CategoryDocument for all other files.
func (f File) Category() string {
	if strings.HasPrefix(strings.ToLower(f.MIMEType), "image/") {
		return CategoryImage
	}
	return CategoryDocument
}

// RequestMeta carries caller network metadata for audit entries.
type RequestMeta struct {
	IPAddress string
	UserAgent string
}

// UploadInput holds parameters for storing a general file or processed avatar.
type UploadInput struct {
	Filename   string
	Reader     io.Reader
	UploaderID *uuid.UUID
	Meta       RequestMeta
}

// DeleteInput holds parameters for deleting a stored file.
type DeleteInput struct {
	FileID  uuid.UUID
	ActorID *uuid.UUID
	Meta    RequestMeta
}

// ListFilter defines pagination, search, filtering, and sorting parameters for files.
type ListFilter struct {
	Pagination pagination.Params
	Q          string
	Categories []string
	MIMETypes  []string
	Drivers    []string
	Sort       string
	Order      string
}

// ContentStream wraps a readable file content stream along with safe HTTP response metadata.
type ContentStream struct {
	File        File
	Reader      io.ReadCloser
	Disposition string
}

// SignedURLResult contains a short-lived signed content URL and its expiry timestamp.
type SignedURLResult struct {
	URL       string
	ExpiresAt time.Time
}

// UploadPolicy exposes the server-side upload limits so clients can pre-check without
// hardcoding configuration values that may drift from the backend.
type UploadPolicy struct {
	MaxFileSizeBytes   int64
	MaxAvatarSizeBytes int64
	AllowedExtensions  []string
}
