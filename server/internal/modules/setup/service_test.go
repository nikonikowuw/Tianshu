package setup_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"tianshu/internal/modules/setup"
	"tianshu/internal/platform/config"
	"tianshu/internal/platform/database"
	"tianshu/internal/platform/i18n"
	"tianshu/internal/platform/logger"
	"tianshu/internal/shared/apperr"
)

type fakeIdentity struct {
	mu    sync.Mutex
	users []setup.CreatedAdmin
}

func (f *fakeIdentity) CountUsers(_ context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return int64(len(f.users)), nil
}

func (f *fakeIdentity) CreateInitialAdmin(_ context.Context, p setup.CreateAdminParams) (setup.CreatedAdmin, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	admin := setup.CreatedAdmin{
		ID:          uuid.Must(uuid.NewV7()),
		Username:    strings.ToLower(strings.TrimSpace(p.Username)),
		Email:       strings.ToLower(strings.TrimSpace(p.Email)),
		DisplayName: strings.TrimSpace(p.DisplayName),
	}
	f.users = append(f.users, admin)
	return admin, nil
}

type fakeRoles struct {
	mu       sync.Mutex
	assigned map[uuid.UUID]string
}

func newFakeRoles() *fakeRoles {
	return &fakeRoles{assigned: make(map[uuid.UUID]string)}
}

func (f *fakeRoles) AssignRoleByCode(_ context.Context, userID uuid.UUID, roleCode string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.assigned[userID] = roleCode
	return nil
}

type fakeTx struct{}

func (fakeTx) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return database.WithSimulatedTx(ctx, fn)
}

func TestSetupTokenModeAndConcurrency(t *testing.T) {
	idStore := &fakeIdentity{}
	roles := newFakeRoles()

	var logBuf bytes.Buffer
	svc, err := setup.NewService(
		t.Context(),
		config.SetupConfig{Mode: setup.ModeToken, Token: "preset-secret-token-123"},
		idStore,
		roles,
		fakeTx{},
		logger.New("test", "info", &logBuf),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	st, err := svc.Status(t.Context())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if st.Initialized || !st.RequiresToken || st.Mode != setup.ModeToken {
		t.Fatalf("unexpected initial status: %+v", st)
	}

	// Wrong token must return 403 SETUP_TOKEN_INVALID.
	_, err = svc.Init(t.Context(), setup.InitInput{
		SetupToken:  "wrong-token",
		Username:    "admin",
		Email:       "admin@example.com",
		DisplayName: "Admin",
		Password:    "strong-password-123",
	})
	appErr, ok := apperr.As(err)
	if !ok || appErr.Status != http.StatusForbidden || appErr.Code != setup.ErrCodeTokenInvalid {
		t.Fatalf("wrong token err = %v, want 403 %s", err, setup.ErrCodeTokenInvalid)
	}

	// Concurrent submissions must allow exactly one creation and reject all others with 409.
	const workers = 12
	var (
		wg        sync.WaitGroup
		succeeded atomic.Int32
		conflicts atomic.Int32
	)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			admin, initErr := svc.Init(t.Context(), setup.InitInput{
				SetupToken:  "preset-secret-token-123",
				Username:    "admin",
				Email:       "admin@example.com",
				DisplayName: "Super Admin",
				Password:    "strong-password-123",
			})
			if initErr == nil {
				succeeded.Add(1)
				if roles.assigned[admin.ID] != "super_admin" {
					t.Errorf("assigned role = %q, want super_admin", roles.assigned[admin.ID])
				}
				return
			}
			if ae, isApp := apperr.As(initErr); isApp && ae.Status == http.StatusConflict && ae.Code == setup.ErrCodeAlreadyInitialized {
				conflicts.Add(1)
				return
			}
			t.Errorf("unexpected concurrent error: %v", initErr)
		}()
	}
	wg.Wait()

	if succeeded.Load() != 1 || conflicts.Load() != workers-1 {
		t.Fatalf("succeeded=%d conflicts=%d, want 1 and %d", succeeded.Load(), conflicts.Load(), workers-1)
	}

	after, err := svc.Status(t.Context())
	if err != nil {
		t.Fatalf("Status() after init error = %v", err)
	}
	if !after.Initialized || after.RequiresToken {
		t.Fatalf("unexpected status after init: %+v", after)
	}
}

func TestSetupLocalAndOpenModes(t *testing.T) {
	t.Run("local mode rejects public IP and accepts loopback/private IP", func(t *testing.T) {
		idStore := &fakeIdentity{}
		roles := newFakeRoles()
		svc, err := setup.NewService(
			t.Context(),
			config.SetupConfig{Mode: setup.ModeLocal},
			idStore,
			roles,
			fakeTx{},
			logger.New("test", "error", io.Discard),
		)
		if err != nil {
			t.Fatalf("NewService() error = %v", err)
		}

		_, err = svc.Init(t.Context(), setup.InitInput{
			Username:    "admin",
			Email:       "admin@example.com",
			DisplayName: "Admin",
			Password:    "strong-password-123",
			ClientIP:    "8.8.8.8",
		})
		appErr, ok := apperr.As(err)
		if !ok || appErr.Status != http.StatusForbidden || appErr.Code != setup.ErrCodeLocalOnly {
			t.Fatalf("public IP err = %v, want 403 %s", err, setup.ErrCodeLocalOnly)
		}

		if _, err := svc.Init(t.Context(), setup.InitInput{
			Username:    "admin",
			Email:       "admin@example.com",
			DisplayName: "Admin",
			Password:    "strong-password-123",
			ClientIP:    "127.0.0.1",
		}); err != nil {
			t.Fatalf("loopback IP Init() error = %v", err)
		}
	})

	t.Run("open mode requires explicit allow_open", func(t *testing.T) {
		svc, err := setup.NewService(
			t.Context(),
			config.SetupConfig{Mode: setup.ModeOpen, AllowOpen: false},
			&fakeIdentity{},
			newFakeRoles(),
			fakeTx{},
			logger.New("test", "error", io.Discard),
		)
		if err != nil {
			t.Fatalf("NewService() error = %v", err)
		}
		if _, err := svc.Init(t.Context(), setup.InitInput{
			Username:    "admin",
			Email:       "admin@example.com",
			DisplayName: "Admin",
			Password:    "strong-password-123",
			ClientIP:    "8.8.8.8",
		}); err == nil {
			t.Fatal("expected rejection when allow_open is false")
		}
	})
}

func TestSetupAutoGeneratesTokenAndLogsIt(t *testing.T) {
	var logBuf bytes.Buffer
	_, err := setup.NewService(
		t.Context(),
		config.SetupConfig{Mode: setup.ModeToken},
		&fakeIdentity{},
		newFakeRoles(),
		fakeTx{},
		logger.New("test", "info", &logBuf),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if !strings.Contains(logBuf.String(), "setup_token=") {
		t.Fatalf("expected generated setup_token in startup log, got: %s", logBuf.String())
	}
}

func TestSetupLocalesComplete(t *testing.T) {
	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle() error = %v", err)
	}
	if err := bundle.RegisterFS("setup", setup.LocalesFS, "locales"); err != nil {
		t.Fatalf("RegisterFS(setup) error = %v", err)
	}
	if err := bundle.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
