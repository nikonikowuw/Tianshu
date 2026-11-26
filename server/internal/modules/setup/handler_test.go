package setup_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tianshu/internal/modules/setup"
	"tianshu/internal/platform/config"
	"tianshu/internal/platform/httpserver"
	"tianshu/internal/platform/i18n"
	"tianshu/internal/platform/logger"
)

func TestSetupHTTPRoutesContract(t *testing.T) {
	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle() error = %v", err)
	}
	if err := bundle.RegisterFS("setup", setup.LocalesFS, "locales"); err != nil {
		t.Fatalf("RegisterFS() error = %v", err)
	}

	engine, err := httpserver.NewEngine(httpserver.Options{
		Env:    "test",
		Logger: logger.New("test", "error", io.Discard),
		I18n:   bundle,
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	svc, err := setup.NewService(
		t.Context(),
		config.SetupConfig{Mode: setup.ModeToken, Token: "valid-setup-token"},
		&fakeIdentity{},
		newFakeRoles(),
		fakeTx{},
		logger.New("test", "error", io.Discard),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	apiV1 := engine.Group("/api/v1")
	setup.NewHandler(svc).RegisterPublicRoutes(apiV1)

	// 1. GET /api/v1/setup/status returns uninitialized status.
	reqStatus := httptest.NewRequest(http.MethodGet, "/api/v1/setup/status", nil)
	recStatus := httptest.NewRecorder()
	engine.ServeHTTP(recStatus, reqStatus)
	if recStatus.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", recStatus.Code)
	}
	var st struct {
		Initialized   bool   `json:"initialized"`
		Mode          string `json:"mode"`
		RequiresToken bool   `json:"requiresToken"`
	}
	if err := json.Unmarshal(recStatus.Body.Bytes(), &st); err != nil {
		t.Fatalf("unmarshal status error = %v", err)
	}
	if st.Initialized || !st.RequiresToken || st.Mode != "token" {
		t.Fatalf("unexpected status payload: %+v", st)
	}

	// 2. Short password returns 422 validation error with JSON field names.
	reqInvalid := httptest.NewRequest(http.MethodPost, "/api/v1/setup/init", strings.NewReader(`{
		"setupToken": "valid-setup-token",
		"username": "admin",
		"email": "admin@example.com",
		"displayName": "Admin",
		"password": "short"
	}`))
	reqInvalid.Header.Set("Content-Type", "application/json")
	reqInvalid.RemoteAddr = "127.0.0.1:10001"
	recInvalid := httptest.NewRecorder()
	engine.ServeHTTP(recInvalid, reqInvalid)
	if recInvalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("short password status = %d, want 422 (body=%s)", recInvalid.Code, recInvalid.Body.String())
	}

	// 3. Valid request creates initial super admin (201) without envelope.
	reqCreate := httptest.NewRequest(http.MethodPost, "/api/v1/setup/init", strings.NewReader(`{
		"setupToken": "valid-setup-token",
		"username": "admin",
		"email": "admin@example.com",
		"displayName": "Super Admin",
		"password": "strong-password-1234"
	}`))
	reqCreate.Header.Set("Content-Type", "application/json")
	reqCreate.RemoteAddr = "127.0.0.1:10002"
	recCreate := httptest.NewRecorder()
	engine.ServeHTTP(recCreate, reqCreate)
	if recCreate.Code != http.StatusCreated {
		t.Fatalf("valid init status = %d, want 201 (body=%s)", recCreate.Code, recCreate.Body.String())
	}
	if !strings.Contains(recCreate.Body.String(), `"username":"admin"`) {
		t.Fatalf("unexpected init response body: %s", recCreate.Body.String())
	}

	// 4. Subsequent attempt returns 409 SETUP_ALREADY_INITIALIZED.
	reqSecond := httptest.NewRequest(http.MethodPost, "/api/v1/setup/init", strings.NewReader(`{
		"setupToken": "valid-setup-token",
		"username": "admin2",
		"email": "admin2@example.com",
		"displayName": "Admin Two",
		"password": "strong-password-1234"
	}`))
	reqSecond.Header.Set("Content-Type", "application/json")
	reqSecond.RemoteAddr = "127.0.0.1:10003"
	recSecond := httptest.NewRecorder()
	engine.ServeHTTP(recSecond, reqSecond)
	if recSecond.Code != http.StatusConflict {
		t.Fatalf("second init status = %d, want 409 (body=%s)", recSecond.Code, recSecond.Body.String())
	}
}
