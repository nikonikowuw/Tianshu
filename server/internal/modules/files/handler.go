package files

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"tianshu/internal/platform/logger"
	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/authctx"
)

const (
	multipartFormOverheadBytes = int64(1 << 20)
	multipartMemoryBytes       = int64(2 << 20)
	maxSignedURLBodyBytes      = int64(64 << 10)
	fileTransferDeadline       = 60 * time.Second
)

// Handler exposes HTTP endpoints for the files module.
type Handler struct {
	svc *Service
}

// NewHandler creates a Handler bound to svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) upload(c *gin.Context) {
	h.handleMultipartUpload(c, h.svc.MaxFileSizeBytes(), false)
}

func (h *Handler) uploadAvatar(c *gin.Context) {
	h.handleMultipartUpload(c, h.svc.MaxAvatarSizeBytes(), true)
}

func (h *Handler) handleMultipartUpload(c *gin.Context, maxBytes int64, isAvatar bool) {
	extendTransferDeadlines(c.Writer)

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes+multipartFormOverheadBytes)

	if err := c.Request.ParseMultipartForm(min(maxBytes, multipartMemoryBytes)); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) || errors.Is(err, multipart.ErrMessageTooLarge) {
			_ = c.Error(errFileTooLarge())
			return
		}
		_ = c.Error(apperr.BadRequest(apperr.CodeBadRequest, err))
		return
	}
	if c.Request.MultipartForm != nil {
		defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
	}

	fh, err := c.FormFile("file")
	if err != nil {
		_ = c.Error(errMissingFile())
		return
	}
	if fh.Size > maxBytes {
		_ = c.Error(errFileTooLarge())
		return
	}

	src, err := fh.Open()
	if err != nil {
		_ = c.Error(apperr.BadRequest(apperr.CodeBadRequest, err))
		return
	}
	defer func() { _ = src.Close() }()

	var (
		uploaderID *uuid.UUID
		sessionID  uuid.UUID
	)
	if id, ok := authctx.IdentityFromContext(c.Request.Context()); ok {
		uid := id.UserID
		uploaderID = &uid
		sessionID = id.SessionID
	}

	input := UploadInput{
		Filename:   fh.Filename,
		Reader:     src,
		UploaderID: uploaderID,
		SessionID:  sessionID,
		Meta: RequestMeta{
			IPAddress: c.ClientIP(),
			UserAgent: c.Request.UserAgent(),
		},
	}

	var stored File
	if isAvatar {
		stored, err = h.svc.UploadAvatar(c.Request.Context(), input)
	} else {
		stored, err = h.svc.Upload(c.Request.Context(), input)
	}
	if err != nil {
		_ = c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, toFileDTO(stored))
}

func (h *Handler) uploadPolicy(c *gin.Context) {
	c.JSON(http.StatusOK, toUploadPolicyDTO(h.svc.UploadPolicy()))
}

func (h *Handler) list(c *gin.Context) {
	filter, err := parseListFilter(c)
	if err != nil {
		_ = c.Error(err)
		return
	}

	res, err := h.svc.List(c.Request.Context(), filter)
	if err != nil {
		_ = c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toFileListResponse(res))
}

func (h *Handler) get(c *gin.Context) {
	id, err := parseUUIDParam(c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		return
	}

	rec, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toFileDTO(rec))
}

func (h *Handler) content(c *gin.Context) {
	extendTransferDeadlines(c.Writer)

	id, err := parseUUIDParam(c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		return
	}

	download := strings.TrimSpace(c.Query("download"))
	forceDownload := download == "1" || strings.EqualFold(download, "true")

	stream, err := h.svc.OpenContent(c.Request.Context(), id, forceDownload)
	if err != nil {
		_ = c.Error(err)
		return
	}
	h.writeContentStream(c, stream)
}

func (h *Handler) createSignedURL(c *gin.Context) {
	id, err := parseUUIDParam(c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		return
	}

	var req createSignedURLRequest
	if c.Request.Body != nil && c.Request.Body != http.NoBody {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSignedURLBodyBytes)
		if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				_ = c.Error(apperr.New(http.StatusRequestEntityTooLarge, apperr.CodeTooLarge))
				return
			}
			_ = c.Error(err)
			return
		}
	}

	res, err := h.svc.CreateSignedURL(c.Request.Context(), id, req.Disposition)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toSignedURLDTO(res))
}

func (h *Handler) signedContent(c *gin.Context) {
	extendTransferDeadlines(c.Writer)

	token := c.Param("token")
	stream, err := h.svc.OpenSignedContent(c.Request.Context(), token)
	if err != nil {
		_ = c.Error(err)
		return
	}
	h.writeContentStream(c, stream)
}

func (h *Handler) deleteFile(c *gin.Context) {
	id, err := parseUUIDParam(c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		return
	}

	var actorID *uuid.UUID
	if identity, ok := authctx.IdentityFromContext(c.Request.Context()); ok {
		uid := identity.UserID
		actorID = &uid
	}

	err = h.svc.Delete(c.Request.Context(), DeleteInput{
		FileID:  id,
		ActorID: actorID,
		Meta: RequestMeta{
			IPAddress: c.ClientIP(),
			UserAgent: c.Request.UserAgent(),
		},
	})
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) writeContentStream(c *gin.Context, stream ContentStream) {
	defer func() { _ = stream.Reader.Close() }()

	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	c.Header("Content-Type", stream.File.MIMEType)
	c.Header("Content-Length", strconv.FormatInt(stream.File.SizeBytes, 10))
	c.Header("Content-Disposition", formatContentDisposition(stream.Disposition, stream.File.OriginalName))
	c.Header("Cache-Control", "private, no-store")
	c.Status(http.StatusOK)

	if _, err := io.Copy(c.Writer, stream.Reader); err != nil && !isClientDisconnect(err) {
		logger.FromContext(c.Request.Context(), h.svc.log).Warn("failed to stream file content",
			"file_id", stream.File.ID.String(),
			"storage_driver", stream.File.StorageDriver,
			"error", err,
		)
	}
}

func isClientDisconnect(err error) bool {
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ECONNRESET)
}

func formatContentDisposition(disposition, filename string) string {
	if disposition != DispositionInline {
		disposition = DispositionAttachment
	}
	asciiFallback := toASCIIFilename(filename)
	escapedUTF8 := encodeRFC8187(filename)
	return fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`, disposition, asciiFallback, escapedUTF8)
}

// encodeRFC8187 percent-encodes any byte outside RFC 8187 attr-char so strict HTTP parsers
// accept filenames containing '=', ':', '@', quotes, spaces, or UTF-8 sequences.
func encodeRFC8187(s string) string {
	const upperHex = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if isRFC8187AttrChar(ch) {
			b.WriteByte(ch)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(upperHex[ch>>4])
		b.WriteByte(upperHex[ch&0x0f])
	}
	return b.String()
}

func isRFC8187AttrChar(c byte) bool {
	if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
		return true
	}
	switch c {
	case '!', '#', '$', '&', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	default:
		return false
	}
}

func toASCIIFilename(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if r >= 0x20 && r <= 0x7e && r != '"' && r != '\\' && r != ';' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	res := strings.TrimSpace(b.String())
	if res == "" {
		return "file"
	}
	return res
}

func parseUUIDParam(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil || id == uuid.Nil {
		return uuid.Nil, apperr.Validation([]apperr.FieldError{{
			Field:   "id",
			Message: "VALIDATION_UUID",
		}})
	}
	return id, nil
}

func extendTransferDeadlines(w http.ResponseWriter) {
	rc := http.NewResponseController(w)
	deadline := time.Now().Add(fileTransferDeadline)
	_ = rc.SetReadDeadline(deadline)
	_ = rc.SetWriteDeadline(deadline)
}
