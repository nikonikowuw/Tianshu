package files

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
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
	extraDeps ...func(*ServiceDeps),
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
	sessionID := uuid.Must(uuid.NewV7())
	deps := ServiceDeps{
		Actors:    fakeActors{names: map[uuid.UUID]string{actorID: "tester"}},
		AvatarRef: avatarRefs,
	}
	for _, fn := range extraDeps {
		fn(&deps)
	}
	svc := newServiceWithStore(newFakeFileStore(), reg, ServiceConfig{
		MaxFileSizeBytes:   maxFileBytes,
		MaxAvatarSizeBytes: 2 << 20,
		SignedURLSecret:    "contract-test-secret-at-least-32-bytes",
		SignedURLTTL:       5 * time.Minute,
	}, deps)
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
			UserID:    actorID,
			SessionID: sessionID,
			Username:  "tester",
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

func decodeJSON(t *testing.T, data []byte, dst any) {
	t.Helper()
	if err := json.Unmarshal(data, dst); err != nil {
		t.Fatalf("json.Unmarshal(%s): %v", string(data), err)
	}
}

func TestFilesHTTPContracts(t *testing.T) {
	avatarRefs := fakeAvatarRefs{inUse: make(map[uuid.UUID]bool)}
	engine, _, _ := newTestFilesEngine(t, map[string]bool{
		"files:read":   true,
		"files:upload": true,
		"files:delete": true,
	}, 1024, avatarRefs)

	// 401 when unauthenticated across protected endpoints.
	for _, tc := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/v1/files"},
		{method: http.MethodGet, path: "/api/v1/files/config"},
		{method: http.MethodPost, path: "/api/v1/files/avatar"},
	} {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != http.StatusUnauthorized || rec.Header().Get("X-Request-Id") == "" {
			t.Fatalf("unauthenticated %s %s: status=%d reqID=%q", tc.method, tc.path, rec.Code, rec.Header().Get("X-Request-Id"))
		}
	}
	var rec *httptest.ResponseRecorder

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
	decodeJSON(t, rec.Body.Bytes(), &policy)
	if policy.MaxFileSizeBytes != 1024 || policy.MaxAvatarSizeBytes != 2<<20 {
		t.Fatalf("unexpected upload policy limits: %+v", policy)
	}
	if !slices.Contains(policy.AllowedExtensions, ".png") || !slices.Contains(policy.AllowedExtensions, ".txt") {
		t.Fatalf("upload policy extensions missing built-ins: %+v", policy.AllowedExtensions)
	}
	if slices.Contains(policy.AllowedExtensions, ".svg") {
		t.Fatalf("upload policy must not advertise SVG: %+v", policy.AllowedExtensions)
	}

	// 400 BAD_REQUEST on non-multipart upload, 400 FILES_MISSING_FILE on missing file field, 400 FILES_EMPTY_FILE on empty file.
	nonMultipart := httptest.NewRequest(http.MethodPost, "/api/v1/files", strings.NewReader("plain"))
	nonMultipart.Header.Set("Authorization", "Bearer test-token")
	nonMultipart.Header.Set("Content-Type", "text/plain")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, nonMultipart)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), apperr.CodeBadRequest) {
		t.Fatalf("non-multipart upload: status=%d body=%s", rec.Code, rec.Body.String())
	}

	missingField := makeMultipartFileRequest(t, "/api/v1/files", "wrongfield", "a.txt", []byte("hi"))
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, missingField)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), ErrCodeMissingFile) {
		t.Fatalf("missing file field: status=%d body=%s", rec.Code, rec.Body.String())
	}

	emptyFile := makeMultipartFileRequest(t, "/api/v1/files", "file", "empty.txt", nil)
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, emptyFile)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), ErrCodeEmptyFile) {
		t.Fatalf("empty file upload: status=%d body=%s", rec.Code, rec.Body.String())
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
	decodeJSON(t, rec.Body.Bytes(), &errRespEN)
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
	decodeJSON(t, rec.Body.Bytes(), &errRespZH)
	if errRespEN["message"] == "" || errRespZH["message"] == "" || errRespEN["message"] == errRespZH["message"] {
		t.Fatalf("expected distinct localized messages, en=%v zh=%v", errRespEN["message"], errRespZH["message"])
	}

	// 422 validation errors on GET /files: sorted by field, no raw {{min}}/{{max}} template placeholders.
	manyMIMEs := make([]string, maxMultiFilterLen+1)
	for i := range manyMIMEs {
		manyMIMEs[i] = "application/x-t-" + strings.Repeat("z", i+1)
	}
	type valErrBody struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Errors  []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	badQueryURL := "/api/v1/files?page=0&pageSize=5&category=video&driver=gcs&sort=bad&order=sideways&q=" + strings.Repeat("a", 101) + "&mimeType=" + strings.Join(manyMIMEs, ",")
	var valEN, valZH valErrBody
	for _, item := range []struct {
		lang string
		out  *valErrBody
	}{
		{lang: "en-US", out: &valEN},
		{lang: "zh-CN", out: &valZH},
	} {
		badQueryReq := httptest.NewRequest(http.MethodGet, badQueryURL, nil)
		badQueryReq.Header.Set("Authorization", "Bearer test-token")
		badQueryReq.Header.Set("Accept-Language", item.lang)
		rec = httptest.NewRecorder()
		engine.ServeHTTP(rec, badQueryReq)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid list query (%s) status = %d, want 422 (body=%s)", item.lang, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "{{") {
			t.Fatalf("422 response (%s) contains unrendered template placeholder: %s", item.lang, rec.Body.String())
		}
		decodeJSON(t, rec.Body.Bytes(), item.out)
		if item.out.Code != apperr.CodeValidationFailed || len(item.out.Errors) != 8 {
			t.Fatalf("unexpected 422 body (%s): %+v", item.lang, item.out)
		}
		for i := 0; i < len(item.out.Errors); i++ {
			if strings.TrimSpace(item.out.Errors[i].Message) == "" {
				t.Fatalf("empty 422 field message (%s) at %d: %+v", item.lang, i, item.out.Errors[i])
			}
			if i > 0 && item.out.Errors[i-1].Field >= item.out.Errors[i].Field {
				t.Fatalf("422 errors not sorted strictly ascending by field (%s): %+v", item.lang, item.out.Errors)
			}
		}
	}
	for i := range valEN.Errors {
		if valEN.Errors[i].Field != valZH.Errors[i].Field {
			t.Fatalf("422 field mismatch between locales: %s vs %s", valEN.Errors[i].Field, valZH.Errors[i].Field)
		}
		if valEN.Errors[i].Message == valZH.Errors[i].Message {
			t.Fatalf("expected localized 422 field message for %q, got identical %q", valEN.Errors[i].Field, valEN.Errors[i].Message)
		}
	}

	// NUL byte and invalid UTF-8 in q / mimeType return 422 VALIDATION_FAILED (never 500).
	for _, rawQuery := range []string{
		"q=test%00",
		"q=%ff",
		"mimeType=image/png%00",
		"mimeType=" + strings.Repeat("a", maxMIMETypeFilterLen+1),
	} {
		qReq := httptest.NewRequest(http.MethodGet, "/api/v1/files?"+rawQuery, nil)
		qReq.Header.Set("Authorization", "Bearer test-token")
		qRec := httptest.NewRecorder()
		engine.ServeHTTP(qRec, qReq)
		if qRec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("GET /files?%s status = %d, want 422 (body=%s)", rawQuery, qRec.Code, qRec.Body.String())
		}
	}

	// 422 on invalid UUID path param and 404 on missing file (GET & DELETE).
	badUUIDReq := httptest.NewRequest(http.MethodGet, "/api/v1/files/not-a-uuid", nil)
	badUUIDReq.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, badUUIDReq)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid uuid status = %d, want 422", rec.Code)
	}

	badDelUUID := httptest.NewRequest(http.MethodDelete, "/api/v1/files/not-a-uuid", nil)
	badDelUUID.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, badDelUUID)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("DELETE invalid uuid status = %d, want 422", rec.Code)
	}

	missingID := uuid.Must(uuid.NewV7()).String()
	notFoundReq := httptest.NewRequest(http.MethodGet, "/api/v1/files/"+missingID, nil)
	notFoundReq.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, notFoundReq)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing GET /files/:id status = %d, want 404", rec.Code)
	}

	notFoundDel := httptest.NewRequest(http.MethodDelete, "/api/v1/files/"+missingID, nil)
	notFoundDel.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, notFoundDel)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing DELETE /files/:id status = %d, want 404", rec.Code)
	}

	// 401 FILES_INVALID_SIGNED_URL on invalid signed token.
	badSignedRec := httptest.NewRecorder()
	engine.ServeHTTP(badSignedRec, httptest.NewRequest(http.MethodGet, "/api/v1/files/signed/invalid.token", nil))
	if badSignedRec.Code != http.StatusUnauthorized || !strings.Contains(badSignedRec.Body.String(), ErrCodeInvalidSignedURL) {
		t.Fatalf("invalid signed url status=%d body=%s", badSignedRec.Code, badSignedRec.Body.String())
	}

	// 413 FILES_TOO_LARGE on both fh.Size check (2048B) and http.MaxBytesReader cap (>1 MiB + maxBytes).
	bigReq := makeMultipartFileRequest(t, "/api/v1/files", "file", "large.txt", bytes.Repeat([]byte("a"), 2048))
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, bigReq)
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), ErrCodeTooLarge) {
		t.Fatalf("oversize upload status = %d, want 413 (body=%s)", rec.Code, rec.Body.String())
	}

	hugeReq := makeMultipartFileRequest(t, "/api/v1/files", "file", "huge.txt", bytes.Repeat([]byte("b"), int(multipartFormOverheadBytes)+4096))
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, hugeReq)
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), ErrCodeTooLarge) {
		t.Fatalf("MaxBytesReader oversize upload status = %d, want 413 (body=%s)", rec.Code, rec.Body.String())
	}

	// 201 upload PNG + non-empty list DTO + content inline/download + signed URL + avatar 409 + 204 delete + post-delete signed 404.
	pngData := makePNGBytes(t, 12, 12)
	upReq := makeMultipartFileRequest(t, "/api/v1/files", "file", "logo.png", pngData)
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, upReq)
	if rec.Code != http.StatusCreated {
		t.Fatalf("png upload status = %d, want 201 (body=%s)", rec.Code, rec.Body.String())
	}
	var created fileDTO
	decodeJSON(t, rec.Body.Bytes(), &created)
	if created.ID == "" || !created.IsImage || created.Category != CategoryImage || created.Uploader == nil || created.Uploader.Username != "tester" {
		t.Fatalf("unexpected created dto: %+v", created)
	}

	// Non-empty GET /api/v1/files returns populated DTO fields.
	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/files", nil)
	listReq.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, listReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /files status = %d, want 200", rec.Code)
	}
	var listResp struct {
		Items    []fileDTO `json:"items"`
		Total    int64     `json:"total"`
		Page     int       `json:"page"`
		PageSize int       `json:"pageSize"`
	}
	decodeJSON(t, rec.Body.Bytes(), &listResp)
	if listResp.Total != 1 || len(listResp.Items) != 1 || listResp.Items[0].ID != created.ID || listResp.Items[0].OriginalName != "logo.png" {
		t.Fatalf("unexpected non-empty list response: %+v", listResp)
	}

	// GET /api/v1/files/:id/content defaults inline for images, switches to attachment for ?download=1 and ?download=true.
	imgInlineReq := httptest.NewRequest(http.MethodGet, "/api/v1/files/"+created.ID+"/content", nil)
	imgInlineReq.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, imgInlineReq)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "inline;") {
		t.Fatalf("image content default status=%d disp=%q", rec.Code, rec.Header().Get("Content-Disposition"))
	}
	for _, q := range []string{"?download=1", "?download=true"} {
		imgDLReq := httptest.NewRequest(http.MethodGet, "/api/v1/files/"+created.ID+"/content"+q, nil)
		imgDLReq.Header.Set("Authorization", "Bearer test-token")
		rec = httptest.NewRecorder()
		engine.ServeHTTP(rec, imgDLReq)
		if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment;") {
			t.Fatalf("image content %s status=%d disp=%q", q, rec.Code, rec.Header().Get("Content-Disposition"))
		}
	}

	// GET /api/v1/files/:id returns 200 metadata.
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/files/"+created.ID, nil)
	getReq.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, getReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /files/:id status = %d, want 200", rec.Code)
	}

	// Empty body on POST /files/:id/signed-url returns 200; >64 KiB body returns 413.
	emptySignReq := httptest.NewRequest(http.MethodPost, "/api/v1/files/"+created.ID+"/signed-url", nil)
	emptySignReq.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, emptySignReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty body signed-url status = %d, want 200", rec.Code)
	}

	oversizedSignReq := httptest.NewRequest(http.MethodPost, "/api/v1/files/"+created.ID+"/signed-url", strings.NewReader(`{"disposition":"`+strings.Repeat("a", int(maxSignedURLBodyBytes)+128)+`"}`))
	oversizedSignReq.Header.Set("Authorization", "Bearer test-token")
	oversizedSignReq.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, oversizedSignReq)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized signed-url status = %d, want 413", rec.Code)
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
	decodeJSON(t, rec.Body.Bytes(), &chunkedSigned)
	chunkedPubRec := httptest.NewRecorder()
	engine.ServeHTTP(chunkedPubRec, httptest.NewRequest(http.MethodGet, chunkedSigned.URL, nil))
	if chunkedPubRec.Code != http.StatusOK || !strings.HasPrefix(chunkedPubRec.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("chunked signed attachment status=%d disp=%q", chunkedPubRec.Code, chunkedPubRec.Header().Get("Content-Disposition"))
	}

	// Malformed JSON on signed-url returns 400; invalid disposition returns 422.
	malformedReq := httptest.NewRequest(http.MethodPost, "/api/v1/files/"+created.ID+"/signed-url", io.NopCloser(strings.NewReader(`{invalid`)))
	malformedReq.ContentLength = -1
	malformedReq.Header.Set("Authorization", "Bearer test-token")
	malformedReq.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, malformedReq)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed signed-url status = %d, want 400", rec.Code)
	}

	var dispEN, dispZH valErrBody
	for _, item := range []struct {
		lang string
		out  *valErrBody
	}{
		{lang: "en-US", out: &dispEN},
		{lang: "zh-CN", out: &dispZH},
	} {
		badDispReq := httptest.NewRequest(http.MethodPost, "/api/v1/files/"+created.ID+"/signed-url", strings.NewReader(`{"disposition":"preview"}`))
		badDispReq.Header.Set("Authorization", "Bearer test-token")
		badDispReq.Header.Set("Content-Type", "application/json")
		badDispReq.Header.Set("Accept-Language", item.lang)
		rec = httptest.NewRecorder()
		engine.ServeHTTP(rec, badDispReq)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid disposition (%s) status = %d, want 422", item.lang, rec.Code)
		}
		decodeJSON(t, rec.Body.Bytes(), item.out)
		if item.out.Code != apperr.CodeValidationFailed || len(item.out.Errors) != 1 || item.out.Errors[0].Field != "disposition" || strings.TrimSpace(item.out.Errors[0].Message) == "" {
			t.Fatalf("unexpected disposition 422 body (%s): %+v", item.lang, item.out)
		}
	}
	if dispEN.Errors[0].Message == dispZH.Errors[0].Message {
		t.Fatalf("expected localized disposition 422 message, got identical %q", dispEN.Errors[0].Message)
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
	decodeJSON(t, rec.Body.Bytes(), &signedResp)

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
	gotBody, err := io.ReadAll(pubRec.Body)
	if err != nil {
		t.Fatalf("read signed response body: %v", err)
	}
	if !bytes.Equal(gotBody, pngData) {
		t.Fatal("signed content bytes mismatch")
	}

	// Upload PDF with Chinese & RFC 8187 reserved characters (=, :, @) and verify Content-Disposition parses cleanly.
	specialPDFName := "合同=2026@10:30.pdf"
	pdfReq := makeMultipartFileRequest(t, "/api/v1/files", "file", specialPDFName, []byte("%PDF-1.4\n%%EOF\n"))
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, pdfReq)
	if rec.Code != http.StatusCreated {
		t.Fatalf("pdf upload status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var createdPDF struct {
		ID string `json:"id"`
	}
	decodeJSON(t, rec.Body.Bytes(), &createdPDF)

	dlReq := httptest.NewRequest(http.MethodGet, "/api/v1/files/"+createdPDF.ID+"/content", nil)
	dlReq.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, dlReq)
	cd := rec.Header().Get("Content-Disposition")
	if rec.Code != http.StatusOK || !strings.HasPrefix(cd, "attachment;") {
		t.Fatalf("pdf content status=%d disp=%q", rec.Code, cd)
	}
	mediaType, params, err := mime.ParseMediaType(cd)
	if err != nil {
		t.Fatalf("mime.ParseMediaType(%q) failed: %v", cd, err)
	}
	if mediaType != "attachment" || params["filename"] != specialPDFName {
		t.Fatalf("parsed Content-Disposition = (%q, %+v), want attachment / %q", mediaType, params, specialPDFName)
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

	// 204 No Content once unreferenced, and previously issued signed URL immediately returns 404.
	delete(avatarRefs.inUse, uuid.MustParse(created.ID))
	delReq = httptest.NewRequest(http.MethodDelete, "/api/v1/files/"+created.ID, nil)
	delReq.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, delReq)
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("delete status=%d bodyLen=%d, want 204 empty", rec.Code, rec.Body.Len())
	}

	deletedSignedRec := httptest.NewRecorder()
	engine.ServeHTTP(deletedSignedRec, httptest.NewRequest(http.MethodGet, signedResp.URL, nil))
	if deletedSignedRec.Code != http.StatusNotFound || !strings.Contains(deletedSignedRec.Body.String(), ErrCodeNotFound) {
		t.Fatalf("signed URL after delete status=%d body=%s, want 404", deletedSignedRec.Code, deletedSignedRec.Body.String())
	}
}

func TestFormatContentDispositionRFC8187(t *testing.T) {
	for _, name := range []string{
		"report=2026.pdf",
		"photo@2x.png",
		"log-10:30.txt",
		"合同;v1.pdf",
		`weird "name" file.txt`,
		"中文 文件名.png",
	} {
		header := formatContentDisposition(DispositionAttachment, name)
		mediaType, params, err := mime.ParseMediaType(header)
		if err != nil {
			t.Fatalf("mime.ParseMediaType(%q) for %q failed: %v", header, name, err)
		}
		if mediaType != DispositionAttachment || params["filename"] != name {
			t.Fatalf("round-trip for %q: got (%q, %+v)", name, mediaType, params)
		}
	}
}

func TestFilesPermissionsAvatarErrorsAndUploadRateLimit(t *testing.T) {
	caller := &fakeAvatarCaller{}
	// Read-only user gets 403 on upload and delete, but can upload own avatar.
	engine, _, _ := newTestFilesEngine(t, map[string]bool{
		"files:read":   true,
		"files:upload": false,
		"files:delete": false,
	}, 1<<20, fakeAvatarRefs{inUse: map[uuid.UUID]bool{}}, func(d *ServiceDeps) {
		d.AvatarCaller = caller
	})

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

	// User without files:read gets 403 across all files:read-protected endpoints.
	noReadEngine, _, _ := newTestFilesEngine(t, map[string]bool{
		"files:read": false,
	}, 1<<20, fakeAvatarRefs{inUse: map[uuid.UUID]bool{}})
	noReadID := uuid.Must(uuid.NewV7()).String()
	for _, tc := range []struct {
		method string
		target string
	}{
		{method: http.MethodGet, target: "/api/v1/files"},
		{method: http.MethodGet, target: "/api/v1/files/" + noReadID},
		{method: http.MethodGet, target: "/api/v1/files/" + noReadID + "/content"},
		{method: http.MethodPost, target: "/api/v1/files/" + noReadID + "/signed-url"},
	} {
		r := httptest.NewRequest(tc.method, tc.target, nil)
		r.Header.Set("Authorization", "Bearer test-token")
		w := httptest.NewRecorder()
		noReadEngine.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s %s without files:read status = %d, want 403", tc.method, tc.target, w.Code)
		}
	}

	// Avatar endpoint rejects corrupt images (400 FILES_INVALID_IMAGE), non-images (400 FILES_TYPE_NOT_ALLOWED),
	// and revoked/inactive sessions (401).
	avatarErrEngine, _, _ := newTestFilesEngine(t, map[string]bool{}, 1<<20, fakeAvatarRefs{inUse: map[uuid.UUID]bool{}}, func(d *ServiceDeps) {
		d.AvatarCaller = caller
	})
	corruptReq := makeMultipartFileRequest(t, "/api/v1/files/avatar", "file", "bad.png", makePNGBytes(t, 16, 16)[:20])
	rec = httptest.NewRecorder()
	avatarErrEngine.ServeHTTP(rec, corruptReq)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), ErrCodeInvalidImage) {
		t.Fatalf("corrupt avatar status=%d body=%s", rec.Code, rec.Body.String())
	}

	pdfAvatarReq := makeMultipartFileRequest(t, "/api/v1/files/avatar", "file", "doc.pdf", []byte("%PDF-1.4\n%%EOF\n"))
	rec = httptest.NewRecorder()
	avatarErrEngine.ServeHTTP(rec, pdfAvatarReq)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), ErrCodeTypeNotAllowed) {
		t.Fatalf("pdf avatar status=%d body=%s", rec.Code, rec.Body.String())
	}

	caller.err = apperr.Unauthorized(apperr.CodeUnauthorized)
	revokedReq := makeMultipartFileRequest(t, "/api/v1/files/avatar", "file", "me.png", makePNGBytes(t, 16, 16))
	rec = httptest.NewRecorder()
	avatarErrEngine.ServeHTTP(rec, revokedReq)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session avatar upload status=%d, want 401", rec.Code)
	}
	caller.err = nil

	// Avatar endpoint allows authenticated active user without files:upload and enforces 20/min limit.
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
