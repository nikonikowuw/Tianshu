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

func TestMenuTreeLifecycleAndGuards(t *testing.T) {
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

	svc := newServiceWithStore(st, ServiceDeps{
		Tx:    fakeTxRunner{},
		Cache: mem,
		I18n:  bundle,
		Log:   logger.New("test", "error", io.Discard),
	})
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

	// Root button creation must be rejected with 422 parentId: MENUS_INVALID_PARENT.
	if _, err := svc.CreateMenu(t.Context(), CreateMenuInput{
		Type:    MenuTypeButton,
		Code:    "root_btn",
		Title:   "Root Button",
		Visible: true,
	}); err == nil {
		t.Fatal("expected 422 when creating root button")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusUnprocessableEntity || len(appErr.Fields) == 0 || appErr.Fields[0].Message != ErrCodeMenuInvalidParent {
		t.Fatalf("unexpected root button error: %+v", err)
	}

	// Invalid menu code uses ACCESS_INVALID_MENU_CODE; overlong icon uses ACCESS_INVALID_MENU_ICON.
	if _, err := svc.CreateMenu(t.Context(), CreateMenuInput{
		Type:    MenuTypeDirectory,
		Code:    "1bad",
		Title:   "Bad Code",
		Icon:    strings.Repeat("x", 65),
		Visible: true,
	}); err == nil {
		t.Fatal("expected validation failure for bad code and icon")
	} else if appErr, ok := apperr.As(err); !ok || len(appErr.Fields) != 2 || appErr.Fields[0].Message != ErrCodeInvalidMenuCode || appErr.Fields[1].Message != ErrCodeInvalidMenuIcon {
		t.Fatalf("unexpected field errors: %+v", err)
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

	// Cannot change childless root directory type to button.
	btnType := MenuTypeButton
	if _, err := svc.UpdateMenu(t.Context(), UpdateMenuInput{
		MenuID: dir.ID,
		Type:   &btnType,
	}); err == nil {
		t.Fatal("expected 422 when changing root directory type to button")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusUnprocessableEntity || appErr.Fields[0].Field != "type" || appErr.Fields[0].Message != ErrCodeMenuInvalidParent {
		t.Fatalf("unexpected error changing root type to button: %+v", err)
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

	btn, err := svc.CreateMenu(t.Context(), CreateMenuInput{
		ParentID:       &page.ID,
		Type:           MenuTypeButton,
		Code:           "custom_export_btn",
		Title:          "Export",
		PermissionCode: PermUsersRead,
		Visible:        false,
	})
	if err != nil {
		t.Fatalf("CreateMenu(button): %v", err)
	}

	// Moving a button node to root via UpdateMenu(ClearParent) or ReorderMenus must fail with 422 MENUS_INVALID_PARENT.
	if _, err := svc.UpdateMenu(t.Context(), UpdateMenuInput{
		MenuID:      btn.ID,
		ClearParent: true,
	}); err == nil {
		t.Fatal("expected 422 when clearing button parent")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusUnprocessableEntity || appErr.Fields[0].Message != ErrCodeMenuInvalidParent {
		t.Fatalf("unexpected ClearParent error on button: %+v", err)
	}
	if _, err := svc.ReorderMenus(t.Context(), ReorderMenusInput{
		Items: []ReorderMenuItem{{ID: btn.ID, ParentID: nil, SortOrder: 1}},
	}); err == nil {
		t.Fatal("expected 422 when reordering button to root")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusUnprocessableEntity || appErr.Fields[0].Message != ErrCodeMenuInvalidParent {
		t.Fatalf("unexpected ReorderMenus root button error: %+v", err)
	}

	// Changing custom menu code returns 422 ACCESS_MENU_CODE_IMMUTABLE; empty custom title returns 422 ACCESS_INVALID_MENU_TITLE.
	newCode := "renamed_page"
	if _, err := svc.UpdateMenu(t.Context(), UpdateMenuInput{MenuID: page.ID, Code: &newCode}); err == nil {
		t.Fatal("expected 422 when changing custom menu code")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusUnprocessableEntity || appErr.Fields[0].Message != ErrCodeMenuCodeImmutable {
		t.Fatalf("unexpected custom menu code error: %+v", err)
	}
	emptyTitle := "   "
	if _, err := svc.UpdateMenu(t.Context(), UpdateMenuInput{MenuID: page.ID, Title: &emptyTitle}); err == nil {
		t.Fatal("expected 422 for blank custom menu title")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusUnprocessableEntity || appErr.Fields[0].Message != ErrCodeInvalidMenuTitle {
		t.Fatalf("unexpected blank title error: %+v", err)
	}

	// System menu code modification returns 409 MENUS_SYSTEM_IMMUTABLE.
	sysMenu := tree[0]
	hackedCode := "hacked_dir"
	if _, err := svc.UpdateMenu(t.Context(), UpdateMenuInput{MenuID: sysMenu.ID, Code: &hackedCode}); err == nil {
		t.Fatal("expected 409 MENUS_SYSTEM_IMMUTABLE when modifying system menu code")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeMenuSystemImmutable {
		t.Fatalf("expected %s, got %v", ErrCodeMenuSystemImmutable, err)
	}

	// Editing a system menu while passing its default translated title keeps Title="" (bilingual preserved),
	// while explicit custom title overrides and empty string resets back to Title="".
	zhGeneral := bundle.Translate(i18n.LangZhCN, sysMenu.TitleKey, nil)
	newIcon := "FolderKanban"
	preserved, err := svc.UpdateMenu(t.Context(), UpdateMenuInput{
		MenuID: sysMenu.ID,
		Title:  &zhGeneral,
		Icon:   &newIcon,
	})
	if err != nil {
		t.Fatalf("UpdateMenu(system preserve title_key): %v", err)
	}
	if preserved.Title != "" || preserved.Icon != newIcon {
		t.Fatalf("expected Title='' and Icon=%q, got Title=%q Icon=%q", newIcon, preserved.Title, preserved.Icon)
	}

	customSysTitle := "Workspace"
	overridden, err := svc.UpdateMenu(t.Context(), UpdateMenuInput{
		MenuID: sysMenu.ID,
		Title:  &customSysTitle,
	})
	if err != nil || overridden.Title != customSysTitle {
		t.Fatalf("UpdateMenu(system custom title) = %+v, err = %v", overridden, err)
	}
	resetTitle := ""
	restored, err := svc.UpdateMenu(t.Context(), UpdateMenuInput{
		MenuID: sysMenu.ID,
		Title:  &resetTitle,
	})
	if err != nil || restored.Title != "" {
		t.Fatalf("UpdateMenu(system reset title) = %+v, err = %v", restored, err)
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

	// ReorderMenus rejects duplicate IDs with ACCESS_DUPLICATE_MENU_ID.
	if _, err := svc.ReorderMenus(t.Context(), ReorderMenusInput{
		Items: []ReorderMenuItem{
			{ID: dir.ID, SortOrder: 1},
			{ID: dir.ID, SortOrder: 2},
		},
	}); err == nil {
		t.Fatalf("expected 422 for duplicate reorder items")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusUnprocessableEntity || appErr.Fields[0].Message != ErrCodeDuplicateMenuReorder {
		t.Fatalf("expected 422 %s, got %v", ErrCodeDuplicateMenuReorder, err)
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

	svc := newServiceWithStore(st, ServiceDeps{
		Tx:    fakeTxRunner{},
		Cache: mem,
		I18n:  bundle,
		Log:   logger.New("test", "error", io.Discard),
	})
	if err := svc.SyncCatalog(t.Context()); err != nil {
		t.Fatalf("SyncCatalog: %v", err)
	}

	adminUser := uuid.Must(uuid.NewV7())
	rolesReadOnlyUser := uuid.Must(uuid.NewV7())
	noPermUser := uuid.Must(uuid.NewV7())
	if err := svc.AssignRoleByCode(t.Context(), adminUser, RoleCodeSuperAdmin); err != nil {
		t.Fatalf("AssignRoleByCode: %v", err)
	}
	rolesReader, err := svc.CreateRole(t.Context(), CreateRoleInput{
		Code:        "roles_only",
		Name:        "Roles Only",
		Permissions: []string{PermRolesRead},
	})
	if err != nil {
		t.Fatalf("CreateRole(roles_only): %v", err)
	}
	if err := svc.AssignRoleByCode(t.Context(), rolesReadOnlyUser, rolesReader.Code); err != nil {
		t.Fatalf("AssignRoleByCode(rolesReadOnlyUser): %v", err)
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
		RequireOptions:    RequireAnyPermission(svc, PermUsersRead, PermRolesRead),
		RequireCreate:     RequirePermission(svc, PermRolesCreate),
		RequireUpdate:     RequirePermission(svc, PermRolesUpdate),
		RequireDelete:     RequirePermission(svc, PermRolesDelete),
		RequireMenuRead:   RequireAnyPermission(svc, PermMenusRead, PermRolesRead),
		RequireMenuCreate: RequirePermission(svc, PermMenusCreate),
		RequireMenuUpdate: RequirePermission(svc, PermMenusUpdate),
		RequireMenuDelete: RequirePermission(svc, PermMenusDelete),
	})

	doReq := func(method, path string, body any, lang string) *httptest.ResponseRecorder {
		var buf io.Reader
		if rawStr, ok := body.(string); ok {
			buf = strings.NewReader(rawStr)
		} else if body != nil {
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal request body: %v", err)
			}
			buf = bytes.NewReader(raw)
		}
		req := httptest.NewRequest(method, path, buf)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
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

	// 403 when lacking both menus:read and roles:read.
	currentUser = noPermUser
	if rec := doReq(http.MethodGet, "/api/v1/menus/tree", nil, "en-US"); rec.Code != http.StatusForbidden {
		t.Fatalf("forbidden GET /menus/tree = %d, want 403", rec.Code)
	}
	if rec := doReq(http.MethodGet, "/api/v1/menus/user", nil, "zh-CN"); rec.Code != http.StatusOK {
		t.Fatalf("authenticated GET /menus/user = %d, want 200", rec.Code)
	}

	// User holding roles:read (without menus:read) can GET /menus/tree, but cannot POST /menus.
	currentUser = rolesReadOnlyUser
	if rec := doReq(http.MethodGet, "/api/v1/menus/tree", nil, "en-US"); rec.Code != http.StatusOK {
		t.Fatalf("roles:read GET /menus/tree = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if rec := doReq(http.MethodPost, "/api/v1/menus", map[string]any{"type": "directory", "code": "denied", "title": "Denied"}, "en-US"); rec.Code != http.StatusForbidden {
		t.Fatalf("roles:read POST /menus = %d, want 403", rec.Code)
	}

	currentUser = adminUser
	// Malformed JSON -> 400 BAD_REQUEST.
	if rec := doReq(http.MethodPost, "/api/v1/menus", `{"type":`, "en-US"); rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed POST /menus = %d, want 400", rec.Code)
	}

	rec := doReq(http.MethodGet, "/api/v1/menus/tree", nil, "zh-CN")
	if rec.Code != http.StatusOK || rec.Header().Get("X-Request-Id") == "" {
		t.Fatalf("GET /menus/tree = %d, X-Request-Id=%q", rec.Code, rec.Header().Get("X-Request-Id"))
	}
	var treeBody struct {
		Items []struct {
			ID       string `json:"id"`
			Code     string `json:"code"`
			Title    string `json:"title"`
			Icon     string `json:"icon"`
			Children []struct {
				ID    string `json:"id"`
				Code  string `json:"code"`
				Title string `json:"title"`
			} `json:"children"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &treeBody); err != nil {
		t.Fatalf("decode zh-CN menu tree: %v", err)
	}
	wantZhGeneral := bundle.Translate(i18n.LangZhCN, "ACCESS_MENU_DIR_GENERAL", nil)
	if len(treeBody.Items) < 2 || treeBody.Items[0].Title != wantZhGeneral {
		t.Fatalf("unexpected zh-CN tree: %s", rec.Body.String())
	}

	// Editing a system menu's icon in zh-CN (sending its zh-CN title) preserves en-US localization!
	sysDirID := treeBody.Items[0].ID
	patchSysRec := doReq(http.MethodPatch, "/api/v1/menus/"+sysDirID, map[string]any{
		"title": treeBody.Items[0].Title,
		"icon":  "Compass",
	}, "zh-CN")
	if patchSysRec.Code != http.StatusOK {
		t.Fatalf("PATCH system menu icon = %d, body = %s", patchSysRec.Code, patchSysRec.Body.String())
	}
	recEnTree := doReq(http.MethodGet, "/api/v1/menus/tree", nil, "en-US")
	var enTreeBody struct {
		Items []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Icon  string `json:"icon"`
		} `json:"items"`
	}
	if err := json.Unmarshal(recEnTree.Body.Bytes(), &enTreeBody); err != nil {
		t.Fatalf("decode en-US menu tree: %v", err)
	}
	if enTreeBody.Items[0].Title != "General" || enTreeBody.Items[0].Icon != "Compass" {
		t.Fatalf("system menu lost en-US translation after zh-CN edit: %+v", enTreeBody.Items[0])
	}

	// Attempting to change system menu code via PATCH returns 409 MENUS_SYSTEM_IMMUTABLE.
	sysCodeRec := doReq(http.MethodPatch, "/api/v1/menus/"+sysDirID, map[string]any{
		"code": "tampered_code",
	}, "en-US")
	if sysCodeRec.Code != http.StatusConflict || !strings.Contains(sysCodeRec.Body.String(), `"code":"MENUS_SYSTEM_IMMUTABLE"`) {
		t.Fatalf("PATCH system menu code = %d, body = %s; want 409 MENUS_SYSTEM_IMMUTABLE", sysCodeRec.Code, sysCodeRec.Body.String())
	}

	// Root button via HTTP POST returns 422.
	rootBtnRec := doReq(http.MethodPost, "/api/v1/menus", map[string]any{
		"type":  "button",
		"code":  "orphan_btn",
		"title": "Orphan",
	}, "en-US")
	if rootBtnRec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST root button = %d, body = %s; want 422", rootBtnRec.Code, rootBtnRec.Body.String())
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
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created menu: %v", err)
	}
	if created.ParentID == nil || *created.ParentID != parentID {
		t.Fatalf("expected parentId %s, got %v", parentID, created.ParentID)
	}

	// PATCH /menus/:id with explicit "parentId": null moves child menu back to root.
	rec = doReq(http.MethodPatch, "/api/v1/menus/"+created.ID, map[string]any{
		"parentId": nil,
	}, "en-US")
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH /menus/:id parentId=null = %d, body = %s", rec.Code, rec.Body.String())
	}
	var patched struct {
		ParentID *string `json:"parentId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &patched); err != nil {
		t.Fatalf("decode patched menu: %v", err)
	}
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

	// DELETE /menus/:id returns 204.
	rec = doReq(http.MethodDelete, "/api/v1/menus/"+created.ID, nil, "en-US")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /menus/:id = %d, want 204", rec.Code)
	}
}
