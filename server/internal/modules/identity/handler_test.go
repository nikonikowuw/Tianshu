package identity

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"tianshu/internal/platform/httpserver"
	"tianshu/internal/platform/i18n"
	"tianshu/internal/platform/logger"
	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/authctx"
)

func TestIdentityHTTPRoutesContract(t *testing.T) {
	svc, _, roles, _, _, superID, mgrID := setupTestService()

	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle() error = %v", err)
	}
	if err := bundle.RegisterFS("identity", LocalesFS, "locales"); err != nil {
		t.Fatalf("RegisterFS(identity) error = %v", err)
	}

	engine, err := httpserver.NewEngine(httpserver.Options{
		Env:    "test",
		Logger: logger.New("test", "error", io.Discard),
		I18n:   bundle,
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	var currentActor uuid.UUID
	api := engine.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		if currentActor == uuid.Nil {
			_ = c.Error(apperr.Unauthorized(apperr.CodeUnauthorized))
			c.Abort()
			return
		}
		c.Request = c.Request.WithContext(authctx.WithIdentity(c.Request.Context(), authctx.Identity{
			UserID:    currentActor,
			SessionID: uuid.New(),
		}))
		c.Next()
	})

	handler := NewHandler(svc, bundle)
	allow := func(c *gin.Context) { c.Next() }
	handler.RegisterRoutes(api, RoutePermissions{
		RequireRead:   allow,
		RequireCreate: allow,
		RequireUpdate: allow,
		RequireDelete: allow,
	})

	doReq := func(method, path string, body any) *httptest.ResponseRecorder {
		var buf io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			buf = bytes.NewReader(raw)
		}
		req := httptest.NewRequest(method, path, buf)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept-Language", "en-US")
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec
	}

	currentActor = uuid.Nil
	if rec := doReq(http.MethodGet, "/api/v1/users", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET /users = %d, want 401", rec.Code)
	}

	currentActor = superID
	t.Run("GET /users returns paginated envelope and X-Request-Id", func(t *testing.T) {
		rec := doReq(http.MethodGet, "/api/v1/users?page=1&pageSize=10", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /users = %d, body = %s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("X-Request-Id") == "" {
			t.Fatalf("missing X-Request-Id header")
		}
		var body struct {
			Items []struct {
				ID    string `json:"id"`
				Roles []any  `json:"roles"`
			} `json:"items"`
			Total int64 `json:"total"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode /users: %v", err)
		}
		if body.Total < 2 || len(body.Items) < 2 {
			t.Fatalf("expected >= 2 users, got %+v", body)
		}
	})

	t.Run("GET /users rejects invalid status and roleId with 422", func(t *testing.T) {
		rec := doReq(http.MethodGet, "/api/v1/users?status=bad", nil)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid status = %d, want 422", rec.Code)
		}
		rec = doReq(http.MethodGet, "/api/v1/users?roleId=not-a-uuid", nil)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid roleId = %d, want 422", rec.Code)
		}
	})

	t.Run("POST /users creates 201 and empty roles serialize as []", func(t *testing.T) {
		rec := doReq(http.MethodPost, "/api/v1/users", map[string]any{
			"username": "alice",
			"email":    "alice@example.com",
			"password": "password123",
			"roleIds":  []string{},
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("POST /users = %d, body = %s", rec.Code, rec.Body.String())
		}
		if !bytes.Contains(rec.Body.Bytes(), []byte(`"roles":[]`)) {
			t.Fatalf("expected empty roles array [], got %s", rec.Body.String())
		}

		// Zero UUID in roleIds must be rejected with 422, never silently ignored.
		rec = doReq(http.MethodPost, "/api/v1/users", map[string]any{
			"username": "badrole",
			"email":    "badrole@example.com",
			"password": "password123",
			"roleIds":  []string{uuid.Nil.String()},
		})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("POST /users with nil roleId = %d, want 422", rec.Code)
		}
	})

	t.Run("PATCH /users/:id and DELETE /users/:id", func(t *testing.T) {
		rec := doReq(http.MethodPatch, "/api/v1/users/"+mgrID.String(), map[string]any{
			"displayName": "Updated Manager",
			"roleIds":     []string{roles.roleID("viewer").String()},
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("PATCH /users = %d, body = %s", rec.Code, rec.Body.String())
		}

		rec = doReq(http.MethodDelete, "/api/v1/users/"+mgrID.String(), nil)
		if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
			t.Fatalf("DELETE /users = %d (len %d), want 204 empty", rec.Code, rec.Body.Len())
		}

		rec = doReq(http.MethodGet, "/api/v1/users/"+mgrID.String(), nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET deleted user = %d, want 404", rec.Code)
		}
	})
}

func TestProfileHTTPRoutesContract(t *testing.T) {
	svc, _, _, sess, avatarFiles, _, superID, mgrID := setupTestServiceWithFiles()

	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle() error = %v", err)
	}
	if err := bundle.RegisterFS("identity", LocalesFS, "locales"); err != nil {
		t.Fatalf("RegisterFS(identity) error = %v", err)
	}

	engine, err := httpserver.NewEngine(httpserver.Options{
		Env:    "test",
		Logger: logger.New("test", "error", io.Discard),
		I18n:   bundle,
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	var (
		currentActor   uuid.UUID
		currentSession = uuid.Must(uuid.NewV7())
	)
	api := engine.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		if currentActor == uuid.Nil {
			_ = c.Error(apperr.Unauthorized(apperr.CodeUnauthorized))
			c.Abort()
			return
		}
		c.Request = c.Request.WithContext(authctx.WithIdentity(c.Request.Context(), authctx.Identity{
			UserID:    currentActor,
			SessionID: currentSession,
		}))
		c.Next()
	})

	handler := NewHandler(svc, bundle)
	denyAdmin := func(c *gin.Context) {
		_ = c.Error(apperr.Forbidden(apperr.CodeForbidden))
		c.Abort()
	}
	// Profile endpoints must remain accessible even when all users:* admin permissions are denied.
	handler.RegisterRoutes(api, RoutePermissions{
		RequireRead:   denyAdmin,
		RequireCreate: denyAdmin,
		RequireUpdate: denyAdmin,
		RequireDelete: denyAdmin,
	})

	doReq := func(method, path, lang string, body any) *httptest.ResponseRecorder {
		var buf io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			buf = bytes.NewReader(raw)
		}
		req := httptest.NewRequest(method, path, buf)
		req.Header.Set("Content-Type", "application/json")
		if lang != "" {
			req.Header.Set("Accept-Language", lang)
		}
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec
	}

	// Unauthenticated requests return 401.
	if rec := doReq(http.MethodGet, "/api/v1/profile", "en-US", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET /profile = %d, want 401", rec.Code)
	}

	currentActor = mgrID
	sess.activeByUser[mgrID] = []UserSession{
		{ID: currentSession, IPAddress: "127.0.0.1", UserAgent: "CurUA"},
	}
	sess.activeByUser[superID] = []UserSession{
		{ID: currentSession, IPAddress: "127.0.0.1", UserAgent: "SuperUA"},
	}
	avatarID := uuid.Must(uuid.NewV7())
	avatarFiles.valid[avatarID] = mgrID

	t.Run("GET and PATCH /profile update fields and avatar", func(t *testing.T) {
		rec := doReq(http.MethodGet, "/api/v1/profile", "en-US", nil)
		if rec.Code != http.StatusOK || rec.Header().Get("X-Request-Id") == "" {
			t.Fatalf("GET /profile = %d, reqID=%q, body=%s", rec.Code, rec.Header().Get("X-Request-Id"), rec.Body.String())
		}

		rec = doReq(http.MethodPatch, "/api/v1/profile", "en-US", map[string]any{
			"displayName":  "Alice Mgr",
			"email":        "alice.mgr@example.com",
			"avatarFileId": avatarID.String(),
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("PATCH /profile = %d, body=%s", rec.Code, rec.Body.String())
		}
		var body struct {
			DisplayName  string  `json:"displayName"`
			AvatarFileID *string `json:"avatarFileId"`
			AvatarURL    *string `json:"avatarUrl"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode profile: %v", err)
		}
		if body.DisplayName != "Alice Mgr" || body.AvatarFileID == nil || *body.AvatarFileID != avatarID.String() || body.AvatarURL == nil {
			t.Fatalf("unexpected profile response: %+v", body)
		}

		// Explicit null clears avatarFileId.
		rec = doReq(http.MethodPatch, "/api/v1/profile", "en-US", map[string]any{
			"avatarFileId": nil,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("PATCH /profile clear avatar = %d, body=%s", rec.Code, rec.Body.String())
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode cleared profile: %v", err)
		}
		if body.AvatarFileID != nil || body.AvatarURL != nil {
			t.Fatalf("expected cleared avatar, got %+v", body)
		}

		// Invalid avatar UUID returns 422.
		rec = doReq(http.MethodPatch, "/api/v1/profile", "en-US", map[string]any{
			"avatarFileId": "not-a-uuid",
		})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("PATCH /profile bad uuid = %d, want 422", rec.Code)
		}
	})

	t.Run("GET and DELETE /profile/sessions enforce current protection", func(t *testing.T) {
		other1 := uuid.Must(uuid.NewV7())
		other2 := uuid.Must(uuid.NewV7())
		sess.activeByUser[mgrID] = []UserSession{
			{ID: currentSession, IPAddress: "127.0.0.1", UserAgent: "CurUA"},
			{ID: other1, IPAddress: "10.0.0.1", UserAgent: "Other1UA"},
			{ID: other2, IPAddress: "10.0.0.2", UserAgent: "Other2UA"},
		}

		rec := doReq(http.MethodGet, "/api/v1/profile/sessions", "en-US", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /profile/sessions = %d, body=%s", rec.Code, rec.Body.String())
		}
		var list struct {
			Items []struct {
				ID        string `json:"id"`
				IsCurrent bool   `json:"isCurrent"`
			} `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
			t.Fatalf("decode sessions: %v", err)
		}
		if len(list.Items) != 3 || !list.Items[0].IsCurrent {
			t.Fatalf("unexpected sessions list: %+v", list)
		}

		rec = doReq(http.MethodDelete, "/api/v1/profile/sessions/"+currentSession.String(), "zh-CN", nil)
		if rec.Code != http.StatusConflict {
			t.Fatalf("DELETE current session (zh-CN) = %d, want 409", rec.Code)
		}
		var zhConflict map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &zhConflict); err != nil {
			t.Fatalf("decode zh-CN conflict: %v", err)
		}
		if _, hasReqID := zhConflict["request_id"]; hasReqID {
			t.Fatalf("error body must not include request_id: %v", zhConflict)
		}
		if _, hasReqID := zhConflict["requestId"]; hasReqID {
			t.Fatalf("error body must not include requestId: %v", zhConflict)
		}

		recEN := doReq(http.MethodDelete, "/api/v1/profile/sessions/"+currentSession.String(), "en-US", nil)
		if recEN.Code != http.StatusConflict || recEN.Header().Get("X-Request-Id") == "" {
			t.Fatalf("DELETE current session (en-US) = %d reqID=%q", recEN.Code, recEN.Header().Get("X-Request-Id"))
		}
		var enConflict map[string]any
		if err := json.Unmarshal(recEN.Body.Bytes(), &enConflict); err != nil {
			t.Fatalf("decode en-US conflict: %v", err)
		}
		if zhConflict["code"] != ErrCodeProfileCurrentSessionRevoke || enConflict["code"] != ErrCodeProfileCurrentSessionRevoke {
			t.Fatalf("conflict codes = (%v, %v)", zhConflict["code"], enConflict["code"])
		}
		if zhConflict["message"] == "" || enConflict["message"] == "" || zhConflict["message"] == enConflict["message"] {
			t.Fatalf("expected distinct bilingual messages, got zh=%q en=%q", zhConflict["message"], enConflict["message"])
		}

		rec = doReq(http.MethodDelete, "/api/v1/profile/sessions/"+other1.String(), "en-US", nil)
		if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
			t.Fatalf("DELETE other session = %d (len %d), want 204", rec.Code, rec.Body.Len())
		}

		rec = doReq(http.MethodDelete, "/api/v1/profile/sessions", "en-US", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("DELETE other sessions = %d, body=%s", rec.Code, rec.Body.String())
		}
		var bulk struct {
			RevokedSessions int64 `json:"revokedSessions"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &bulk); err != nil || bulk.RevokedSessions != 1 {
			t.Fatalf("unexpected bulk revoke response: %+v err=%v", bulk, err)
		}
	})

	t.Run("PUT /profile/password returns 400 on bad current password, 200 on success, and 429 on rate limit", func(t *testing.T) {
		currentActor = superID
		rec := doReq(http.MethodPut, "/api/v1/profile/password", "zh-CN", map[string]any{
			"currentPassword": "wrong-password",
			"newPassword":     "valid-new-password-1",
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("bad current password = %d, want 400, body=%s", rec.Code, rec.Body.String())
		}
		var errBody struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil || errBody.Code != ErrCodeProfileInvalidCurrentPassword || errBody.Message == "" {
			t.Fatalf("unexpected error body: %+v", errBody)
		}

		rec = doReq(http.MethodPut, "/api/v1/profile/password", "en-US", map[string]any{
			"currentPassword": "password123",
			"newPassword":     "valid-new-password-1",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("valid password change = %d, body=%s", rec.Code, rec.Body.String())
		}

		// Exhaust remaining 3 attempts (total limit is 5/min/user) then assert 429 + Retry-After.
		for i := range 3 {
			r := doReq(http.MethodPut, "/api/v1/profile/password", "en-US", map[string]any{
				"currentPassword": "wrong-password",
				"newPassword":     "valid-new-password-2",
			})
			if r.Code != http.StatusBadRequest {
				t.Fatalf("attempt %d = %d, want 400", i+3, r.Code)
			}
		}
		limited := doReq(http.MethodPut, "/api/v1/profile/password", "en-US", map[string]any{
			"currentPassword": "wrong-password",
			"newPassword":     "valid-new-password-2",
		})
		if limited.Code != http.StatusTooManyRequests || limited.Header().Get("Retry-After") == "" {
			t.Fatalf("rate-limited password change = %d Retry-After=%q, want 429 with Retry-After", limited.Code, limited.Header().Get("Retry-After"))
		}
	})
}
