package setup

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"sync"

	"tianshu/internal/platform/config"
	"tianshu/internal/platform/database"
	"tianshu/internal/platform/security"
	"tianshu/internal/shared/apperr"
)

// setupAdvisoryLockKey is a fixed application-scoped PostgreSQL advisory lock ID
// used to serialize concurrent initial setup submissions across instances.
const setupAdvisoryLockKey int64 = 0x7469616e73687501

const superAdminRoleCode = "super_admin"

// Service orchestrates one-time initial super administrator creation.
type Service struct {
	cfg      config.SetupConfig
	identity IdentityManager
	roles    RoleAssigner
	tx       TxRunner
	log      *slog.Logger

	mu       sync.Mutex
	oneToken string
}

// NewService creates a setup Service and initializes the one-time setup token when needed.
func NewService(
	ctx context.Context,
	cfg config.SetupConfig,
	identity IdentityManager,
	roles RoleAssigner,
	tx TxRunner,
	log *slog.Logger,
) (*Service, error) {
	if log == nil {
		log = slog.Default()
	}
	if cfg.Mode == "" {
		cfg.Mode = ModeToken
	}

	s := &Service{
		cfg:      cfg,
		identity: identity,
		roles:    roles,
		tx:       tx,
		log:      log,
	}

	if err := s.prepareToken(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Service) prepareToken(ctx context.Context) error {
	if s.cfg.Mode != ModeToken {
		return nil
	}
	count, err := s.identity.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("check initialization state: %w", err)
	}
	if count > 0 {
		return nil
	}

	token := strings.TrimSpace(s.cfg.Token)
	if token == "" {
		generated, err := security.GenerateOpaqueToken(security.DefaultOpaqueTokenBytes)
		if err != nil {
			return fmt.Errorf("generate one-time setup token: %w", err)
		}
		token = generated
		s.log.Info("system is uninitialized; use this one-time setup token to complete /setup",
			"setup_token", token,
		)
	}
	s.oneToken = token
	return nil
}

// Status reports whether the system has been initialized and whether a setup token is required.
func (s *Service) Status(ctx context.Context) (Status, error) {
	count, err := s.identity.CountUsers(ctx)
	if err != nil {
		return Status{}, err
	}
	initialized := count > 0
	return Status{
		Initialized:   initialized,
		Mode:          s.cfg.Mode,
		RequiresToken: !initialized && s.cfg.Mode == ModeToken,
	}, nil
}

// Init validates the setup protection gate and atomically creates the first super administrator.
func (s *Service) Init(ctx context.Context, input InitInput) (CreatedAdmin, error) {
	// Serialize in-process attempts so concurrent unit/single-instance calls do not race before advisory lock.
	s.mu.Lock()
	defer s.mu.Unlock()

	var created CreatedAdmin
	err := s.withinTx(ctx, func(txCtx context.Context) error {
		if err := database.AcquireAdvisoryXactLock(txCtx, setupAdvisoryLockKey); err != nil {
			return apperr.Internal(err)
		}

		count, err := s.identity.CountUsers(txCtx)
		if err != nil {
			return err
		}
		if count > 0 {
			return alreadyInitializedError()
		}

		if err := s.checkProtection(input); err != nil {
			return err
		}

		admin, err := s.identity.CreateInitialAdmin(txCtx, CreateAdminParams{
			Username:    input.Username,
			Email:       input.Email,
			DisplayName: input.DisplayName,
			Password:    input.Password,
		})
		if err != nil {
			return err
		}

		if err := s.roles.AssignRoleByCode(txCtx, admin.ID, superAdminRoleCode); err != nil {
			return err
		}

		created = admin
		return nil
	})
	if err != nil {
		return CreatedAdmin{}, err
	}

	// Scrub one-time token from memory immediately after successful initialization.
	s.oneToken = ""
	s.log.Info("initial super administrator created",
		"user_id", created.ID.String(),
		"username", created.Username,
	)
	return created, nil
}

func (s *Service) checkProtection(input InitInput) error {
	switch s.cfg.Mode {
	case ModeToken:
		submitted := strings.TrimSpace(input.SetupToken)
		if s.oneToken == "" || submitted == "" || !security.ConstantTimeEqual(submitted, s.oneToken) {
			return tokenInvalidError()
		}
		return nil
	case ModeLocal:
		if !isLocalOrPrivateIP(input.ClientIP) {
			return localOnlyError()
		}
		return nil
	case ModeOpen:
		if !s.cfg.AllowOpen {
			return localOnlyError()
		}
		return nil
	default:
		return tokenInvalidError()
	}
}

func (s *Service) withinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if s.tx == nil {
		return fn(ctx)
	}
	return s.tx.WithinTx(ctx, fn)
}

func isLocalOrPrivateIP(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	addr, err := netip.ParseAddr(trimmed)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast()
}
