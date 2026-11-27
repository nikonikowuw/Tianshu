package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/platform/cache"
	"tianshu/internal/platform/config"
	"tianshu/internal/platform/security"
	"tianshu/internal/shared/apperr"
)

const (
	userStatusActive         = "active"
	userStatusDisabled       = "disabled"
	refreshGraceWindow       = 45 * time.Second
	maxLoginFailures         = 10
	loginCooldownDuration    = 15 * time.Minute
	loginFailWindowDuration  = 15 * time.Minute
	defaultSessionPurgeBatch = int32(5000)
)

type loginFailureState struct {
	Count        int       `json:"count"`
	BlockedUntil time.Time `json:"blockedUntil"`
}

// Service implements authentication, session lifecycle, token rotation, replay detection, and soft cooldown.
type Service struct {
	cfg            config.AuthConfig
	store          store
	identity       IdentityProvider
	access         AccessProvider
	hasher         PasswordVerifier
	jwt            *security.JWTManager
	tx             TxRunner
	cache          cache.Cache
	audit          AuditRecorder
	avatarSigner   AvatarURLSigner
	log            *slog.Logger
	now            func() time.Time
	purgeBatchSize int32
	failMu         sync.Mutex
}

// NewService creates a PostgreSQL-backed auth Service.
func NewService(cfg config.AuthConfig, pool *pgxpool.Pool, deps ServiceDeps) *Service {
	return newServiceWithStore(cfg, newStore(pool), deps)
}

func newServiceWithStore(cfg config.AuthConfig, s store, deps ServiceDeps) *Service {
	log := deps.Log
	if log == nil {
		log = slog.Default()
	}
	if cfg.RefreshTokenTTL <= 0 {
		cfg.RefreshTokenTTL = 7 * 24 * time.Hour
	}
	return &Service{
		cfg:            cfg,
		store:          s,
		identity:       deps.Identity,
		access:         deps.Access,
		hasher:         deps.Hasher,
		jwt:            deps.JWT,
		tx:             deps.Tx,
		cache:          deps.Cache,
		audit:          deps.Audit,
		avatarSigner:   deps.AvatarSigner,
		log:            log,
		now:            time.Now,
		purgeBatchSize: defaultSessionPurgeBatch,
	}
}

// JWTManager exposes the configured HS256 verifier for auth middleware wiring.
func (s *Service) JWTManager() *security.JWTManager {
	return s.jwt
}

// RefreshTokenTTL returns the configured refresh token cookie lifetime.
func (s *Service) RefreshTokenTTL() time.Duration {
	return s.cfg.RefreshTokenTTL
}

// CookieSecure reports whether the refresh cookie must set the Secure attribute.
func (s *Service) CookieSecure() bool {
	return s.cfg.CookieSecure
}

// LoginRateLimit returns the per-IP per-minute login rate limit (defaults to 5).
func (s *Service) LoginRateLimit() int {
	if s.cfg.LoginRateLimit < 1 {
		return 5
	}
	return s.cfg.LoginRateLimit
}

// Login authenticates a user by username or email, enforces soft cooldown, creates a session, and issues tokens.
func (s *Service) Login(ctx context.Context, input LoginInput) (LoginResult, error) {
	identifier := strings.ToLower(strings.TrimSpace(input.Identifier))
	if identifier == "" || input.Password == "" {
		return LoginResult{}, invalidCredentialsError()
	}

	if retryAfter := s.checkLoginCooldown(ctx, input.ClientIP, identifier); retryAfter > 0 {
		s.recordAuditBestEffort(ctx, AuditEntry{
			ActorUsername: identifier,
			Action:        "auth.login",
			Resource:      "session",
			Status:        "failure",
			IPAddress:     input.ClientIP,
			UserAgent:     input.UserAgent,
			Metadata: map[string]any{
				"reason":     "rate_limited",
				"retryAfter": retryAfter,
			},
		})
		return LoginResult{}, apperr.RateLimited().WithParams(map[string]any{"retry_after": retryAfter})
	}

	creds, err := s.identity.GetAuthCredentials(ctx, identifier)
	if err != nil {
		if appErr, ok := apperr.As(err); ok && appErr.Status == 404 {
			s.hasher.VerifyDummy(input.Password)
			s.recordLoginFailure(ctx, input.ClientIP, identifier)
			s.recordAuditBestEffort(ctx, AuditEntry{
				ActorUsername: identifier,
				Action:        "auth.login",
				Resource:      "session",
				Status:        "failure",
				IPAddress:     input.ClientIP,
				UserAgent:     input.UserAgent,
				Metadata:      map[string]any{"reason": "invalid_credentials"},
			})
			return LoginResult{}, invalidCredentialsError()
		}
		return LoginResult{}, err
	}

	matched, needsRehash, err := s.hasher.Verify(input.Password, creds.PasswordHash)
	if err != nil {
		s.log.Error("stored password hash invalid", "user_id", creds.User.ID, "error", err)
		s.recordAuditBestEffort(ctx, AuditEntry{
			ActorID:       &creds.User.ID,
			ActorUsername: creds.User.Username,
			Action:        "auth.login",
			Resource:      "session",
			Status:        "failure",
			IPAddress:     input.ClientIP,
			UserAgent:     input.UserAgent,
			Metadata:      map[string]any{"reason": "invalid_credentials"},
		})
		return LoginResult{}, invalidCredentialsError()
	}
	if !matched {
		s.recordLoginFailure(ctx, input.ClientIP, identifier)
		s.recordAuditBestEffort(ctx, AuditEntry{
			ActorID:       &creds.User.ID,
			ActorUsername: creds.User.Username,
			Action:        "auth.login",
			Resource:      "session",
			Status:        "failure",
			IPAddress:     input.ClientIP,
			UserAgent:     input.UserAgent,
			Metadata:      map[string]any{"reason": "invalid_credentials"},
		})
		return LoginResult{}, invalidCredentialsError()
	}

	if creds.User.Status == userStatusDisabled {
		s.recordAuditBestEffort(ctx, AuditEntry{
			ActorID:       &creds.User.ID,
			ActorUsername: creds.User.Username,
			Action:        "auth.login",
			Resource:      "session",
			Status:        "failure",
			IPAddress:     input.ClientIP,
			UserAgent:     input.UserAgent,
			Metadata:      map[string]any{"reason": "account_disabled"},
		})
		return LoginResult{}, accountDisabledError()
	}

	s.clearLoginFailures(ctx, input.ClientIP, identifier)

	if needsRehash {
		if upgraded, hashErr := s.hasher.Hash(input.Password); hashErr == nil {
			if upErr := s.identity.UpdatePasswordHash(ctx, creds.User.ID, upgraded); upErr != nil {
				s.log.Warn("transparent password rehash failed", "user_id", creds.User.ID, "error", upErr)
			}
		}
	}

	sessionID, err := uuid.NewV7()
	if err != nil {
		return LoginResult{}, apperr.Internal(fmt.Errorf("generate session uuid: %w", err))
	}
	rawRefresh, err := security.GenerateOpaqueToken(security.DefaultOpaqueTokenBytes)
	if err != nil {
		return LoginResult{}, apperr.Internal(err)
	}
	refreshHash := security.HashTokenSHA256(rawRefresh)
	now := s.now().UTC()

	err = s.withinTx(ctx, func(txCtx context.Context) error {
		if createErr := s.store.CreateSession(txCtx, sessionRecord{
			ID:               sessionID,
			UserID:           creds.User.ID,
			RefreshTokenHash: refreshHash,
			UserAgent:        truncateUA(input.UserAgent),
			IPAddress:        input.ClientIP,
			ExpiresAt:        now.Add(s.cfg.RefreshTokenTTL),
		}); createErr != nil {
			return apperr.Internal(fmt.Errorf("create session: %w", createErr))
		}
		if recErr := s.identity.RecordLogin(txCtx, creds.User.ID); recErr != nil {
			return recErr
		}
		if s.audit != nil {
			userID := creds.User.ID
			if auditErr := s.audit.Record(txCtx, AuditEntry{
				ActorID:       &userID,
				ActorUsername: creds.User.Username,
				Action:        "auth.login",
				Resource:      "session",
				ResourceID:    sessionID.String(),
				Status:        "success",
				IPAddress:     input.ClientIP,
				UserAgent:     input.UserAgent,
				Metadata:      map[string]any{"identifier": identifier},
			}); auditErr != nil {
				return auditErr
			}
		}
		return nil
	})
	if err != nil {
		return LoginResult{}, err
	}

	accessToken, expiresAt, err := s.jwt.Sign(creds.User.ID, sessionID)
	if err != nil {
		return LoginResult{}, apperr.Internal(fmt.Errorf("sign access token: %w", err))
	}

	ua, err := s.access.GetUserAccess(ctx, creds.User.ID)
	if err != nil {
		return LoginResult{}, err
	}

	user := creds.User
	user.AvatarURL = s.resolveAvatarURL(ctx, user.AvatarFileID)

	return LoginResult{
		AccessToken:  accessToken,
		RefreshToken: rawRefresh,
		ExpiresAt:    expiresAt,
		User:         user,
		Roles:        nonNilStrings(ua.Roles),
		Permissions:  nonNilStrings(ua.Permissions),
	}, nil
}

// Refresh rotates the refresh token, detects replay via refresh_token_history, and allows concurrent grace-window refreshes.
func (s *Service) Refresh(ctx context.Context, input RefreshInput) (RefreshResult, error) {
	rawToken := strings.TrimSpace(input.RefreshToken)
	if rawToken == "" {
		return RefreshResult{}, apperr.Unauthorized(apperr.CodeUnauthorized)
	}
	tokenHash := security.HashTokenSHA256(rawToken)
	now := s.now().UTC()

	var (
		result       RefreshResult
		authErr      error
		pendingAudit *AuditEntry
	)
	err := s.withinTx(ctx, func(txCtx context.Context) error {
		sess, err := s.store.GetSessionByRefreshHashForUpdate(txCtx, tokenHash)
		if err == nil {
			authErr, pendingAudit, err = s.rotateActiveSession(txCtx, sess, tokenHash, input, now, &result)
			return err
		}
		if !errors.Is(err, errSessionNotFound) {
			return apperr.Internal(fmt.Errorf("lookup session by refresh hash: %w", err))
		}

		hist, histErr := s.store.GetRefreshTokenHistoryByHash(txCtx, tokenHash)
		if errors.Is(histErr, errHistoryNotFound) {
			authErr = apperr.Unauthorized(apperr.CodeUnauthorized)
			return nil
		}
		if histErr != nil {
			return apperr.Internal(fmt.Errorf("lookup refresh token history: %w", histErr))
		}

		authErr, pendingAudit, err = s.handleHistoricalToken(txCtx, hist, input, now, &result)
		return err
	})
	if err != nil {
		return RefreshResult{}, err
	}
	if pendingAudit != nil {
		s.recordAuditBestEffort(ctx, *pendingAudit)
	}
	if authErr != nil {
		return RefreshResult{}, authErr
	}
	return result, nil
}

// rotateActiveSession returns (businessAuthErr, pendingAudit, txErr).
// Security revocations (disabled/deleted user) return a non-nil businessAuthErr with nil txErr
// so the enclosing transaction commits the revocation instead of rolling it back;
// pendingAudit is emitted after commit so an audit failure never aborts the revocation tx.
func (s *Service) rotateActiveSession(
	txCtx context.Context,
	sess sessionRecord,
	oldHash string,
	input RefreshInput,
	now time.Time,
	out *RefreshResult,
) (error, *AuditEntry, error) {
	if sess.RevokedAt != nil || now.After(sess.ExpiresAt) {
		return apperr.Unauthorized(apperr.CodeUnauthorized), nil, nil
	}

	user, err := s.identity.GetByID(txCtx, sess.UserID)
	if err != nil {
		if appErr, ok := apperr.As(err); ok && appErr.Status == 404 {
			if _, _, revErr := s.store.RevokeSession(txCtx, sess.ID); revErr != nil {
				return nil, nil, apperr.Internal(fmt.Errorf("revoke deleted user session: %w", revErr))
			}
			return apperr.Unauthorized(apperr.CodeUnauthorized), nil, nil
		}
		return nil, nil, err
	}
	if user.Status == userStatusDisabled {
		if revErr := s.store.RevokeAllUserSessions(txCtx, sess.UserID); revErr != nil {
			return nil, nil, apperr.Internal(fmt.Errorf("revoke disabled user sessions: %w", revErr))
		}
		userID := user.ID
		return accountDisabledError(), &AuditEntry{
			ActorID:       &userID,
			ActorUsername: user.Username,
			Action:        "auth.refresh",
			Resource:      "session",
			ResourceID:    sess.ID.String(),
			Status:        "failure",
			IPAddress:     input.ClientIP,
			UserAgent:     input.UserAgent,
			Metadata:      map[string]any{"reason": "account_disabled"},
		}, nil
	}

	newRaw, err := security.GenerateOpaqueToken(security.DefaultOpaqueTokenBytes)
	if err != nil {
		return nil, nil, apperr.Internal(err)
	}
	newHash := security.HashTokenSHA256(newRaw)
	newExp := now.Add(s.cfg.RefreshTokenTTL)

	if err := s.store.InsertRefreshTokenHistory(txCtx, oldHash, sess.ID, sess.ExpiresAt); err != nil {
		return nil, nil, apperr.Internal(fmt.Errorf("record refresh token history: %w", err))
	}
	if err := s.store.RotateSessionRefreshToken(
		txCtx,
		sess.ID,
		newHash,
		truncateUA(input.UserAgent),
		input.ClientIP,
		newExp,
	); err != nil {
		return nil, nil, apperr.Internal(fmt.Errorf("rotate session refresh token: %w", err))
	}

	accessToken, accessExp, err := s.jwt.Sign(sess.UserID, sess.ID)
	if err != nil {
		return nil, nil, apperr.Internal(fmt.Errorf("sign refreshed access token: %w", err))
	}

	*out = RefreshResult{
		AccessToken:  accessToken,
		RefreshToken: newRaw,
		RotateCookie: true,
		ExpiresAt:    accessExp,
	}
	return nil, nil, nil
}

// handleHistoricalToken returns (businessAuthErr, pendingAudit, txErr) so replay revocations commit cleanly.
func (s *Service) handleHistoricalToken(
	txCtx context.Context,
	hist refreshHistoryRecord,
	input RefreshInput,
	now time.Time,
	out *RefreshResult,
) (error, *AuditEntry, error) {
	sess, err := s.store.GetSessionByIDForUpdate(txCtx, hist.SessionID)
	if errors.Is(err, errSessionNotFound) {
		return apperr.Unauthorized(apperr.CodeUnauthorized), nil, nil
	}
	if err != nil {
		return nil, nil, apperr.Internal(fmt.Errorf("lookup historical session: %w", err))
	}
	if sess.RevokedAt != nil || now.After(sess.ExpiresAt) {
		return refreshReusedError(), nil, nil
	}

	// Outside concurrent refresh grace window: treat as token theft/replay and revoke entire session.
	if now.Sub(hist.ReplacedAt) > refreshGraceWindow {
		if _, _, revErr := s.store.RevokeSession(txCtx, sess.ID); revErr != nil {
			return nil, nil, apperr.Internal(fmt.Errorf("revoke replayed session: %w", revErr))
		}
		actorUsername := s.lookupUsernameBestEffort(txCtx, sess.UserID)
		userID := sess.UserID
		s.log.Warn("refresh token replay detected; session revoked",
			"session_id", sess.ID.String(),
			"user_id", sess.UserID.String(),
		)
		return refreshReusedError(), &AuditEntry{
			ActorID:       &userID,
			ActorUsername: actorUsername,
			Action:        "auth.refresh_replay",
			Resource:      "session",
			ResourceID:    sess.ID.String(),
			Status:        "failure",
			IPAddress:     input.ClientIP,
			UserAgent:     input.UserAgent,
			Metadata: map[string]any{
				"reason":    "refresh_token_reused",
				"sessionId": sess.ID.String(),
			},
		}, nil
	}

	user, err := s.identity.GetByID(txCtx, sess.UserID)
	if err != nil {
		return apperr.Unauthorized(apperr.CodeUnauthorized), nil, nil
	}
	if user.Status == userStatusDisabled {
		if revErr := s.store.RevokeAllUserSessions(txCtx, sess.UserID); revErr != nil {
			return nil, nil, apperr.Internal(fmt.Errorf("revoke disabled user sessions: %w", revErr))
		}
		userID := user.ID
		return accountDisabledError(), &AuditEntry{
			ActorID:       &userID,
			ActorUsername: user.Username,
			Action:        "auth.refresh",
			Resource:      "session",
			ResourceID:    sess.ID.String(),
			Status:        "failure",
			IPAddress:     input.ClientIP,
			UserAgent:     input.UserAgent,
			Metadata:      map[string]any{"reason": "account_disabled"},
		}, nil
	}

	accessToken, accessExp, err := s.jwt.Sign(sess.UserID, sess.ID)
	if err != nil {
		return nil, nil, apperr.Internal(fmt.Errorf("sign grace-window access token: %w", err))
	}

	*out = RefreshResult{
		AccessToken:  accessToken,
		RotateCookie: false,
		ExpiresAt:    accessExp,
	}
	return nil, nil, nil
}

// Logout revokes the current session identified by refresh token or session ID and writes an audit entry.
func (s *Service) Logout(ctx context.Context, input LogoutInput) error {
	var (
		revokedSessID uuid.UUID
		actorID       = input.UserID
		anyRevoked    bool
	)

	if trimmed := strings.TrimSpace(input.RefreshToken); trimmed != "" {
		hash := security.HashTokenSHA256(trimmed)
		rev, ok, err := s.store.RevokeSessionByRefreshHash(ctx, hash)
		if err != nil {
			return apperr.Internal(fmt.Errorf("revoke session by token: %w", err))
		}
		if ok {
			anyRevoked = true
			revokedSessID = rev.ID
			if actorID == uuid.Nil {
				actorID = rev.UserID
			}
		}
	}
	if input.SessionID != uuid.Nil {
		rev, ok, err := s.store.RevokeSession(ctx, input.SessionID)
		if err != nil {
			return apperr.Internal(fmt.Errorf("revoke session by id: %w", err))
		}
		if ok {
			anyRevoked = true
			revokedSessID = rev.ID
			if actorID == uuid.Nil {
				actorID = rev.UserID
			}
		}
	}

	if anyRevoked || actorID != uuid.Nil {
		var actorPtr *uuid.UUID
		var actorUsername string
		if actorID != uuid.Nil {
			idCopy := actorID
			actorPtr = &idCopy
			actorUsername = s.lookupUsernameBestEffort(ctx, actorID)
		}
		resourceID := ""
		if revokedSessID != uuid.Nil {
			resourceID = revokedSessID.String()
		} else if input.SessionID != uuid.Nil {
			resourceID = input.SessionID.String()
		}
		s.recordAuditBestEffort(ctx, AuditEntry{
			ActorID:       actorPtr,
			ActorUsername: actorUsername,
			Action:        "auth.logout",
			Resource:      "session",
			ResourceID:    resourceID,
			Status:        "success",
			IPAddress:     input.ClientIP,
			UserAgent:     input.UserAgent,
		})
	}

	return nil
}

func (s *Service) lookupUsernameBestEffort(ctx context.Context, userID uuid.UUID) string {
	if userID == uuid.Nil {
		return ""
	}
	u, err := s.identity.GetByID(ctx, userID)
	if err != nil {
		return ""
	}
	return u.Username
}

func (s *Service) recordAuditBestEffort(ctx context.Context, entry AuditEntry) {
	if s.audit == nil {
		return
	}
	if err := s.audit.Record(ctx, entry); err != nil {
		s.log.Warn("record auth audit log failed", "action", entry.Action, "error", err)
	}
}

// Me returns the authenticated user's profile together with fresh roles and permissions.
func (s *Service) Me(ctx context.Context, userID uuid.UUID) (MeResult, error) {
	user, err := s.identity.GetByID(ctx, userID)
	if err != nil {
		if appErr, ok := apperr.As(err); ok && appErr.Status == 404 {
			return MeResult{}, apperr.Unauthorized(apperr.CodeUnauthorized)
		}
		return MeResult{}, err
	}
	if user.Status == userStatusDisabled {
		return MeResult{}, accountDisabledError()
	}

	ua, err := s.access.GetUserAccess(ctx, userID)
	if err != nil {
		return MeResult{}, err
	}

	user.AvatarURL = s.resolveAvatarURL(ctx, user.AvatarFileID)

	return MeResult{
		User:        user,
		Roles:       nonNilStrings(ua.Roles),
		Permissions: nonNilStrings(ua.Permissions),
	}, nil
}

// RevokeAllUserSessions revokes every active session of userID and participates in the caller's tx via ctx.
func (s *Service) RevokeAllUserSessions(ctx context.Context, userID uuid.UUID) error {
	if err := s.store.RevokeAllUserSessions(ctx, userID); err != nil {
		return apperr.Internal(fmt.Errorf("revoke all user sessions for %s: %w", userID, err))
	}
	return nil
}

// ListActiveUserSessions returns non-revoked, non-expired sessions owned by userID.
func (s *Service) ListActiveUserSessions(ctx context.Context, userID uuid.UUID) ([]ActiveSession, error) {
	items, err := s.store.ListActiveUserSessions(ctx, userID)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("list active user sessions for %s: %w", userID, err))
	}
	if items == nil {
		return make([]ActiveSession, 0), nil
	}
	return items, nil
}

// IsSessionActive reports whether sessionID belongs to userID and is neither revoked nor expired.
func (s *Service) IsSessionActive(ctx context.Context, userID, sessionID uuid.UUID) (bool, error) {
	if userID == uuid.Nil || sessionID == uuid.Nil {
		return false, nil
	}
	active, err := s.store.IsUserSessionActive(ctx, userID, sessionID)
	if err != nil {
		return false, apperr.Internal(fmt.Errorf("check active session %s for %s: %w", sessionID, userID, err))
	}
	return active, nil
}

// LockActiveSessionForShare locks sessionID FOR SHARE within an active transaction and reports whether it is active.
func (s *Service) LockActiveSessionForShare(ctx context.Context, userID, sessionID uuid.UUID) (bool, error) {
	if userID == uuid.Nil || sessionID == uuid.Nil {
		return false, nil
	}
	active, err := s.store.LockActiveUserSessionForShare(ctx, userID, sessionID)
	if err != nil {
		return false, apperr.Internal(fmt.Errorf("lock active session %s for %s: %w", sessionID, userID, err))
	}
	return active, nil
}

// RevokeUserSession revokes a single active session owned by userID and reports whether a row was revoked.
func (s *Service) RevokeUserSession(ctx context.Context, userID, sessionID uuid.UUID) (bool, error) {
	revoked, err := s.store.RevokeUserSessionByID(ctx, userID, sessionID)
	if err != nil {
		return false, apperr.Internal(fmt.Errorf("revoke session %s for %s: %w", sessionID, userID, err))
	}
	return revoked, nil
}

// RevokeOtherUserSessions revokes all active sessions owned by userID except keepSessionID.
func (s *Service) RevokeOtherUserSessions(ctx context.Context, userID, keepSessionID uuid.UUID) (int64, error) {
	revoked, err := s.store.RevokeOtherUserSessions(ctx, userID, keepSessionID)
	if err != nil {
		return 0, apperr.Internal(fmt.Errorf("revoke other sessions for %s: %w", userID, err))
	}
	return revoked, nil
}

// PurgeExpiredSessions deletes refresh token history and user sessions that expired earlier than retention ago.
// Keeping revoked sessions until their original expires_at preserves replay detection for the full token lifetime.
// A non-positive retention disables cleanup and returns 0, nil.
func (s *Service) PurgeExpiredSessions(ctx context.Context, retention time.Duration) (int64, error) {
	if retention <= 0 {
		return 0, nil
	}
	batchSize := s.purgeBatchSize
	if batchSize <= 0 {
		batchSize = defaultSessionPurgeBatch
	}
	cutoff := s.now().UTC().Add(-retention)

	var totalDeleted int64
	for {
		if err := ctx.Err(); err != nil {
			return totalDeleted, err
		}
		deleted, err := s.store.DeleteExpiredRefreshTokenHistoryBatch(ctx, cutoff, batchSize)
		if err != nil {
			return totalDeleted, fmt.Errorf("delete expired refresh token history batch: %w", err)
		}
		totalDeleted += deleted
		if deleted < int64(batchSize) {
			break
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return totalDeleted, err
		}
		deleted, err := s.store.DeleteExpiredUserSessionsBatch(ctx, cutoff, batchSize)
		if err != nil {
			return totalDeleted, fmt.Errorf("delete expired user sessions batch: %w", err)
		}
		totalDeleted += deleted
		if deleted < int64(batchSize) {
			break
		}
	}
	return totalDeleted, nil
}

func (s *Service) resolveAvatarURL(ctx context.Context, fileID *uuid.UUID) *string {
	if s.avatarSigner == nil || fileID == nil || *fileID == uuid.Nil {
		return nil
	}
	url, err := s.avatarSigner.SignAvatarURL(ctx, *fileID)
	if err != nil {
		s.log.Warn("sign avatar url failed", "file_id", fileID.String(), "error", err)
		return nil
	}
	if strings.TrimSpace(url) == "" {
		return nil
	}
	return &url
}

func (s *Service) checkLoginCooldown(ctx context.Context, ip, identifier string) int {
	if s.cache == nil {
		return 0
	}
	now := s.now().UTC()
	var maxRetry int
	for _, key := range loginFailKeys(ip, identifier) {
		raw, err := s.cache.Get(ctx, key)
		if err != nil {
			continue
		}
		var st loginFailureState
		if json.Unmarshal(raw, &st) != nil {
			continue
		}
		if now.Before(st.BlockedUntil) {
			secs := max(int(math.Ceil(st.BlockedUntil.Sub(now).Seconds())), 1)
			if secs > maxRetry {
				maxRetry = secs
			}
		}
	}
	return maxRetry
}

func (s *Service) recordLoginFailure(ctx context.Context, ip, identifier string) {
	if s.cache == nil {
		return
	}
	s.failMu.Lock()
	defer s.failMu.Unlock()

	now := s.now().UTC()
	for _, key := range loginFailKeys(ip, identifier) {
		var st loginFailureState
		if raw, err := s.cache.Get(ctx, key); err == nil {
			_ = json.Unmarshal(raw, &st)
		}
		st.Count++
		ttl := loginFailWindowDuration
		if st.Count >= maxLoginFailures {
			st.BlockedUntil = now.Add(loginCooldownDuration)
			ttl = loginCooldownDuration
		}
		if encoded, err := json.Marshal(st); err == nil {
			_ = s.cache.Set(ctx, key, encoded, ttl)
		}
	}
}

func (s *Service) clearLoginFailures(ctx context.Context, ip, identifier string) {
	if s.cache == nil {
		return
	}
	s.failMu.Lock()
	defer s.failMu.Unlock()
	_ = s.cache.Delete(ctx, loginFailKeys(ip, identifier)...)
}

func loginFailKeys(ip, identifier string) []string {
	keys := []string{"auth:fail:acct:" + identifier}
	if ip != "" {
		keys = append(keys, "auth:fail:ipacct:"+ip+":"+identifier)
	}
	return keys
}

func (s *Service) withinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if s.tx == nil {
		return fn(ctx)
	}
	return s.tx.WithinTx(ctx, fn)
}

func truncateUA(ua string) string {
	if len(ua) > 512 {
		return ua[:512]
	}
	return ua
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return make([]string, 0)
	}
	return in
}
