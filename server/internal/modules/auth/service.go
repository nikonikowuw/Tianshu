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
	userStatusActive        = "active"
	userStatusDisabled      = "disabled"
	refreshGraceWindow      = 45 * time.Second
	maxLoginFailures        = 10
	loginCooldownDuration   = 15 * time.Minute
	loginFailWindowDuration = 15 * time.Minute
)

type loginFailureState struct {
	Count        int       `json:"count"`
	BlockedUntil time.Time `json:"blockedUntil"`
}

// Service implements authentication, session lifecycle, token rotation, replay detection, and soft cooldown.
type Service struct {
	cfg      config.AuthConfig
	store    store
	identity IdentityProvider
	access   AccessProvider
	hasher   PasswordVerifier
	jwt      *security.JWTManager
	tx       TxRunner
	cache    cache.Cache
	log      *slog.Logger
	now      func() time.Time
	failMu   sync.Mutex
}

// NewService creates a PostgreSQL-backed auth Service.
func NewService(
	cfg config.AuthConfig,
	pool *pgxpool.Pool,
	identity IdentityProvider,
	access AccessProvider,
	hasher PasswordVerifier,
	jwtMgr *security.JWTManager,
	tx TxRunner,
	c cache.Cache,
	log *slog.Logger,
) *Service {
	return newServiceWithStore(cfg, newStore(pool), identity, access, hasher, jwtMgr, tx, c, log)
}

func newServiceWithStore(
	cfg config.AuthConfig,
	s store,
	identity IdentityProvider,
	access AccessProvider,
	hasher PasswordVerifier,
	jwtMgr *security.JWTManager,
	tx TxRunner,
	c cache.Cache,
	log *slog.Logger,
) *Service {
	if log == nil {
		log = slog.Default()
	}
	if cfg.RefreshTokenTTL <= 0 {
		cfg.RefreshTokenTTL = 7 * 24 * time.Hour
	}
	return &Service{
		cfg:      cfg,
		store:    s,
		identity: identity,
		access:   access,
		hasher:   hasher,
		jwt:      jwtMgr,
		tx:       tx,
		cache:    c,
		log:      log,
		now:      time.Now,
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

// Login authenticates a user by username or email, enforces soft cooldown, creates a session, and issues tokens.
func (s *Service) Login(ctx context.Context, input LoginInput) (LoginResult, error) {
	identifier := strings.ToLower(strings.TrimSpace(input.Identifier))
	if identifier == "" || input.Password == "" {
		return LoginResult{}, invalidCredentialsError()
	}

	if retryAfter := s.checkLoginCooldown(ctx, input.ClientIP, identifier); retryAfter > 0 {
		return LoginResult{}, apperr.RateLimited().WithParams(map[string]any{"retry_after": retryAfter})
	}

	creds, err := s.identity.GetAuthCredentials(ctx, identifier)
	if err != nil {
		if appErr, ok := apperr.As(err); ok && appErr.Status == 404 {
			s.hasher.VerifyDummy(input.Password)
			s.recordLoginFailure(ctx, input.ClientIP, identifier)
			return LoginResult{}, invalidCredentialsError()
		}
		return LoginResult{}, err
	}

	matched, needsRehash, err := s.hasher.Verify(input.Password, creds.PasswordHash)
	if err != nil {
		s.log.Error("stored password hash invalid", "user_id", creds.User.ID, "error", err)
		return LoginResult{}, invalidCredentialsError()
	}
	if !matched {
		s.recordLoginFailure(ctx, input.ClientIP, identifier)
		return LoginResult{}, invalidCredentialsError()
	}

	if creds.User.Status == userStatusDisabled {
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
		return s.identity.RecordLogin(txCtx, creds.User.ID)
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

	return LoginResult{
		AccessToken:  accessToken,
		RefreshToken: rawRefresh,
		ExpiresAt:    expiresAt,
		User:         creds.User,
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
		result  RefreshResult
		authErr error
	)
	err := s.withinTx(ctx, func(txCtx context.Context) error {
		sess, err := s.store.GetSessionByRefreshHashForUpdate(txCtx, tokenHash)
		if err == nil {
			authErr, err = s.rotateActiveSession(txCtx, sess, tokenHash, input, now, &result)
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

		authErr, err = s.handleHistoricalToken(txCtx, hist, now, &result)
		return err
	})
	if err != nil {
		return RefreshResult{}, err
	}
	if authErr != nil {
		return RefreshResult{}, authErr
	}
	return result, nil
}

// rotateActiveSession returns (businessAuthErr, txErr).
// Security revocations (disabled/deleted user) return a non-nil businessAuthErr with nil txErr
// so the enclosing transaction commits the revocation instead of rolling it back.
func (s *Service) rotateActiveSession(
	txCtx context.Context,
	sess sessionRecord,
	oldHash string,
	input RefreshInput,
	now time.Time,
	out *RefreshResult,
) (error, error) {
	if sess.RevokedAt != nil || now.After(sess.ExpiresAt) {
		return apperr.Unauthorized(apperr.CodeUnauthorized), nil
	}

	user, err := s.identity.GetByID(txCtx, sess.UserID)
	if err != nil {
		if appErr, ok := apperr.As(err); ok && appErr.Status == 404 {
			if revErr := s.store.RevokeSession(txCtx, sess.ID); revErr != nil {
				return nil, apperr.Internal(fmt.Errorf("revoke deleted user session: %w", revErr))
			}
			return apperr.Unauthorized(apperr.CodeUnauthorized), nil
		}
		return nil, err
	}
	if user.Status == userStatusDisabled {
		if revErr := s.store.RevokeAllUserSessions(txCtx, sess.UserID); revErr != nil {
			return nil, apperr.Internal(fmt.Errorf("revoke disabled user sessions: %w", revErr))
		}
		return accountDisabledError(), nil
	}

	newRaw, err := security.GenerateOpaqueToken(security.DefaultOpaqueTokenBytes)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	newHash := security.HashTokenSHA256(newRaw)
	newExp := now.Add(s.cfg.RefreshTokenTTL)

	if err := s.store.InsertRefreshTokenHistory(txCtx, oldHash, sess.ID, sess.ExpiresAt); err != nil {
		return nil, apperr.Internal(fmt.Errorf("record refresh token history: %w", err))
	}
	if err := s.store.RotateSessionRefreshToken(
		txCtx,
		sess.ID,
		newHash,
		truncateUA(input.UserAgent),
		input.ClientIP,
		newExp,
	); err != nil {
		return nil, apperr.Internal(fmt.Errorf("rotate session refresh token: %w", err))
	}

	accessToken, accessExp, err := s.jwt.Sign(sess.UserID, sess.ID)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("sign refreshed access token: %w", err))
	}

	*out = RefreshResult{
		AccessToken:  accessToken,
		RefreshToken: newRaw,
		RotateCookie: true,
		ExpiresAt:    accessExp,
	}
	return nil, nil
}

// handleHistoricalToken returns (businessAuthErr, txErr) so replay revocations commit cleanly.
func (s *Service) handleHistoricalToken(
	txCtx context.Context,
	hist refreshHistoryRecord,
	now time.Time,
	out *RefreshResult,
) (error, error) {
	sess, err := s.store.GetSessionByIDForUpdate(txCtx, hist.SessionID)
	if errors.Is(err, errSessionNotFound) {
		return apperr.Unauthorized(apperr.CodeUnauthorized), nil
	}
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("lookup historical session: %w", err))
	}
	if sess.RevokedAt != nil || now.After(sess.ExpiresAt) {
		return refreshReusedError(), nil
	}

	// Outside concurrent refresh grace window: treat as token theft/replay and revoke entire session.
	if now.Sub(hist.ReplacedAt) > refreshGraceWindow {
		if revErr := s.store.RevokeSession(txCtx, sess.ID); revErr != nil {
			return nil, apperr.Internal(fmt.Errorf("revoke replayed session: %w", revErr))
		}
		s.log.Warn("refresh token replay detected; session revoked",
			"session_id", sess.ID.String(),
			"user_id", sess.UserID.String(),
		)
		return refreshReusedError(), nil
	}

	user, err := s.identity.GetByID(txCtx, sess.UserID)
	if err != nil {
		return apperr.Unauthorized(apperr.CodeUnauthorized), nil
	}
	if user.Status == userStatusDisabled {
		if revErr := s.store.RevokeAllUserSessions(txCtx, sess.UserID); revErr != nil {
			return nil, apperr.Internal(fmt.Errorf("revoke disabled user sessions: %w", revErr))
		}
		return accountDisabledError(), nil
	}

	accessToken, accessExp, err := s.jwt.Sign(sess.UserID, sess.ID)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("sign grace-window access token: %w", err))
	}

	*out = RefreshResult{
		AccessToken:  accessToken,
		RotateCookie: false,
		ExpiresAt:    accessExp,
	}
	return nil, nil
}

// Logout revokes the current session identified by refresh token or session ID.
func (s *Service) Logout(ctx context.Context, rawRefreshToken string, sessionID uuid.UUID) error {
	if trimmed := strings.TrimSpace(rawRefreshToken); trimmed != "" {
		hash := security.HashTokenSHA256(trimmed)
		if err := s.store.RevokeSessionByRefreshHash(ctx, hash); err != nil {
			return apperr.Internal(fmt.Errorf("revoke session by token: %w", err))
		}
	}
	if sessionID != uuid.Nil {
		if err := s.store.RevokeSession(ctx, sessionID); err != nil {
			return apperr.Internal(fmt.Errorf("revoke session by id: %w", err))
		}
	}
	return nil
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

	return MeResult{
		User:        user,
		Roles:       nonNilStrings(ua.Roles),
		Permissions: nonNilStrings(ua.Permissions),
	}, nil
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
