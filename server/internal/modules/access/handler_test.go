package access

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

	"tianshu/internal/platform/cache"
	"tianshu/internal/platform/httpserver"
	"tianshu/internal/platform/i18n"
	"tianshu/internal/platform/logger"
	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/authctx"
)

func TestAccessHTTPRoutesContract(t *testing.T) {
	st := newFakeStore()
	mem := cache.NewMemory()
	defer func() { _ = mem.Close() }()

	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle() error = %v", err)
	}
	if err := bundle.RegisterFS("access", LocalesFS, "locales"); err != nil {
		t.Fatalf("RegisterFS(access) error = %v", err)
	}

	svc := newServiceWithStore(st, ServiceDeps{
		Tx:    fakeTxRunner{},
		Cache: mem,
		I18n:  bundle,
		Log:   logger.New("test", "error", io.Discard),
	})
	if err := svc.SyncCatalog(t.Context()); err != nil {
		t.Fatalf("SyncCatalog() error = %v", err)
	}

	adminUser := uuid.Must(uuid.NewV7())
	readOnlyUser := uuid.Must(uuid.NewV7())
	usersReadUser := uuid.Must(uuid.NewV7())
	noPermUser := uuid.Must(uuid.NewV7())

	if err := svc.AssignRoleByCode(t.Context(), adminUser, RoleCodeSuperAdmin); err != nil {
		t.Fatalf("AssignRoleByCode(admin) error = %v", err)
	}
	viewerRole, err := svc.CreateRole(t.Context(), CreateRoleInput{
		Code:        "role_viewer",
		Name:        "Role Viewer",
		Permissions: []string{PermRolesRead},
	})
	if err != nil {
		t.Fatalf("CreateRole(role_viewer) error = %v", err)
	}
	if err := svc.AssignRoleByCode(t.Context(), readOnlyUser, viewerRole.Code); err != nil {
		t.Fatalf("AssignRoleByCode(viewer) error = %v", err)
	}
	usersViewerRole, err := svc.CreateRole(t.Context(), CreateRoleInput{
		Code:        "user_viewer",
		Name:        "User Viewer",
		Permissions: []string{PermUsersRead},
	})
	if err != nil {
		t.Fatalf("CreateRole(user_viewer) error = %v", err)
	}
	if err := svc.AssignRoleByCode(t.Context(), usersReadUser, usersViewerRole.Code); err != nil {
		t.Fatalf("AssignRoleByCode(user_viewer) error = %v", err)
	}

	engine, err := httpserver.NewEngine(httpserver.Options{
		Env:    "test",
		Logger: logger.New("test", "error", io.Discard),
		I18n:   bundle,
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	mockAuth := func(c *gin.Context) {
		switch c.GetHeader("X-Test-User") {
		case "admin":
			c.Request = c.Request.WithContext(authctx.WithIdentity(c.Request.Context(), authctx.Identity{UserID: adminUser}))
		case "viewer":
			c.Request = c.Request.WithContext(authctx.WithIdentity(c.Request.Context(), authctx.Identity{UserID: readOnlyUser}))
		case "users_reader":
			c.Request = c.Request.WithContext(authctx.WithIdentity(c.Request.Context(), authctx.Identity{UserID: usersReadUser}))
		case "noperm":
			c.Request = c.Request.WithContext(authctx.WithIdentity(c.Request.Context(), authctx.Identity{UserID: noPermUser}))
		default:
			_ = c.Error(apperr.Unauthorized(apperr.CodeUnauthorized))
			c.Abort()
			return
		}
		c.Next()
	}

	api := engine.Group("/api/v1", mockAuth)
	NewHandler(svc, bundle).RegisterRoutes(api, RoutePermissions{
		RequireRead:       RequirePermission(svc, PermRolesRead),
		RequireOptions:    RequireAnyPermission(svc, PermUsersRead, PermRolesRead),
		RequireCreate:     RequirePermission(svc, PermRolesCreate),
		RequireUpdate:     RequirePermission(svc, PermRolesUpdate),
		RequireDelete:     RequirePermission(svc, PermRolesDelete),
		RequireMenuRead:   RequireAnyPermission(svc, PermMenusRead, PermRolesRead),
		RequireMenuCreate: RequirePermission(svc, PermMenusCreate),
		RequireMenuUpdate: RequirePermission(svc, PermMenusUpdate),
		RequireMenuDelete: RequirePermission(svc, PermMenusDelete),
	})

	t.Run("unauthenticated and forbidden requests are rejected", func(t *testing.T) {
		rec := doJSONReq(engine, http.MethodGet, "/api/v1/permissions", "", "", "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		if rec.Header().Get("X-Request-Id") == "" {
			t.Fatal("missing X-Request-Id header")
		}

		rec = doJSONReq(engine, http.MethodGet, "/api/v1/permissions", "noperm", "", "")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}

		rec = doJSONReq(engine, http.MethodPost, "/api/v1/roles", "viewer", "en-US", `{"code":"blocked","name":"Blocked"}`)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("viewer POST /roles status = %d, want 403", rec.Code)
		}
	})

	t.Run("GET /permissions localizes group labels and permission copy by Accept-Language", func(t *testing.T) {
		recZh := doJSONReq(engine, http.MethodGet, "/api/v1/permissions", "viewer", "zh-CN", "")
		if recZh.Code != http.StatusOK {
			t.Fatalf("zh-CN status = %d, body = %s", recZh.Code, recZh.Body.String())
		}
		var zhResp listPermissionsResponse
		if err := json.Unmarshal(recZh.Body.Bytes(), &zhResp); err != nil {
			t.Fatalf("decode zh-CN response: %v", err)
		}
		wantZhLabel := bundle.Translate(i18n.LangZhCN, "ACCESS_MODULE_DASHBOARD", nil)
		wantZhPerm := bundle.Translate(i18n.LangZhCN, "ACCESS_PERM_DASHBOARD_READ_NAME", nil)
		if len(zhResp.Groups) != 5 || zhResp.Groups[0].Label != wantZhLabel || zhResp.Groups[0].Permissions[0].Name != wantZhPerm {
			t.Fatalf("unexpected zh-CN permission catalog: %+v", zhResp)
		}

		recEn := doJSONReq(engine, http.MethodGet, "/api/v1/permissions", "viewer", "en-US", "")
		if recEn.Code != http.StatusOK {
			t.Fatalf("en-US status = %d, body = %s", recEn.Code, recEn.Body.String())
		}
		var enResp listPermissionsResponse
		if err := json.Unmarshal(recEn.Body.Bytes(), &enResp); err != nil {
			t.Fatalf("decode en-US response: %v", err)
		}
		if enResp.Groups[0].Label != "Dashboard" || enResp.Groups[0].Permissions[0].Name != "View Dashboard" {
			t.Fatalf("unexpected en-US permission catalog: %+v", enResp)
		}
	})

	t.Run("GET /roles/options enforces RequireAnyPermission and localizes system role", func(t *testing.T) {
		if rec := doJSONReq(engine, http.MethodGet, "/api/v1/roles/options", "", "en-US", ""); rec.Code != http.StatusUnauthorized {
			t.Fatalf("anon GET /roles/options status = %d, want 401", rec.Code)
		}
		if rec := doJSONReq(engine, http.MethodGet, "/api/v1/roles/options", "noperm", "en-US", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("noperm GET /roles/options status = %d, want 403", rec.Code)
		}

		// User with only users:read can access /roles/options, while /roles remains 403.
		if rec := doJSONReq(engine, http.MethodGet, "/api/v1/roles", "users_reader", "en-US", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("users_reader GET /roles status = %d, want 403", rec.Code)
		}
		recEn := doJSONReq(engine, http.MethodGet, "/api/v1/roles/options", "users_reader", "en-US", "")
		if recEn.Code != http.StatusOK || recEn.Header().Get("X-Request-Id") == "" {
			t.Fatalf("users_reader GET /roles/options status = %d, X-Request-Id=%q", recEn.Code, recEn.Header().Get("X-Request-Id"))
		}
		var enOpts listRoleOptionsResponse
		if err := json.Unmarshal(recEn.Body.Bytes(), &enOpts); err != nil {
			t.Fatalf("decode en-US options: %v", err)
		}
		if len(enOpts.Items) < 3 || !enOpts.Items[0].IsSystem || enOpts.Items[0].Code != RoleCodeSuperAdmin || enOpts.Items[0].Name != "Super Administrator" {
			t.Fatalf("unexpected en-US role options: %+v", enOpts)
		}

		recZh := doJSONReq(engine, http.MethodGet, "/api/v1/roles/options", "viewer", "zh-CN", "")
		if recZh.Code != http.StatusOK {
			t.Fatalf("viewer GET /roles/options zh-CN status = %d", recZh.Code)
		}
		var zhOpts listRoleOptionsResponse
		if err := json.Unmarshal(recZh.Body.Bytes(), &zhOpts); err != nil {
			t.Fatalf("decode zh-CN options: %v", err)
		}
		wantZhSuper := bundle.Translate(i18n.LangZhCN, "ACCESS_ROLE_SUPER_ADMIN_NAME", nil)
		if len(zhOpts.Items) == 0 || zhOpts.Items[0].Name != wantZhSuper {
			t.Fatalf("unexpected zh-CN role options: %+v", zhOpts)
		}
	})

	t.Run("GET /roles returns paginated roles with localized system role name", func(t *testing.T) {
		rec := doJSONReq(engine, http.MethodGet, "/api/v1/roles?page=1&pageSize=10&isSystem=true", "viewer", "en-US", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Items    []roleResponse `json:"items"`
			Total    int64          `json:"total"`
			Page     int            `json:"page"`
			PageSize int            `json:"pageSize"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode roles list: %v", err)
		}
		if body.Total != 1 || len(body.Items) != 1 || body.Items[0].Name != "Super Administrator" {
			t.Fatalf("unexpected system role list: %+v", body)
		}
	})

	t.Run("POST, GET, PATCH, DELETE /roles full lifecycle and error contracts", func(t *testing.T) {
		// Malformed JSON -> 400 BAD_REQUEST (not 422)
		badJSONRec := doJSONReq(engine, http.MethodPost, "/api/v1/roles", "admin", "en-US", `{"code":`)
		if badJSONRec.Code != http.StatusBadRequest || !strings.Contains(badJSONRec.Body.String(), `"code":"BAD_REQUEST"`) {
			t.Fatalf("malformed JSON POST /roles status = %d, body = %s", badJSONRec.Code, badJSONRec.Body.String())
		}

		// Create role -> 201
		rec := doJSONReq(engine, http.MethodPost, "/api/v1/roles", "admin", "en-US", `{
			"code": "content_editor",
			"name": "Content Editor",
			"description": "Manages files",
			"permissions": ["files:read", "files:upload"]
		}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("POST /roles status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var created roleResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
			t.Fatalf("decode created role: %v", err)
		}
		if created.Code != "content_editor" || created.PermissionCount != 2 {
			t.Fatalf("unexpected created role: %+v", created)
		}

		// Duplicate code -> 409 ROLES_CODE_CONFLICT with localized message by Accept-Language
		dupRecZh := doJSONReq(engine, http.MethodPost, "/api/v1/roles", "admin", "zh-CN", `{"code":"content_editor","name":"Dup"}`)
		if dupRecZh.Code != http.StatusConflict || !strings.Contains(dupRecZh.Body.String(), `"code":"ROLES_CODE_CONFLICT"`) {
			t.Fatalf("duplicate zh status = %d, body = %s", dupRecZh.Code, dupRecZh.Body.String())
		}
		dupRecEn := doJSONReq(engine, http.MethodPost, "/api/v1/roles", "admin", "en-US", `{"code":"content_editor","name":"Dup"}`)
		if dupRecEn.Code != http.StatusConflict || !strings.Contains(dupRecEn.Body.String(), `"code":"ROLES_CODE_CONFLICT"`) {
			t.Fatalf("duplicate en status = %d, body = %s", dupRecEn.Code, dupRecEn.Body.String())
		}
		var dupMsgZh, dupMsgEn struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(dupRecZh.Body.Bytes(), &dupMsgZh); err != nil {
			t.Fatalf("decode zh conflict body: %v", err)
		}
		if err := json.Unmarshal(dupRecEn.Body.Bytes(), &dupMsgEn); err != nil {
			t.Fatalf("decode en conflict body: %v", err)
		}
		if dupMsgZh.Message == "" || dupMsgEn.Message == "" || dupMsgZh.Message == dupMsgEn.Message {
			t.Fatalf("expected localized conflict messages to differ, zh=%q en=%q", dupMsgZh.Message, dupMsgEn.Message)
		}

		// Empty search result serializes items as [] (not null)
		emptyRec := doJSONReq(engine, http.MethodGet, "/api/v1/roles?q=definitely-no-match", "viewer", "en-US", "")
		if emptyRec.Code != http.StatusOK || !strings.Contains(emptyRec.Body.String(), `"items":[]`) {
			t.Fatalf("empty list status = %d, body = %s", emptyRec.Code, emptyRec.Body.String())
		}

		// Invalid permission code -> 422 VALIDATION_FAILED
		badPermRec := doJSONReq(engine, http.MethodPost, "/api/v1/roles", "admin", "en-US", `{"code":"bad_role","name":"Bad","permissions":["bogus:write"]}`)
		if badPermRec.Code != http.StatusUnprocessableEntity || !strings.Contains(badPermRec.Body.String(), `"field":"permissions"`) {
			t.Fatalf("invalid permission status = %d, body = %s", badPermRec.Code, badPermRec.Body.String())
		}

		// GET /roles/:id -> 200
		getRec := doJSONReq(engine, http.MethodGet, "/api/v1/roles/"+created.ID, "viewer", "en-US", "")
		if getRec.Code != http.StatusOK {
			t.Fatalf("GET /roles/:id status = %d, body = %s", getRec.Code, getRec.Body.String())
		}

		// PATCH /roles/:id -> 200
		patchRec := doJSONReq(engine, http.MethodPatch, "/api/v1/roles/"+created.ID, "admin", "en-US", `{"name":"Senior Editor","permissions":["files:read"]}`)
		if patchRec.Code != http.StatusOK {
			t.Fatalf("PATCH /roles/:id status = %d, body = %s", patchRec.Code, patchRec.Body.String())
		}
		var patched roleResponse
		if err := json.Unmarshal(patchRec.Body.Bytes(), &patched); err != nil {
			t.Fatalf("decode patched role: %v", err)
		}
		if patched.Name != "Senior Editor" || patched.PermissionCount != 1 {
			t.Fatalf("unexpected patched role: %+v", patched)
		}

		// PATCH system role -> 409 ROLES_SYSTEM_IMMUTABLE
		superAdminID := st.rolesByCode[RoleCodeSuperAdmin]
		sysPatchRec := doJSONReq(engine, http.MethodPatch, "/api/v1/roles/"+superAdminID.String(), "admin", "en-US", `{"name":"Hacked"}`)
		if sysPatchRec.Code != http.StatusConflict || !strings.Contains(sysPatchRec.Body.String(), `"code":"ROLES_SYSTEM_IMMUTABLE"`) {
			t.Fatalf("system role patch status = %d, body = %s", sysPatchRec.Code, sysPatchRec.Body.String())
		}

		// DELETE bound role -> 409 ROLES_IN_USE
		inUseRec := doJSONReq(engine, http.MethodDelete, "/api/v1/roles/"+viewerRole.ID.String(), "admin", "en-US", "")
		if inUseRec.Code != http.StatusConflict || !strings.Contains(inUseRec.Body.String(), `"code":"ROLES_IN_USE"`) {
			t.Fatalf("in-use delete status = %d, body = %s", inUseRec.Code, inUseRec.Body.String())
		}

		// DELETE unused role -> 204 with empty body
		delRec := doJSONReq(engine, http.MethodDelete, "/api/v1/roles/"+created.ID, "admin", "en-US", "")
		if delRec.Code != http.StatusNoContent || delRec.Body.Len() != 0 {
			t.Fatalf("DELETE status = %d, bodyLen = %d", delRec.Code, delRec.Body.Len())
		}

		// Invalid query parameter -> 422
		badQueryRec := doJSONReq(engine, http.MethodGet, "/api/v1/roles?isSystem=maybe", "viewer", "en-US", "")
		if badQueryRec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("bad isSystem query status = %d, body = %s", badQueryRec.Code, badQueryRec.Body.String())
		}

		// Missing role ID -> 404 ACCESS_ROLE_NOT_FOUND across GET/PATCH/DELETE
		missingID := uuid.Must(uuid.NewV7()).String()
		for _, tc := range []struct {
			method string
			body   string
		}{
			{http.MethodGet, ""},
			{http.MethodPatch, `{"name":"Ghost"}`},
			{http.MethodDelete, ""},
		} {
			notFoundRec := doJSONReq(engine, tc.method, "/api/v1/roles/"+missingID, "admin", "en-US", tc.body)
			if notFoundRec.Code != http.StatusNotFound || !strings.Contains(notFoundRec.Body.String(), `"code":"ACCESS_ROLE_NOT_FOUND"`) {
				t.Fatalf("%s missing role status = %d, body = %s", tc.method, notFoundRec.Code, notFoundRec.Body.String())
			}
		}
	})
}

func doJSONReq(engine *gin.Engine, method, path, testUser, acceptLang, rawBody string) *httptest.ResponseRecorder {
	var bodyReader io.Reader
	if rawBody != "" {
		bodyReader = bytes.NewBufferString(rawBody)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	if rawBody != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if testUser != "" {
		req.Header.Set("X-Test-User", testUser)
	}
	if acceptLang != "" {
		req.Header.Set("Accept-Language", acceptLang)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}
