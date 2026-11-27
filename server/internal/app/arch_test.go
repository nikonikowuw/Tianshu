package app_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tianshu/internal/app"
	"tianshu/internal/platform/config"
)

const modulePrefix = "tianshu/"

func shouldSkipDir(name string) bool {
	return name == ".git" || name == "tmp" || name == "bin" || name == "vendor"
}

// TestArchitectureBoundaries enforces package dependency direction, module encapsulation, init bans, and //go:embed allowlist.
func TestArchitectureBoundaries(t *testing.T) {
	serverRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve server root: %v", err)
	}

	fset := token.NewFileSet()
	err = filepath.WalkDir(serverRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		rel, err := filepath.Rel(serverRoot, path)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)

		fileAST, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			t.Errorf("parse %s: %v", relSlash, err)
			return nil
		}

		checkEmbedAllowlist(t, relSlash, path)
		checkModuleStoreEncapsulation(t, relSlash, fileAST)
		checkModuleInitForbidden(t, relSlash, fileAST)
		return nil
	})
	if err != nil {
		t.Fatalf("walk server root: %v", err)
	}
}

func checkModuleStoreEncapsulation(t *testing.T, relPath string, fileAST *ast.File) {
	t.Helper()
	if !strings.HasPrefix(relPath, "internal/modules/") || strings.Contains(relPath, "/internal/dbgen/") {
		return
	}
	for _, decl := range fileAST.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil && (d.Name.Name == "NewStore" || d.Name.Name == "NewPGStore") {
				t.Errorf("%s must not export %s (store must stay private inside module)", relPath, d.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok && (ts.Name.Name == "Store" || ts.Name.Name == "PGStore") {
					t.Errorf("%s must not export %s (store must stay private inside module)", relPath, ts.Name.Name)
				}
			}
		}
	}
}

func checkModuleInitForbidden(t *testing.T, relPath string, fileAST *ast.File) {
	t.Helper()
	if !strings.HasPrefix(relPath, "internal/modules/") || strings.Contains(relPath, "/internal/dbgen/") {
		return
	}
	for _, decl := range fileAST.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "init" {
			t.Errorf("%s must not define func init() inside a business module", relPath)
		}
	}
}

func checkEmbedAllowlist(t *testing.T, relPath, fullPath string) {
	t.Helper()
	raw, err := os.ReadFile(fullPath)
	if err != nil {
		t.Errorf("read %s: %v", relPath, err)
		return
	}
	if !strings.Contains(string(raw), "//go:embed") {
		return
	}

	allowed := relPath == "internal/webui/webui.go" ||
		strings.HasPrefix(relPath, "migrations/") ||
		strings.HasPrefix(relPath, "internal/platform/i18n/") ||
		isModuleLocalesFile(relPath)

	if !allowed {
		t.Errorf("%s uses //go:embed outside allowed locations", relPath)
	}
}

func isModuleLocalesFile(relPath string) bool {
	parts := strings.Split(relPath, "/")
	return len(parts) == 4 && parts[0] == "internal" && parts[1] == "modules" && parts[3] == "locales.go"
}

func TestPackageDependencyDirection(t *testing.T) {
	serverRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve server root: %v", err)
	}

	fset := token.NewFileSet()
	err = filepath.WalkDir(serverRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		rel, err := filepath.Rel(serverRoot, path)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)

		fileAST, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Errorf("parse %s: %v", relSlash, err)
			return nil
		}

		for _, spec := range fileAST.Imports {
			imp := strings.Trim(spec.Path.Value, `"`)
			verifyImportRule(t, relSlash, imp)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk server root: %v", err)
	}
}

func verifyImportRule(t *testing.T, relPath, imp string) {
	t.Helper()

	switch {
	case strings.HasPrefix(relPath, "internal/webui/"):
		if strings.Contains(imp, ".") || strings.HasPrefix(imp, modulePrefix) {
			t.Errorf("webui package %s must only import Go stdlib, got %q", relPath, imp)
		}
	case strings.HasPrefix(relPath, "internal/shared/"):
		if strings.HasPrefix(imp, modulePrefix+"internal/platform/") ||
			strings.HasPrefix(imp, modulePrefix+"internal/modules/") ||
			imp == modulePrefix+"internal/app" {
			t.Errorf("shared package %s must remain a leaf, got %q", relPath, imp)
		}
	case strings.HasPrefix(relPath, "internal/platform/"):
		if strings.HasPrefix(imp, modulePrefix+"internal/modules/") || imp == modulePrefix+"internal/app" {
			t.Errorf("platform package %s must not import modules or app, got %q", relPath, imp)
		}
	case strings.HasPrefix(relPath, "internal/modules/"):
		curDomain := strings.Split(relPath, "/")[2]
		if rest, ok := strings.CutPrefix(imp, modulePrefix+"internal/modules/"); ok {
			targetDomain := strings.Split(rest, "/")[0]
			if targetDomain != curDomain {
				t.Errorf("module %s (%s) must not import sibling module %s", curDomain, relPath, targetDomain)
			}
		}
		dbgenPkg := modulePrefix + "internal/modules/" + curDomain + "/internal/dbgen"
		storePath := "internal/modules/" + curDomain + "/store.go"
		if imp == dbgenPkg && relPath != storePath && !strings.HasPrefix(relPath, "internal/modules/"+curDomain+"/internal/dbgen/") {
			t.Errorf("module file %s must not import %s directly (encapsulate SQL behind %s)", relPath, dbgenPkg, storePath)
		}
	case strings.HasPrefix(relPath, "cmd/server/"):
		if strings.HasPrefix(imp, modulePrefix+"internal/modules/") {
			t.Errorf("cmd/server (%s) must wire via internal/app, not import %q", relPath, imp)
		}
	}
}

func TestAppAPINotFoundReturnsJSON(t *testing.T) {
	cfg := config.Default()
	cfg.App.Env = "test"
	cfg.Storage.LocalRoot = t.TempDir()

	application, err := app.New(t.Context(), cfg, app.Options{SkipDB: true})
	if err != nil {
		t.Fatalf("app.New() error = %v", err)
	}
	defer func() { _ = application.Close(t.Context()) }()

	for _, target := range []string{
		"/api",
		"/api/v1/not-found",
		"//api/v1/not-found",
		"/./api/v1/not-found",
		"/api/v1/../v1/not-found",
	} {
		t.Run("api_not_found_"+target, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, target, nil)
			rec := httptest.NewRecorder()
			application.Engine().ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Fatalf("GET %s status = %d, want 404", target, rec.Code)
			}
			if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("GET %s Content-Type = %q, want application/json", target, rec.Header().Get("Content-Type"))
			}
			if rec.Header().Get("X-Request-Id") == "" {
				t.Fatalf("GET %s missing X-Request-Id header", target)
			}
		})
	}

	t.Run("spa_fallback_does_not_return_json_404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/users/123", nil)
		rec := httptest.NewRecorder()
		application.Engine().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK && rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("GET /users/123 status = %d, want 200 or 503", rec.Code)
		}
		if strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("GET /users/123 unexpectedly returned JSON Content-Type = %q", rec.Header().Get("Content-Type"))
		}
	})
}
