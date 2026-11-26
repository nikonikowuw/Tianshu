package access

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestMenuTreeLifecycleAndGuards(t *testing.T) {
	st := newFakeStore()
	mem := cache.NewMemory()
	defer func() { _ = mem.Close() }()

	svc := newServiceWithStore(st, fakeTxRunner{}, mem, logger.New("test", "error", io.Discard))
	if err := svc.SyncCatalog(t.Context()); err != nil {
		t.Fatalf("SyncCatalog: %v", err)
	}

	tree, err := svc.ListMenuTree(t.Context())
	if err != nil {
		t.Fatalf("ListMenuTree: %v", err)
	}
	if len(tree) != 2 {
		t.Fatalf("expected 2 root directories, got %d", len(tree))
	}

	dir, err := svc.CreateMenu(t.Context(), CreateMenuInput{
		Type:      MenuTypeDirectory,
		Code:      "custom_dir",
		Title:     "Custom Dir",
		SortOrder: 30,
		Visible:   true,
	})
	if err != nil {
		t.Fatalf("CreateMenu(dir): %v", err)
	}

	// Duplicate code returns 409 MENUS_CODE_CONFLICT.
	if _, err := svc.CreateMenu(t.Context(), CreateMenuInput{
		Type:    MenuTypeDirectory,
		Code:    "custom_dir",
		Title:   "Dup",
		Visible: true,
	}); err == nil {
		t.Fatalf("expected MENUS_CODE_CONFLICT")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeMenuCodeConflict {
		t.Fatalf("expected %s, got %v", ErrCodeMenuCodeConflict, err)
	}

	page, err := svc.CreateMenu(t.Context(), CreateMenuInput{
		ParentID:       &dir.ID,
		Type:           MenuTypeMenu,
		Code:           "custom_page",
		Title:          "Custom Page",
		Path:           "/custom",
		Icon:           "Box",
		PermissionCode: PermUsersRead,
		SortOrder:      10,
		Visible:        true,
	})
	if err != nil {
		t.Fatalf("CreateMenu(page): %v", err)
	}

	// Cannot change dir type to button while it has a menu child.
	btnType := MenuTypeButton
	if _, err := svc.UpdateMenu(t.Context(), UpdateMenuInput{
		MenuID: dir.ID,
		Type:   &btnType,
	}); err == nil {
		t.Fatalf("expected 422 when changing parent type to incompatible")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Status != 422 {
		t.Fatalf("expected 422, got %v", err)
	}

	// Cannot delete directory while it still has children.
	if err := svc.DeleteMenu(t.Context(), dir.ID, nil, RequestMeta{}); err == nil {
		t.Fatalf("expected MENUS_HAS_CHILDREN")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeMenuHasChildren {
		t.Fatalf("expected %s, got %v", ErrCodeMenuHasChildren, err)
	}

	// Cannot delete system menu.
	if err := svc.DeleteMenu(t.Context(), tree[0].ID, nil, RequestMeta{}); err == nil {
		t.Fatalf("expected MENUS_SYSTEM_IMMUTABLE")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeMenuSystemImmutable {
		t.Fatalf("expected %s, got %v", ErrCodeMenuSystemImmutable, err)
	}

	// Cannot reparent dir under its own child (cycle).
	if _, err := svc.UpdateMenu(t.Context(), UpdateMenuInput{
		MenuID:   dir.ID,
		ParentID: &page.ID,
	}); err == nil {
		t.Fatalf("expected MENUS_CYCLE_DETECTED")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeMenuCycleDetected {
		t.Fatalf("expected %s, got %v", ErrCodeMenuCycleDetected, err)
	}

	// ReorderMenus rejects duplicate IDs.
	if _, err := svc.ReorderMenus(t.Context(), ReorderMenusInput{
		Items: []ReorderMenuItem{
			{ID: dir.ID, SortOrder: 1},
			{ID: dir.ID, SortOrder: 2},
		},
	}); err == nil {
		t.Fatalf("expected 422 for duplicate reorder items")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Status != 422 {
		t.Fatalf("expected 422, got %v", err)
	}

	// User without dashboard:read sees pruned tree.
	viewerRole, err := svc.CreateRole(t.Context(), CreateRoleInput{
		Code:        "custom_viewer",
		Name:        "Custom Viewer",
		Permissions: []string{PermUsersRead},
	})
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	userID := uuid.Must(uuid.NewV7())
	if err := svc.AssignRoleByCode(t.Context(), userID, viewerRole.Code); err != nil {
		t.Fatalf("AssignRoleByCode: %v", err)
	}

	userTree, err := svc.ListUserMenuTree(t.Context(), userID)
	if err != nil {
		t.Fatalf("ListUserMenuTree: %v", err)
	}
	if len(userTree) != 2 {
		t.Fatalf("expected 2 visible directories for viewer, got %d", len(userTree))
	}

	reordered, err := svc.ReorderMenus(t.Context(), ReorderMenusInput{
		Items: []ReorderMenuItem{{ID: dir.ID, ParentID: nil, SortOrder: 1}},
	})
	if err != nil {
		t.Fatalf("ReorderMenus: %v", err)
	}
	if reordered[0].Code != "custom_dir" {
		t.Fatalf("expected custom_dir first after reorder, got %s", reordered[0].Code)
	}
}

func TestSystemMenusLocalesComplete(t *testing.T) {
	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle: %v", err)
	}
	if err := bundle.RegisterFS("access", LocalesFS, "locales"); err != nil {
		t.Fatalf("RegisterFS: %v", err)
	}
	for _, m := range SystemMenus() {
		for _, lang := range []string{i18n.LangZhCN, i18n.LangEnUS} {
			got := bundle.Translate(lang, m.TitleKey, nil)
			if got == "" || got == m.TitleKey {
				t.Fatalf("missing translation for %s (%s)", m.TitleKey, lang)
			}
		}
	}
}

func TestMenuHTTPRoutesContract(t *testing.T) {
	st := newFakeStore()
	mem := cache.NewMemory()
	defer func() { _ = mem.Close() }()

	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle: %v", err)
	}
	if err := bundle.RegisterFS("access", LocalesFS, "locales"); err != nil {
		t.Fatalf("RegisterFS: %v", err)
	}

	svc := newServiceWithStore(st, fakeTxRunner{}, mem, logger.New("test", "error", io.Discard), ServiceDeps{I18n: bundle})
	if err := svc.SyncCatalog(t.Context()); err != nil {
		t.Fatalf("SyncCatalog: %v", err)
	}

	adminUser := uuid.Must(uuid.NewV7())
	noPermUser := uuid.Must(uuid.NewV7())
	if err := svc.AssignRoleByCode(t.Context(), adminUser, RoleCodeSuperAdmin); err != nil {
		t.Fatalf("AssignRoleByCode: %v", err)
	}

	engine, err := httpserver.NewEngine(httpserver.Options{
		Env:    "test",
		Logger: logger.New("test", "error", io.Discard),
		I18n:   bundle,
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	var currentUser uuid.UUID
	api := engine.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		if currentUser == uuid.Nil {
			_ = c.Error(apperr.Unauthorized(apperr.CodeUnauthorized))
			c.Abort()
			return
		}
		c.Request = c.Request.WithContext(authctx.WithIdentity(c.Request.Context(), authctx.Identity{
			UserID:    currentUser,
			SessionID: uuid.New(),
		}))
		c.Next()
	})

	NewHandler(svc, bundle).RegisterRoutes(api, RoutePermissions{
		RequireRead:       RequirePermission(svc, PermRolesRead),
		RequireCreate:     RequirePermission(svc, PermRolesCreate),
		RequireUpdate:     RequirePermission(svc, PermRolesUpdate),
		RequireDelete:     RequirePermission(svc, PermRolesDelete),
		RequireMenuRead:   RequirePermission(svc, PermMenusRead),
		RequireMenuCreate: RequirePermission(svc, PermMenusCreate),
		RequireMenuUpdate: RequirePermission(svc, PermMenusUpdate),
		RequireMenuDelete: RequirePermission(svc, PermMenusDelete),
	})

	doReq := func(method, path string, body any, lang string) *httptest.ResponseRecorder {
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

	// 401 when unauthenticated.
	currentUser = uuid.Nil
	if rec := doReq(http.MethodGet, "/api/v1/menus/tree", nil, "en-US"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET /menus/tree = %d, want 401", rec.Code)
	}

	// 403 when lacking menus:read.
	currentUser = noPermUser
	if rec := doReq(http.MethodGet, "/api/v1/menus/tree", nil, "en-US"); rec.Code != http.StatusForbidden {
		t.Fatalf("forbidden GET /menus/tree = %d, want 403", rec.Code)
	}
	if rec := doReq(http.MethodGet, "/api/v1/menus/user", nil, "zh-CN"); rec.Code != http.StatusOK {
		t.Fatalf("authenticated GET /menus/user = %d, want 200", rec.Code)
	}

	currentUser = adminUser
	rec := doReq(http.MethodGet, "/api/v1/menus/tree", nil, "zh-CN")
	if rec.Code != http.StatusOK || rec.Header().Get("X-Request-Id") == "" {
		t.Fatalf("GET /menus/tree = %d, X-Request-Id=%q", rec.Code, rec.Header().Get("X-Request-Id"))
	}
	var treeBody struct {
		Items []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &treeBody)
	if len(treeBody.Items) < 2 || treeBody.Items[0].Title != "通用" {
		t.Fatalf("unexpected zh-CN tree: %s", rec.Body.String())
	}

	// POST /menus creates 201 under first directory.
	parentID := treeBody.Items[0].ID
	rec = doReq(http.MethodPost, "/api/v1/menus", map[string]any{
		"parentId": parentID,
		"type":     "menu",
		"code":     "reports",
		"title":    "Reports",
		"path":     "/reports",
		"visible":  true,
	}, "en-US")
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /menus = %d, body = %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID       string  `json:"id"`
		ParentID *string `json:"parentId"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ParentID == nil || *created.ParentID != parentID {
		t.Fatalf("expected parentId %s, got %v", parentID, created.ParentID)
	}

	// PATCH /menus/:id with explicit "parentId": null moves child back to root.
	rec = doReq(http.MethodPatch, "/api/v1/menus/"+created.ID, map[string]any{
		"parentId": nil,
	}, "en-US")
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH /menus/:id parentId=null = %d, body = %s", rec.Code, rec.Body.String())
	}
	var patched struct {
		ParentID *string `json:"parentId"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &patched)
	if patched.ParentID != nil {
		t.Fatalf("expected parentId cleared to nil, got %v", *patched.ParentID)
	}

	// PUT /menus/reorder returns 200.
	rec = doReq(http.MethodPut, "/api/v1/menus/reorder", map[string]any{
		"items": []map[string]any{
			{"id": created.ID, "parentId": nil, "sortOrder": 5},
		},
	}, "en-US")
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /menus/reorder = %d, body = %s", rec.Code, rec.Body.String())
	}

	// DELETE /menus/:id returns 204, and system menu delete returns 409.
	if rec := doReq(http.MethodDelete, "/api/v1/menus/"+parentID, nil, "en-US"); rec.Code != http.StatusConflict {
		t.Fatalf("DELETE system menu = %d, want 409", rec.Code)
	}
	if rec := doReq(http.MethodDelete, "/api/v1/menus/"+created.ID, nil, "en-US"); rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("DELETE custom menu = %d (len %d), want 204", rec.Code, rec.Body.Len())
	}
}
