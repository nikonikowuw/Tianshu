package identity

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"tianshu/internal/platform/httpserver"
	"tianshu/internal/platform/i18n"
	"tianshu/internal/platform/logger"
	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/authctx"
)

type httpTestHarness struct {
	t              *testing.T
	fx             *testFixture
	engine         *gin.Engine
	currentActor   uuid.UUID
	currentSession uuid.UUID
	skipAuth       bool
}

func newHTTPTestHarness(t *testing.T, perms RoutePermissions) *httpTestHarness {
	t.Helper()

	fx := newTestFixture(t)
	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle() error = %v", err)
	}
	if err := bundle.RegisterFS("identity", LocalesFS, "locales"); err != nil {
		t.Fatalf("RegisterFS(identity) error = %v", err)
	}
	if err := bundle.Validate(); err != nil {
		t.Fatalf("bundle.Validate() error = %v", err)
	}

	engine, err := httpserver.NewEngine(httpserver.Options{
		Env:    "test",
		Logger: logger.New("test", "error", io.Discard),
		I18n:   bundle,
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	h := &httpTestHarness{
		t:              t,
		fx:             fx,
		engine:         engine,
		currentActor:   fx.superID,
		currentSession: fx.superSessID,
	}

	api := engine.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		if h.skipAuth {
			c.Next()
			return
		}
		if h.currentActor == uuid.Nil {
			_ = c.Error(apperr.Unauthorized(apperr.CodeUnauthorized))
			c.Abort()
			return
		}
		c.Request = c.Request.WithContext(authctx.WithIdentity(c.Request.Context(), authctx.Identity{
			UserID:    h.currentActor,
			SessionID: h.currentSession,
		}))
		c.Next()
	})

	handler := NewHandler(fx.svc, bundle)
	handler.RegisterRoutes(api, perms)
	return h
}

func allowAllPerms() RoutePermissions {
	allow := func(c *gin.Context) { c.Next() }
	return RoutePermissions{
		RequireRead:   allow,
		RequireCreate: allow,
		RequireUpdate: allow,
		RequireDelete: allow,
	}
}

func denyAllPerms() RoutePermissions {
	deny := func(c *gin.Context) {
		_ = c.Error(apperr.Forbidden(apperr.CodeForbidden))
		c.Abort()
	}
	return RoutePermissions{
		RequireRead:   deny,
		RequireCreate: deny,
		RequireUpdate: deny,
		RequireDelete: deny,
	}
}

func (h *httpTestHarness) doReq(method, path, lang string, body any) *httptest.ResponseRecorder {
	h.t.Helper()
	var buf io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			h.t.Fatalf("marshal request body: %v", err)
		}
		buf = bytes.NewReader(raw)
	}
	return h.doRawReq(method, path, lang, buf)
}

func (h *httpTestHarness) doRawReq(method, path, lang string, body io.Reader) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Content-Type", "application/json")
	if lang != "" {
		req.Header.Set("Accept-Language", lang)
	}
	rec := httptest.NewRecorder()
	h.engine.ServeHTTP(rec, req)
	return rec
}

func TestIdentityHTTPRoutesContract(t *testing.T) {
	t.Run("unauthenticated and permission matrix enforce 401 and 403", func(t *testing.T) {
		h := newHTTPTestHarness(t, allowAllPerms())
		h.currentActor = uuid.Nil
		if rec := h.doReq(http.MethodGet, "/api/v1/users", "en-US", nil); rec.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated GET /users = %d, want 401", rec.Code)
		}

		// Even if upstream auth middleware is bypassed, mutating handlers defensively return 401.
		h.skipAuth = true
		for _, tc := range []struct {
			method string
			path   string
		}{
			{http.MethodPost, "/api/v1/users"},
			{http.MethodPatch, "/api/v1/users/" + h.fx.mgrID.String()},
			{http.MethodDelete, "/api/v1/users/" + h.fx.mgrID.String()},
		} {
			if rec := h.doReq(tc.method, tc.path, "en-US", map[string]any{}); rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s %s without context identity = %d, want 401", tc.method, tc.path, rec.Code)
			}
		}

		// Permission denial matrix across all 5 /users* routes.
		denied := newHTTPTestHarness(t, denyAllPerms())
		for _, tc := range []struct {
			method string
			path   string
		}{
			{http.MethodGet, "/api/v1/users"},
			{http.MethodGet, "/api/v1/users/" + denied.fx.mgrID.String()},
			{http.MethodPost, "/api/v1/users"},
			{http.MethodPatch, "/api/v1/users/" + denied.fx.mgrID.String()},
			{http.MethodDelete, "/api/v1/users/" + denied.fx.mgrID.String()},
		} {
			rec := denied.doReq(tc.method, tc.path, "en-US", nil)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("denied %s %s = %d, want 403", tc.method, tc.path, rec.Code)
			}
		}
	})

	t.Run("GET /users returns paginated response, empty array [], and never exposes passwordHash", func(t *testing.T) {
		h := newHTTPTestHarness(t, allowAllPerms())
		rec := h.doReq(http.MethodGet, "/api/v1/users?page=&pageSize=", "en-US", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /users = %d, body = %s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("X-Request-Id") == "" {
			t.Fatalf("missing X-Request-Id header")
		}
		if bytes.Contains(rec.Body.Bytes(), []byte("passwordHash")) || bytes.Contains(rec.Body.Bytes(), []byte("hash:")) {
			t.Fatalf("response must never expose password hash: %s", rec.Body.String())
		}
		var body struct {
			Items []struct {
				ID    string `json:"id"`
				Roles []any  `json:"roles"`
			} `json:"items"`
			Total    int64 `json:"total"`
			Page     int   `json:"page"`
			PageSize int   `json:"pageSize"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode /users: %v", err)
		}
		if body.Total < 2 || len(body.Items) < 2 || body.Page != 1 || body.PageSize != 20 {
			t.Fatalf("unexpected paginated payload: %+v", body)
		}

		// Empty result must serialize items as [] with page & pageSize.
		emptyRec := h.doReq(http.MethodGet, "/api/v1/users?q=no-such-user-xyz", "en-US", nil)
		if emptyRec.Code != http.StatusOK || !bytes.Contains(emptyRec.Body.Bytes(), []byte(`"items":[]`)) {
			t.Fatalf("empty GET /users = %d, body = %s", emptyRec.Code, emptyRec.Body.String())
		}
	})

	t.Run("GET /users strictly validates page, pageSize, q, status, roleId, sort, order with bilingual 422", func(t *testing.T) {
		h := newHTTPTestHarness(t, allowAllPerms())

		tooManyRoles := make([]string, 0, 21)
		for range 21 {
			tooManyRoles = append(tooManyRoles, "roleId="+uuid.Must(uuid.NewV7()).String())
		}

		cases := []struct {
			name      string
			query     string
			wantField string
		}{
			{"non-integer page", "page=abc", "page"},
			{"zero page", "page=0", "page"},
			{"negative page", "page=-1", "page"},
			{"non-integer pageSize", "pageSize=abc", "pageSize"},
			{"zero pageSize", "pageSize=0", "pageSize"},
			{"below min pageSize", "pageSize=9", "pageSize"},
			{"above max pageSize", "pageSize=101", "pageSize"},
			{"invalid status", "status=bad", "status"},
			{"invalid roleId uuid", "roleId=not-a-uuid", "roleId"},
			{"nil roleId uuid", "roleId=" + uuid.Nil.String(), "roleId"},
			{"too many roleIds", strings.Join(tooManyRoles, "&"), "roleId"},
			{"NUL byte in q", "q=abc%00def", "q"},
			{"too long q", "q=" + strings.Repeat("a", 101), "q"},
			{"invalid sort", "sort=password_hash", "sort"},
			{"invalid order", "order=sideways", "order"},
		}
		for _, tc := range cases {
			recEN := h.doReq(http.MethodGet, "/api/v1/users?"+tc.query, "en-US", nil)
			if recEN.Code != http.StatusUnprocessableEntity {
				t.Fatalf("%s (en-US): status = %d, want 422, body = %s", tc.name, recEN.Code, recEN.Body.String())
			}
			recZH := h.doReq(http.MethodGet, "/api/v1/users?"+tc.query, "zh-CN", nil)
			if recZH.Code != http.StatusUnprocessableEntity {
				t.Fatalf("%s (zh-CN): status = %d, want 422, body = %s", tc.name, recZH.Code, recZH.Body.String())
			}
			var errEN, errZH struct {
				Code    string              `json:"code"`
				Message string              `json:"message"`
				Errors  []apperr.FieldError `json:"errors"`
			}
			if err := json.Unmarshal(recEN.Body.Bytes(), &errEN); err != nil {
				t.Fatalf("%s decode en-US: %v", tc.name, err)
			}
			if err := json.Unmarshal(recZH.Body.Bytes(), &errZH); err != nil {
				t.Fatalf("%s decode zh-CN: %v", tc.name, err)
			}
			if errEN.Code != apperr.CodeValidationFailed || len(errEN.Errors) == 0 || errEN.Errors[0].Field != tc.wantField {
				t.Fatalf("%s unexpected en-US error: %+v", tc.name, errEN)
			}
			if errEN.Errors[0].Message == "" || errZH.Errors[0].Message == "" || errEN.Errors[0].Message == errZH.Errors[0].Message {
				t.Fatalf("%s expected distinct bilingual field messages, got en=%q zh=%q", tc.name, errEN.Errors[0].Message, errZH.Errors[0].Message)
			}
		}
	})

	t.Run("GET /users/:id returns 200, invalid uuid 422, and malformed JSON returns 400", func(t *testing.T) {
		h := newHTTPTestHarness(t, allowAllPerms())
		rec := h.doReq(http.MethodGet, "/api/v1/users/"+h.fx.mgrID.String(), "en-US", nil)
		if rec.Code != http.StatusOK || bytes.Contains(rec.Body.Bytes(), []byte("passwordHash")) {
			t.Fatalf("GET /users/:id = %d, body = %s", rec.Code, rec.Body.String())
		}

		badID := h.doReq(http.MethodGet, "/api/v1/users/not-a-uuid", "en-US", nil)
		if badID.Code != http.StatusUnprocessableEntity {
			t.Fatalf("GET /users/not-a-uuid = %d, want 422", badID.Code)
		}

		badJSON := h.doRawReq(http.MethodPost, "/api/v1/users", "en-US", strings.NewReader("{invalid"))
		if badJSON.Code != http.StatusBadRequest {
			t.Fatalf("malformed JSON POST /users = %d, want 400, body = %s", badJSON.Code, badJSON.Body.String())
		}
	})

	t.Run("POST /users creates 201, empty roles [], bilingual 409, and rejects disabled/revoked actor", func(t *testing.T) {
		h := newHTTPTestHarness(t, allowAllPerms())
		rec := h.doReq(http.MethodPost, "/api/v1/users", "en-US", map[string]any{
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

		// Duplicate username returns bilingual 409 USERS_USERNAME_CONFLICT.
		dupEN := h.doReq(http.MethodPost, "/api/v1/users", "en-US", map[string]any{
			"username": "alice",
			"email":    "alice2@example.com",
			"password": "password123",
		})
		dupZH := h.doReq(http.MethodPost, "/api/v1/users", "zh-CN", map[string]any{
			"username": "alice",
			"email":    "alice2@example.com",
			"password": "password123",
		})
		if dupEN.Code != http.StatusConflict || dupZH.Code != http.StatusConflict {
			t.Fatalf("duplicate POST /users = (%d, %d), want 409", dupEN.Code, dupZH.Code)
		}
		var bEN, bZH map[string]any
		_ = json.Unmarshal(dupEN.Body.Bytes(), &bEN)
		_ = json.Unmarshal(dupZH.Body.Bytes(), &bZH)
		if bEN["code"] != ErrCodeUsernameConflict || bEN["message"] == bZH["message"] {
			t.Fatalf("expected distinct bilingual 409 messages, got en=%v zh=%v", bEN, bZH)
		}

		// Zero UUID in roleIds must be rejected with 422.
		badRole := h.doReq(http.MethodPost, "/api/v1/users", "en-US", map[string]any{
			"username": "badrole",
			"email":    "badrole@example.com",
			"password": "password123",
			"roleIds":  []string{uuid.Nil.String()},
		})
		if badRole.Code != http.StatusUnprocessableEntity {
			t.Fatalf("POST /users with nil roleId = %d, want 422", badRole.Code)
		}

		// Disabled admin attempting self-re-enable receives 403 AUTH_ACCOUNT_DISABLED.
		h.fx.store.users[h.fx.superID].user.Status = StatusDisabled
		reEnable := h.doReq(http.MethodPatch, "/api/v1/users/"+h.fx.superID.String(), "en-US", map[string]any{
			"status": "active",
		})
		if reEnable.Code != http.StatusForbidden {
			t.Fatalf("disabled admin self-re-enable = %d, want 403, body=%s", reEnable.Code, reEnable.Body.String())
		}
		h.fx.store.users[h.fx.superID].user.Status = StatusActive

		// Revoked-session admin receives 401.
		h.currentSession = uuid.Must(uuid.NewV7())
		revokedReq := h.doReq(http.MethodPost, "/api/v1/users", "en-US", map[string]any{
			"username": "revokedcreate",
			"email":    "revokedcreate@example.com",
			"password": "password123",
		})
		if revokedReq.Code != http.StatusUnauthorized {
			t.Fatalf("revoked session POST /users = %d, want 401", revokedReq.Code)
		}
	})

	t.Run("PATCH /users/:id and DELETE /users/:id", func(t *testing.T) {
		h := newHTTPTestHarness(t, allowAllPerms())
		rec := h.doReq(http.MethodPatch, "/api/v1/users/"+h.fx.mgrID.String(), "en-US", map[string]any{
			"displayName": "Updated Manager",
			"roleIds":     []string{h.fx.roles.roleID("viewer").String()},
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("PATCH /users = %d, body = %s", rec.Code, rec.Body.String())
		}

		rec = h.doReq(http.MethodDelete, "/api/v1/users/"+h.fx.mgrID.String(), "en-US", nil)
		if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
			t.Fatalf("DELETE /users = %d (len %d), want 204 empty", rec.Code, rec.Body.Len())
		}

		rec = h.doReq(http.MethodGet, "/api/v1/users/"+h.fx.mgrID.String(), "en-US", nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET deleted user = %d, want 404", rec.Code)
		}
	})
}

func TestProfileHTTPRoutesContract(t *testing.T) {
	t.Run("unauthenticated GET /profile returns 401", func(t *testing.T) {
		h := newHTTPTestHarness(t, denyAllPerms())
		h.currentActor = uuid.Nil
		if rec := h.doReq(http.MethodGet, "/api/v1/profile", "en-US", nil); rec.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated GET /profile = %d, want 401", rec.Code)
		}
	})

	t.Run("GET and PATCH /profile update fields and avatar even when admin perms denied", func(t *testing.T) {
		h := newHTTPTestHarness(t, denyAllPerms())
		h.currentActor = h.fx.mgrID
		h.currentSession = h.fx.mgrSessID
		avatarID := uuid.Must(uuid.NewV7())
		h.fx.avatarFiles.valid[avatarID] = h.fx.mgrID

		rec := h.doReq(http.MethodGet, "/api/v1/profile", "en-US", nil)
		if rec.Code != http.StatusOK || rec.Header().Get("X-Request-Id") == "" {
			t.Fatalf("GET /profile = %d, reqID=%q, body=%s", rec.Code, rec.Header().Get("X-Request-Id"), rec.Body.String())
		}

		rec = h.doReq(http.MethodPatch, "/api/v1/profile", "en-US", map[string]any{
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
		rec = h.doReq(http.MethodPatch, "/api/v1/profile", "en-US", map[string]any{
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

		// Invalid avatar UUID returns 422; malformed JSON returns 400.
		rec = h.doReq(http.MethodPatch, "/api/v1/profile", "en-US", map[string]any{
			"avatarFileId": "not-a-uuid",
		})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("PATCH /profile bad uuid = %d, want 422", rec.Code)
		}
		badJSON := h.doRawReq(http.MethodPatch, "/api/v1/profile", "en-US", strings.NewReader("{bad"))
		if badJSON.Code != http.StatusBadRequest {
			t.Fatalf("PATCH /profile malformed json = %d, want 400", badJSON.Code)
		}
	})

	t.Run("GET and DELETE /profile/sessions enforce current, 422, 404, and bilingual conflict", func(t *testing.T) {
		h := newHTTPTestHarness(t, denyAllPerms())
		h.currentActor = h.fx.mgrID
		h.currentSession = h.fx.mgrSessID

		other1 := uuid.Must(uuid.NewV7())
		other2 := uuid.Must(uuid.NewV7())
		h.fx.sessions.activeByUser[h.fx.mgrID] = []UserSession{
			{ID: h.currentSession, IPAddress: "127.0.0.1", UserAgent: "CurUA"},
			{ID: other1, IPAddress: "10.0.0.1", UserAgent: "Other1UA"},
			{ID: other2, IPAddress: "10.0.0.2", UserAgent: "Other2UA"},
		}

		rec := h.doReq(http.MethodGet, "/api/v1/profile/sessions", "en-US", nil)
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

		// Invalid session UUID -> 422; unknown session UUID -> 404.
		if bad := h.doReq(http.MethodDelete, "/api/v1/profile/sessions/not-a-uuid", "en-US", nil); bad.Code != http.StatusUnprocessableEntity {
			t.Fatalf("DELETE invalid session uuid = %d, want 422", bad.Code)
		}
		if missing := h.doReq(http.MethodDelete, "/api/v1/profile/sessions/"+uuid.Must(uuid.NewV7()).String(), "en-US", nil); missing.Code != http.StatusNotFound {
			t.Fatalf("DELETE unknown session = %d, want 404", missing.Code)
		}

		rec = h.doReq(http.MethodDelete, "/api/v1/profile/sessions/"+h.currentSession.String(), "zh-CN", nil)
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

		recEN := h.doReq(http.MethodDelete, "/api/v1/profile/sessions/"+h.currentSession.String(), "en-US", nil)
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

		rec = h.doReq(http.MethodDelete, "/api/v1/profile/sessions/"+other1.String(), "en-US", nil)
		if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
			t.Fatalf("DELETE other session = %d (len %d), want 204", rec.Code, rec.Body.Len())
		}

		rec = h.doReq(http.MethodDelete, "/api/v1/profile/sessions", "en-US", nil)
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
		h := newHTTPTestHarness(t, denyAllPerms())
		rec := h.doReq(http.MethodPut, "/api/v1/profile/password", "zh-CN", map[string]any{
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

		rec = h.doReq(http.MethodPut, "/api/v1/profile/password", "en-US", map[string]any{
			"currentPassword": "password123",
			"newPassword":     "valid-new-password-1",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("valid password change = %d, body=%s", rec.Code, rec.Body.String())
		}

		// Exhaust remaining 3 attempts (total limit is 5/min/user) then assert 429 + Retry-After.
		for i := range 3 {
			r := h.doReq(http.MethodPut, "/api/v1/profile/password", "en-US", map[string]any{
				"currentPassword": "wrong-password",
				"newPassword":     "valid-new-password-2",
			})
			if r.Code != http.StatusBadRequest {
				t.Fatalf("attempt %d = %d, want 400", i+3, r.Code)
			}
		}
		limited := h.doReq(http.MethodPut, "/api/v1/profile/password", "en-US", map[string]any{
			"currentPassword": "wrong-password",
			"newPassword":     "valid-new-password-2",
		})
		if limited.Code != http.StatusTooManyRequests || limited.Header().Get("Retry-After") == "" {
			t.Fatalf("rate-limited password change = %d Retry-After=%q, want 429 with Retry-After", limited.Code, limited.Header().Get("Retry-After"))
		}
	})
}
