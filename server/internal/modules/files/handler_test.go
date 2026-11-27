package files

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"tianshu/internal/platform/httpserver"
	"tianshu/internal/platform/i18n"
	"tianshu/internal/platform/storage"
	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/authctx"
)

func newTestFilesEngine(
	t *testing.T,
	perms map[string]bool,
	maxFileBytes int64,
	avatarRefs fakeAvatarRefs,
) (*gin.Engine, *Service, uuid.UUID) {
	t.Helper()

	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle: %v", err)
	}
	if err := bundle.RegisterFS("files", LocalesFS, "locales"); err != nil {
		t.Fatalf("RegisterFS: %v", err)
	}
	if err := bundle.Validate(); err != nil {
		t.Fatalf("Validate i18n: %v", err)
	}

	engine, err := httpserver.NewEngine(httpserver.Options{
		Env:  "test",
		I18n: bundle,
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	localStore, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	reg, err := storage.NewRegistry(storage.DriverLocal, localStore)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	actorID := uuid.Must(uuid.NewV7())
	svc := newServiceWithStore(newFakeFileStore(), reg, ServiceConfig{
		MaxFileSizeBytes:   maxFileBytes,
		MaxAvatarSizeBytes: 2 << 20,
		SignedURLSecret:    "contract-test-secret-at-least-32-bytes",
		SignedURLTTL:       5 * time.Minute,
	}, ServiceDeps{
		Actors:    fakeActors{names: map[uuid.UUID]string{actorID: "tester"}},
		AvatarRef: avatarRefs,
	})
	handler := NewHandler(svc)

	apiV1 := engine.Group("/api/v1")
	handler.RegisterPublicRoutes(apiV1)

	authed := apiV1.Group("")
	authed.Use(func(c *gin.Context) {
		if c.GetHeader("Authorization") == "" {
			_ = c.Error(apperr.Unauthorized(apperr.CodeUnauthorized))
			c.Abort()
			return
		}
		c.Request = c.Request.WithContext(authctx.WithIdentity(c.Request.Context(), authctx.Identity{
			UserID:   actorID,
			Username: "tester",
		}))
		c.Next()
	})

	requirePerm := func(code string) gin.HandlerFunc {
		return func(c *gin.Context) {
			if !perms[code] {
				_ = c.Error(apperr.Forbidden(apperr.CodeForbidden))
				c.Abort()
				return
			}
			c.Next()
		}
	}

	handler.RegisterRoutes(authed, RoutePermissions{
		RequireRead:   requirePerm("files:read"),
		RequireUpload: requirePerm("files:upload"),
		RequireDelete: requirePerm("files:delete"),
	})

	return engine, svc, actorID
}

func makeMultipartFileRequest(t *testing.T, url, fieldName, filename string, payload []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile(fieldName, filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatalf("part.Write: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("mw.Close: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, url, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer test-token")
	return req
}

func TestFilesHTTPContracts(t *testing.T) {
	avatarRefs := fakeAvatarRefs{inUse: make(map[uuid.UUID]bool)}
	engine, _, _ := newTestFilesEngine(t, map[string]bool{
		"files:read":   true,
		"files:upload": true,
		"files:delete": true,
	}, 1024, avatarRefs)

	// 401 when unauthenticated.
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/files", nil))
	if rec.Code != http.StatusUnauthorized || rec.Header().Get("X-Request-Id") == "" {
		t.Fatalf("unauthenticated GET /files: status=%d reqID=%q", rec.Code, rec.Header().Get("X-Request-Id"))
	}

	// Empty list returns [] not null.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/files", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("empty list response: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Upload policy is available to any authenticated user and mirrors configured limits.
	policyReq := httptest.NewRequest(http.MethodGet, "/api/v1/files/config", nil)
	policyReq.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, policyReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload policy status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var policy struct {
		MaxFileSizeBytes   int64    `json:"maxFileSizeBytes"`
		MaxAvatarSizeBytes int64    `json:"maxAvatarSizeBytes"`
		AllowedExtensions  []string `json:"allowedExtensions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &policy); err != nil {
		t.Fatalf("decode upload policy: %v", err)
	}
	if policy.MaxFileSizeBytes != 1024 || policy.MaxAvatarSizeBytes != 2<<20 {
		t.Fatalf("unexpected upload policy limits: %+v", policy)
	}
	if !slices.Contains(policy.AllowedExtensions, ".png") || !slices.Contains(policy.AllowedExtensions, ".txt") {
		t.Fatalf("upload policy extensions missing built-ins: %+v", policy.AllowedExtensions)
	}
	if slices.Contains(policy.AllowedExtensions, ".svg") {
		t.Fatalf("upload policy must not advertise SVG: %+v", policy.AllowedExtensions)
	}

	// 400 FILES_TYPE_NOT_ALLOWED for SVG with zh-CN vs en-US Accept-Language translation difference.
	svgReqEN := makeMultipartFileRequest(t, "/api/v1/files", "file", "xss.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`))
	svgReqEN.Header.Set("Accept-Language", "en-US")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, svgReqEN)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("svg upload status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
	var errRespEN map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &errRespEN)
	if errRespEN["code"] != ErrCodeTypeNotAllowed {
		t.Fatalf("svg error code = %v, want %s", errRespEN["code"], ErrCodeTypeNotAllowed)
	}
	if _, hasReqID := errRespEN["request_id"]; hasReqID {
		t.Fatal("error body must not duplicate request_id")
	}

	svgReqZH := makeMultipartFileRequest(t, "/api/v1/files", "file", "xss.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`))
	svgReqZH.Header.Set("Accept-Language", "zh-CN")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, svgReqZH)
	var errRespZH map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &errRespZH)
	if errRespEN["message"] == "" || errRespZH["message"] == "" || errRespEN["message"] == errRespZH["message"] {
		t.Fatalf("expected distinct localized messages, en=%v zh=%v", errRespEN["message"], errRespZH["message"])
	}

	// 422 validation errors on GET /files: sorted by field, no raw {{min}}/{{max}} template placeholders.
	badQueryReq := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/files?page=0&pageSize=5&category=video&driver=gcs&sort=bad&order=sideways&q="+strings.Repeat("a", 101),
		nil,
	)
	badQueryReq.Header.Set("Authorization", "Bearer test-token")
	badQueryReq.Header.Set("Accept-Language", "en-US")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, badQueryReq)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid list query status = %d, want 422 (body=%s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "{{") {
		t.Fatalf("422 response contains unrendered template placeholder: %s", rec.Body.String())
	}
	var valBody struct {
		Code   string `json:"code"`
		Errors []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &valBody); err != nil {
		t.Fatalf("decode 422 response: %v", err)
	}
	if valBody.Code != apperr.CodeValidationFailed || len(valBody.Errors) != 7 {
		t.Fatalf("unexpected 422 body: %+v", valBody)
	}
	for i := 1; i < len(valBody.Errors); i++ {
		if valBody.Errors[i-1].Field >= valBody.Errors[i].Field {
			t.Fatalf("422 errors not sorted strictly ascending by field: %+v", valBody.Errors)
		}
	}

	// 422 on invalid UUID path param and 404 on missing file.
	badUUIDReq := httptest.NewRequest(http.MethodGet, "/api/v1/files/not-a-uuid", nil)
	badUUIDReq.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, badUUIDReq)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid uuid status = %d, want 422", rec.Code)
	}

	missingID := uuid.Must(uuid.NewV7()).String()
	notFoundReq := httptest.NewRequest(http.MethodGet, "/api/v1/files/"+missingID, nil)
	notFoundReq.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, notFoundReq)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing GET /files/:id status = %d, want 404", rec.Code)
	}

	// 401 FILES_INVALID_SIGNED_URL on invalid signed token.
	badSignedRec := httptest.NewRecorder()
	engine.ServeHTTP(badSignedRec, httptest.NewRequest(http.MethodGet, "/api/v1/files/signed/invalid.token", nil))
	if badSignedRec.Code != http.StatusUnauthorized || !strings.Contains(badSignedRec.Body.String(), ErrCodeInvalidSignedURL) {
		t.Fatalf("invalid signed url status=%d body=%s", badSignedRec.Code, badSignedRec.Body.String())
	}

	// 413 FILES_TOO_LARGE when exceeding configured limit (1024 bytes).
	bigReq := makeMultipartFileRequest(t, "/api/v1/files", "file", "large.txt", bytes.Repeat([]byte("a"), 2048))
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, bigReq)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize upload status = %d, want 413 (body=%s)", rec.Code, rec.Body.String())
	}

	// 201 upload PNG + signed inline preview + content download + avatar in-use 409 + 204 delete.
	pngData := makePNGBytes(t, 12, 12)
	upReq := makeMultipartFileRequest(t, "/api/v1/files", "file", "logo.png", pngData)
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, upReq)
	if rec.Code != http.StatusCreated {
		t.Fatalf("png upload status = %d, want 201 (body=%s)", rec.Code, rec.Body.String())
	}
	var created struct {
		ID      string `json:"id"`
		IsImage bool   `json:"isImage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created dto: %v", err)
	}
	if created.ID == "" || !created.IsImage {
		t.Fatalf("unexpected created dto: %+v", created)
	}

	// GET /api/v1/files/:id returns 200 metadata.
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/files/"+created.ID, nil)
	getReq.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, getReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /files/:id status = %d, want 200", rec.Code)
	}

	// Mint signed URL with chunked body (ContentLength = -1) honors disposition: attachment.
	chunkedReq := httptest.NewRequest(http.MethodPost, "/api/v1/files/"+created.ID+"/signed-url", io.NopCloser(strings.NewReader(`{"disposition":"attachment"}`)))
	chunkedReq.ContentLength = -1
	chunkedReq.Header.Set("Authorization", "Bearer test-token")
	chunkedReq.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, chunkedReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("chunked signed-url status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var chunkedSigned struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &chunkedSigned)
	chunkedPubRec := httptest.NewRecorder()
	engine.ServeHTTP(chunkedPubRec, httptest.NewRequest(http.MethodGet, chunkedSigned.URL, nil))
	if chunkedPubRec.Code != http.StatusOK || !strings.HasPrefix(chunkedPubRec.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("chunked signed attachment status=%d disp=%q", chunkedPubRec.Code, chunkedPubRec.Header().Get("Content-Disposition"))
	}

	// Malformed JSON on signed-url (even with ContentLength = -1) returns 400; invalid disposition returns 422.
	malformedReq := httptest.NewRequest(http.MethodPost, "/api/v1/files/"+created.ID+"/signed-url", io.NopCloser(strings.NewReader(`{invalid`)))
	malformedReq.ContentLength = -1
	malformedReq.Header.Set("Authorization", "Bearer test-token")
	malformedReq.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, malformedReq)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed signed-url status = %d, want 400", rec.Code)
	}

	badDispReq := httptest.NewRequest(http.MethodPost, "/api/v1/files/"+created.ID+"/signed-url", strings.NewReader(`{"disposition":"preview"}`))
	badDispReq.Header.Set("Authorization", "Bearer test-token")
	badDispReq.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, badDispReq)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid disposition status = %d, want 422", rec.Code)
	}

	// Mint signed URL with inline disposition and fetch publicly without Authorization header.
	signReq := httptest.NewRequest(http.MethodPost, "/api/v1/files/"+created.ID+"/signed-url", strings.NewReader(`{"disposition":"inline"}`))
	signReq.Header.Set("Authorization", "Bearer test-token")
	signReq.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, signReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("signed-url status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var signedResp struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &signedResp)

	pubRec := httptest.NewRecorder()
	engine.ServeHTTP(pubRec, httptest.NewRequest(http.MethodGet, signedResp.URL, nil))
	if pubRec.Code != http.StatusOK {
		t.Fatalf("signed content status = %d, body=%s", pubRec.Code, pubRec.Body.String())
	}
	if pubRec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing nosniff header")
	}
	if pubRec.Header().Get("Content-Security-Policy") != "default-src 'none'; sandbox" {
		t.Fatalf("Content-Security-Policy = %q, want sandbox CSP", pubRec.Header().Get("Content-Security-Policy"))
	}
	if pubRec.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("Cache-Control = %q, want private, no-store", pubRec.Header().Get("Cache-Control"))
	}
	if !strings.HasPrefix(pubRec.Header().Get("Content-Disposition"), "inline;") {
		t.Fatalf("image disposition = %q, want inline", pubRec.Header().Get("Content-Disposition"))
	}
	gotBody, _ := io.ReadAll(pubRec.Body)
	if !bytes.Equal(gotBody, pngData) {
		t.Fatal("signed content bytes mismatch")
	}

	// Upload PDF and verify forced attachment disposition even without ?download=1.
	pdfReq := makeMultipartFileRequest(t, "/api/v1/files", "file", "合同.pdf", []byte("%PDF-1.4\n%%EOF\n"))
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, pdfReq)
	if rec.Code != http.StatusCreated {
		t.Fatalf("pdf upload status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var createdPDF struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &createdPDF)

	dlReq := httptest.NewRequest(http.MethodGet, "/api/v1/files/"+createdPDF.ID+"/content", nil)
	dlReq.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, dlReq)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("pdf content status=%d disp=%q", rec.Code, rec.Header().Get("Content-Disposition"))
	}

	// 409 FILES_IN_USE when avatar references file.
	avatarRefs.inUse[uuid.MustParse(created.ID)] = true
	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/files/"+created.ID, nil)
	delReq.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, delReq)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete in-use status = %d, want 409", rec.Code)
	}

	// 204 No Content once unreferenced.
	delete(avatarRefs.inUse, uuid.MustParse(created.ID))
	delReq = httptest.NewRequest(http.MethodDelete, "/api/v1/files/"+created.ID, nil)
	delReq.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, delReq)
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("delete status=%d bodyLen=%d, want 204 empty", rec.Code, rec.Body.Len())
	}
}

func TestFilesPermissionsAndUploadRateLimit(t *testing.T) {
	// Read-only user gets 403 on upload and delete, but can upload own avatar.
	engine, _, _ := newTestFilesEngine(t, map[string]bool{
		"files:read":   true,
		"files:upload": false,
		"files:delete": false,
	}, 1<<20, fakeAvatarRefs{inUse: map[uuid.UUID]bool{}})

	upReq := makeMultipartFileRequest(t, "/api/v1/files", "file", "a.txt", []byte("hello"))
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, upReq)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("upload without files:upload status = %d, want 403", rec.Code)
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/files/"+uuid.Must(uuid.NewV7()).String(), nil)
	delReq.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, delReq)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("delete without files:delete status = %d, want 403", rec.Code)
	}

	// User without files:read gets 403 on list.
	noReadEngine, _, _ := newTestFilesEngine(t, map[string]bool{
		"files:read": false,
	}, 1<<20, fakeAvatarRefs{inUse: map[uuid.UUID]bool{}})
	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/files", nil)
	listReq.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	noReadEngine.ServeHTTP(rec, listReq)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("list without files:read status = %d, want 403", rec.Code)
	}

	// Avatar endpoint allows authenticated user without files:upload and enforces 20/min limit.
	pngBytes := makePNGBytes(t, 16, 16)
	for i := 0; i < uploadRateLimitPerMinute; i++ {
		req := makeMultipartFileRequest(t, "/api/v1/files/avatar", "file", "me.png", pngBytes)
		r := httptest.NewRecorder()
		engine.ServeHTTP(r, req)
		if r.Code != http.StatusCreated {
			t.Fatalf("avatar upload #%d status = %d, want 201", i+1, r.Code)
		}
	}

	overReq := makeMultipartFileRequest(t, "/api/v1/files/avatar", "file", "me.png", pngBytes)
	overRec := httptest.NewRecorder()
	engine.ServeHTTP(overRec, overReq)
	if overRec.Code != http.StatusTooManyRequests || overRec.Header().Get("Retry-After") == "" {
		t.Fatalf("21st upload status=%d retryAfter=%q, want 429", overRec.Code, overRec.Header().Get("Retry-After"))
	}
}
