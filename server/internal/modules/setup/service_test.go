package setup_test

import (
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

func TestSetupInitConcurrencyAndIdempotency(t *testing.T) {
	idStore := &fakeIdentity{}
	roles := newFakeRoles()
	svc := setup.NewService(idStore, roles, fakeTx{}, logger.New("test", "error", io.Discard))

	st, err := svc.Status(t.Context())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if st.Initialized {
		t.Fatal("expected initialized=false initially")
	}

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
				Username:    "admin",
				Email:       "admin@example.com",
				DisplayName: "Super Admin",
				Password:    "pass1234",
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
	if !after.Initialized {
		t.Fatal("expected initialized=true after init")
	}
}

func TestBootstrapFromEnvIsIdempotent(t *testing.T) {
	idStore := &fakeIdentity{}
	roles := newFakeRoles()
	svc := setup.NewService(idStore, roles, fakeTx{}, logger.New("test", "error", io.Discard))

	cfg := config.BootstrapConfig{
		AdminUsername: "admin",
		AdminEmail:    "admin@example.com",
		AdminPassword: "pass1234",
	}

	if err := svc.BootstrapFromEnv(t.Context(), cfg); err != nil {
		t.Fatalf("first BootstrapFromEnv() error = %v", err)
	}
	if len(idStore.users) != 1 {
		t.Fatalf("users count = %d, want 1", len(idStore.users))
	}

	// Second call on already-initialized DB must be a safe no-op.
	if err := svc.BootstrapFromEnv(t.Context(), cfg); err != nil {
		t.Fatalf("second BootstrapFromEnv() error = %v", err)
	}
	if len(idStore.users) != 1 {
		t.Fatalf("users count after second bootstrap = %d, want 1", len(idStore.users))
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
