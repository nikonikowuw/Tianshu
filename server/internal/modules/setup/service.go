package setup

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"tianshu/internal/platform/config"
	"tianshu/internal/platform/database"
	"tianshu/internal/shared/apperr"
)

// setupAdvisoryLockKey is a fixed application-scoped PostgreSQL advisory lock ID
// used to serialize concurrent initial setup submissions across instances.
const setupAdvisoryLockKey int64 = 0x7469616e73687501

const superAdminRoleCode = "super_admin"

// Service orchestrates one-time initial super administrator creation (via web wizard or env bootstrap).
type Service struct {
	identity IdentityManager
	roles    RoleAssigner
	tx       TxRunner
	audit    AuditRecorder
	log      *slog.Logger

	mu sync.Mutex
}

// NewService creates a setup Service.
func NewService(
	identity IdentityManager,
	roles RoleAssigner,
	tx TxRunner,
	audit AuditRecorder,
	log *slog.Logger,
) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		identity: identity,
		roles:    roles,
		tx:       tx,
		audit:    audit,
		log:      log,
	}
}

// Status reports whether the system has completed initial administrator setup.
func (s *Service) Status(ctx context.Context) (Status, error) {
	count, err := s.identity.CountUsers(ctx)
	if err != nil {
		return Status{}, err
	}
	return Status{Initialized: count > 0}, nil
}

// BootstrapFromEnv creates the initial super administrator automatically on startup when
// BootstrapConfig credentials are configured and the database has no active users yet.
// If the system is already initialized, it is a safe no-op.
func (s *Service) BootstrapFromEnv(ctx context.Context, cfg config.BootstrapConfig) error {
	if !cfg.Enabled() {
		return nil
	}

	displayName := strings.TrimSpace(cfg.AdminDisplayName)
	if displayName == "" {
		displayName = strings.TrimSpace(cfg.AdminUsername)
	}

	_, err := s.Init(ctx, InitInput{
		Username:    cfg.AdminUsername,
		Email:       cfg.AdminEmail,
		DisplayName: displayName,
		Password:    cfg.AdminPassword,
		Method:      "env_bootstrap",
	})
	if err != nil {
		if appErr, ok := apperr.As(err); ok && appErr.Code == ErrCodeAlreadyInitialized {
			return nil
		}
		return err
	}
	s.log.Info("bootstrapped initial super administrator from configuration")
	return nil
}

// Init atomically creates the first super administrator when no active users exist.
func (s *Service) Init(ctx context.Context, input InitInput) (CreatedAdmin, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	method := strings.TrimSpace(input.Method)
	if method == "" {
		method = "wizard"
	}

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

		if s.audit != nil {
			adminID := admin.ID
			if err := s.audit.Record(txCtx, AuditEntry{
				ActorID:       &adminID,
				ActorUsername: admin.Username,
				Action:        "setup.init",
				Resource:      "system",
				ResourceID:    admin.ID.String(),
				Status:        "success",
				IPAddress:     input.ClientIP,
				UserAgent:     input.UserAgent,
				Metadata: map[string]any{
					"method":   method,
					"username": admin.Username,
				},
			}); err != nil {
				return err
			}
		}

		created = admin
		return nil
	})
	if err != nil {
		if s.audit != nil && method != "env_bootstrap" {
			reason := "setup_failed"
			if appErr, ok := apperr.As(err); ok && appErr.Code != "" {
				reason = appErr.Code
			}
			s.recordBestEffort(ctx, AuditEntry{
				ActorUsername: strings.ToLower(strings.TrimSpace(input.Username)),
				Action:        "setup.init",
				Resource:      "system",
				Status:        "failure",
				IPAddress:     input.ClientIP,
				UserAgent:     input.UserAgent,
				Metadata: map[string]any{
					"method": method,
					"reason": reason,
				},
			})
		}
		return CreatedAdmin{}, err
	}

	s.log.Info("initial super administrator created",
		"user_id", created.ID.String(),
		"username", created.Username,
	)
	return created, nil
}

func (s *Service) recordBestEffort(ctx context.Context, entry AuditEntry) {
	if s.audit == nil {
		return
	}
	if err := s.audit.Record(ctx, entry); err != nil {
		s.log.Warn("record setup audit log failed", "action", entry.Action, "error", err)
	}
}

func (s *Service) withinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if s.tx == nil {
		return fn(ctx)
	}
	return s.tx.WithinTx(ctx, fn)
}
