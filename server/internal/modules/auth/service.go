package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/platform/cache"
	"tianshu/internal/platform/config"
	"tianshu/internal/platform/security"
	"tianshu/internal/shared/apperr"
)

const (
	userStatusActive              = "active"
	userStatusDisabled            = "disabled"
	refreshGraceWindow            = 45 * time.Second
	maxLoginFailures              = 10
	loginCooldownDuration         = 15 * time.Minute
	loginFailWindowDuration       = 15 * time.Minute
	maxLoginAccountRequestsPerMin = 10
	loginAccountRateWindow        = time.Minute
	accountRateSweepThreshold     = 4096
	accountRateMaxEntries         = 16384
	failLockShardCount            = 64
	maxIdentifierRunes            = 255
	maxUserAgentBytes             = 512
	defaultSessionPurgeBatch      = int32(5000)
)

type loginFailureState struct {
	Count        int       `json:"count"`
	BlockedUntil time.Time `json:"blockedUntil"`
}

type accountRateBucket struct {
	count   int
	resetAt time.Time
}

type accountRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]accountRateBucket
	limit   int
	window  time.Duration
}

func newAccountRateLimiter(limit int, window time.Duration) *accountRateLimiter {
	return &accountRateLimiter{
		buckets: make(map[string]accountRateBucket),
		limit:   limit,
		window:  window,
	}
}

func (l *accountRateLimiter) peek(key string, now time.Time) int {
	if l == nil || key == "" {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	bucket, ok := l.buckets[key]
	if !ok || !now.Before(bucket.resetAt) {
		return 0
	}
	if bucket.count < l.limit {
		return 0
	}
	return max(int(math.Ceil(bucket.resetAt.Sub(now).Seconds())), 1)
}

func (l *accountRateLimiter) allow(key string, now time.Time) (bool, int) {
	if l == nil || key == "" {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.buckets) >= accountRateSweepThreshold {
		for k, b := range l.buckets {
			if !now.Before(b.resetAt) {
				delete(l.buckets, k)
			}
		}
	}

	bucket, ok := l.buckets[key]
	if !ok || !now.Before(bucket.resetAt) {
		if !ok && len(l.buckets) >= accountRateMaxEntries {
			// Drop one bucket to bound memory under high-cardinality identifier spraying.
			for k := range l.buckets {
				delete(l.buckets, k)
				break
			}
		}
		l.buckets[key] = accountRateBucket{
			count:   1,
			resetAt: now.Add(l.window),
		}
		return true, 0
	}

	if bucket.count >= l.limit {
		return false, max(int(math.Ceil(bucket.resetAt.Sub(now).Seconds())), 1)
	}

	bucket.count++
	l.buckets[key] = bucket
	return true, 0
}

type refreshOutcome struct {
	result       RefreshResult
	authErr      error
	pendingAudit *AuditEntry
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
	failLocks      [failLockShardCount]sync.Mutex
	accountRateLim *accountRateLimiter
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
		accountRateLim: newAccountRateLimiter(maxLoginAccountRequestsPerMin, loginAccountRateWindow),
	}
}

func (s *Service) refreshTokenTTL() time.Duration {
	return s.cfg.RefreshTokenTTL
}

func (s *Service) cookieSecure() bool {
	return s.cfg.CookieSecure
}

func (s *Service) loginRateLimit() int {
	if s.cfg.LoginRateLimit < 1 {
		return 5
	}
	return s.cfg.LoginRateLimit
}

func validateLoginInput(identifier, password string) []apperr.FieldError {
	var fields []apperr.FieldError
	switch {
	case identifier == "":
		fields = append(fields, apperr.FieldError{Field: "identifier", Message: "VALIDATION_REQUIRED"})
	case utf8.RuneCountInString(identifier) > maxIdentifierRunes:
		fields = append(fields, apperr.FieldError{Field: "identifier", Message: "VALIDATION_DEFAULT"})
	}
	switch {
	case password == "":
		fields = append(fields, apperr.FieldError{Field: "password", Message: "VALIDATION_REQUIRED"})
	case utf8.RuneCountInString(password) > security.MaxPasswordLength:
		fields = append(fields, apperr.FieldError{Field: "password", Message: "VALIDATION_DEFAULT"})
	}
	return fields
}

// Login authenticates a user by username or email, enforces per-account rate limiting and soft cooldown,
// creates a session, and issues tokens.
func (s *Service) Login(ctx context.Context, input LoginInput) (LoginResult, error) {
	identifier := strings.ToLower(strings.TrimSpace(input.Identifier))
	if fieldErrs := validateLoginInput(identifier, input.Password); len(fieldErrs) > 0 {
		return LoginResult{}, apperr.Validation(fieldErrs)
	}

	preKeys := preLookupFailKeys(input.ClientIP, identifier)
	if retryAfter := s.checkLoginCooldownByKeys(ctx, preKeys); retryAfter > 0 {
		s.recordLoginFailureAudit(ctx, nil, identifier, input, "rate_limited", map[string]any{"retryAfter": retryAfter})
		return LoginResult{}, apperr.RateLimited().WithParams(map[string]any{"retry_after": retryAfter})
	}
	if retryAfter := s.accountRateLim.peek("ident:"+identifier, s.now().UTC()); retryAfter > 0 {
		s.recordLoginFailureAudit(ctx, nil, identifier, input, "rate_limited", map[string]any{"retryAfter": retryAfter})
		return LoginResult{}, apperr.RateLimited().WithParams(map[string]any{"retry_after": retryAfter})
	}

	creds, err := s.identity.GetAuthCredentials(ctx, identifier)
	if err != nil {
		if appErr, ok := apperr.As(err); ok && appErr.Status == http.StatusNotFound {
			if allowed, retryAfter := s.accountRateLim.allow("ident:"+identifier, s.now().UTC()); !allowed {
				s.recordLoginFailureAudit(ctx, nil, identifier, input, "rate_limited", map[string]any{"retryAfter": retryAfter})
				return LoginResult{}, apperr.RateLimited().WithParams(map[string]any{"retry_after": retryAfter})
			}
			s.hasher.VerifyDummy(input.Password)
			s.recordLoginFailureByKeys(ctx, preKeys)
			s.recordLoginFailureAudit(ctx, nil, identifier, input, "invalid_credentials", nil)
			return LoginResult{}, invalidCredentialsError()
		}
		return LoginResult{}, err
	}

	canonKeys := canonicalFailKeys(input.ClientIP, identifier, creds.User)
	if retryAfter := s.checkLoginCooldownByKeys(ctx, canonKeys); retryAfter > 0 {
		s.recordLoginFailureAudit(ctx, &creds.User.ID, creds.User.Username, input, "rate_limited", map[string]any{"retryAfter": retryAfter})
		return LoginResult{}, apperr.RateLimited().WithParams(map[string]any{"retry_after": retryAfter})
	}
	if allowed, retryAfter := s.accountRateLim.allow("uid:"+creds.User.ID.String(), s.now().UTC()); !allowed {
		s.recordLoginFailureAudit(ctx, &creds.User.ID, creds.User.Username, input, "rate_limited", map[string]any{"retryAfter": retryAfter})
		return LoginResult{}, apperr.RateLimited().WithParams(map[string]any{"retry_after": retryAfter})
	}

	matched, needsRehash, err := s.hasher.Verify(input.Password, creds.PasswordHash)
	if err != nil {
		s.log.Error("stored password hash invalid", "user_id", creds.User.ID, "error", err)
		s.recordLoginFailureAudit(ctx, &creds.User.ID, creds.User.Username, input, "invalid_credentials", nil)
		return LoginResult{}, invalidCredentialsError()
	}
	if !matched {
		s.recordLoginFailureByKeys(ctx, canonKeys)
		s.recordLoginFailureAudit(ctx, &creds.User.ID, creds.User.Username, input, "invalid_credentials", nil)
		return LoginResult{}, invalidCredentialsError()
	}
	if creds.User.Status == userStatusDisabled {
		s.recordLoginFailureAudit(ctx, &creds.User.ID, creds.User.Username, input, "account_disabled", nil)
		return LoginResult{}, accountDisabledError()
	}

	return s.finalizeLogin(ctx, creds, identifier, input, canonKeys, needsRehash)
}

func (s *Service) finalizeLogin(
	ctx context.Context,
	creds UserCredentials,
	identifier string,
	input LoginInput,
	canonKeys []string,
	needsRehash bool,
) (LoginResult, error) {
	var upgradedHash string
	if needsRehash {
		if upgraded, hashErr := s.hasher.Hash(input.Password); hashErr != nil {
			s.log.Warn("transparent password rehash failed", "user_id", creds.User.ID, "error", hashErr)
		} else {
			upgradedHash = upgraded
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

	accessToken, expiresAt, err := s.jwt.Sign(creds.User.ID, sessionID)
	if err != nil {
		return LoginResult{}, apperr.Internal(fmt.Errorf("sign access token: %w", err))
	}
	ua, err := s.access.GetUserAccess(ctx, creds.User.ID)
	if err != nil {
		return LoginResult{}, err
	}

	err = s.withinTx(ctx, func(txCtx context.Context) error {
		if completeErr := s.identity.CompleteLogin(txCtx, CompleteLoginInput{
			UserID:               creds.User.ID,
			ExpectedPasswordHash: creds.PasswordHash,
			UpgradedPasswordHash: upgradedHash,
		}); completeErr != nil {
			return completeErr
		}
		if createErr := s.store.CreateSession(txCtx, sessionRecord{
			ID:               sessionID,
			UserID:           creds.User.ID,
			RefreshTokenHash: refreshHash,
			UserAgent:        truncateUA(input.UserAgent),
			IPAddress:        input.ClientIP,
			ExpiresAt:        s.now().UTC().Add(s.cfg.RefreshTokenTTL),
		}); createErr != nil {
			return apperr.Internal(fmt.Errorf("create session: %w", createErr))
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
		if appErr, ok := apperr.As(err); ok {
			switch {
			case appErr.Code == ErrCodeAccountDisabled:
				s.recordLoginFailureAudit(ctx, &creds.User.ID, creds.User.Username, input, "account_disabled", nil)
				return LoginResult{}, accountDisabledError()
			case appErr.Status == http.StatusNotFound || appErr.Status == http.StatusUnauthorized:
				s.recordLoginFailureAudit(ctx, &creds.User.ID, creds.User.Username, input, "invalid_credentials", nil)
				return LoginResult{}, invalidCredentialsError()
			}
		}
		return LoginResult{}, err
	}

	s.clearLoginFailuresByKeys(ctx, canonKeys)

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

func (s *Service) recordLoginFailureAudit(
	ctx context.Context,
	actorID *uuid.UUID,
	actorUsername string,
	input LoginInput,
	reason string,
	extra map[string]any,
) {
	meta := map[string]any{"reason": reason}
	for k, v := range extra {
		meta[k] = v
	}
	var actorPtr *uuid.UUID
	if actorID != nil && *actorID != uuid.Nil {
		idCopy := *actorID
		actorPtr = &idCopy
	}
	s.recordAuditBestEffort(ctx, AuditEntry{
		ActorID:       actorPtr,
		ActorUsername: actorUsername,
		Action:        "auth.login",
		Resource:      "session",
		Status:        "failure",
		IPAddress:     input.ClientIP,
		UserAgent:     input.UserAgent,
		Metadata:      meta,
	})
}

// Refresh rotates the refresh token, detects replay via refresh_token_history, and allows concurrent grace-window refreshes.
func (s *Service) Refresh(ctx context.Context, input RefreshInput) (RefreshResult, error) {
	rawToken := strings.TrimSpace(input.RefreshToken)
	if rawToken == "" {
		return RefreshResult{}, apperr.Unauthorized(apperr.CodeUnauthorized)
	}
	tokenHash := security.HashTokenSHA256(rawToken)

	var outcome refreshOutcome
	err := s.withinTx(ctx, func(txCtx context.Context) error {
		sess, err := s.store.GetSessionByRefreshHashForUpdate(txCtx, tokenHash)
		if err == nil {
			outcome, err = s.rotateActiveSession(txCtx, sess, tokenHash, input)
			return err
		}
		if !errors.Is(err, errSessionNotFound) {
			return apperr.Internal(fmt.Errorf("lookup session by refresh hash: %w", err))
		}

		hist, histErr := s.store.GetRefreshTokenHistoryByHash(txCtx, tokenHash)
		if errors.Is(histErr, errHistoryNotFound) {
			outcome = refreshOutcome{authErr: apperr.Unauthorized(apperr.CodeUnauthorized)}
			return nil
		}
		if histErr != nil {
			return apperr.Internal(fmt.Errorf("lookup refresh token history: %w", histErr))
		}

		outcome, err = s.handleHistoricalToken(txCtx, hist, input)
		return err
	})
	if err != nil {
		return RefreshResult{}, err
	}
	if outcome.pendingAudit != nil {
		if outcome.pendingAudit.ActorUsername == "" && outcome.pendingAudit.ActorID != nil {
			outcome.pendingAudit.ActorUsername = s.lookupUsernameBestEffort(ctx, *outcome.pendingAudit.ActorID)
		}
		s.recordAuditBestEffort(ctx, *outcome.pendingAudit)
	}
	if outcome.authErr != nil {
		return RefreshResult{}, outcome.authErr
	}
	return outcome.result, nil
}

// rotateActiveSession returns (outcome, txErr).
// Security revocations (disabled/deleted user) populate outcome.authErr with nil txErr
// so the enclosing transaction commits the revocation instead of rolling it back;
// outcome.pendingAudit is emitted after commit so an audit failure never aborts the revocation tx.
func (s *Service) rotateActiveSession(
	txCtx context.Context,
	sess sessionRecord,
	oldHash string,
	input RefreshInput,
) (refreshOutcome, error) {
	now := s.now().UTC()
	if sess.RevokedAt != nil || !now.Before(sess.ExpiresAt) {
		return refreshOutcome{authErr: apperr.Unauthorized(apperr.CodeUnauthorized)}, nil
	}

	user, outcome, err := s.verifySessionUserActive(txCtx, sess, input)
	if err != nil || outcome.authErr != nil {
		return outcome, err
	}

	newRaw, err := security.GenerateOpaqueToken(security.DefaultOpaqueTokenBytes)
	if err != nil {
		return refreshOutcome{}, apperr.Internal(err)
	}
	newHash := security.HashTokenSHA256(newRaw)
	newExp := now.Add(s.cfg.RefreshTokenTTL)

	if err := s.store.InsertRefreshTokenHistory(txCtx, oldHash, sess.ID, now, sess.ExpiresAt); err != nil {
		return refreshOutcome{}, apperr.Internal(fmt.Errorf("record refresh token history: %w", err))
	}
	if err := s.store.RotateSessionRefreshToken(
		txCtx,
		sess.ID,
		newHash,
		truncateUA(input.UserAgent),
		input.ClientIP,
		newExp,
	); err != nil {
		return refreshOutcome{}, apperr.Internal(fmt.Errorf("rotate session refresh token: %w", err))
	}

	accessToken, accessExp, err := s.jwt.Sign(user.ID, sess.ID)
	if err != nil {
		return refreshOutcome{}, apperr.Internal(fmt.Errorf("sign refreshed access token: %w", err))
	}

	return refreshOutcome{
		result: RefreshResult{
			AccessToken:  accessToken,
			RefreshToken: newRaw,
			RotateCookie: true,
			ExpiresAt:    accessExp,
		},
	}, nil
}

// handleHistoricalToken returns (outcome, txErr) so replay and disabled-user revocations commit cleanly.
func (s *Service) handleHistoricalToken(
	txCtx context.Context,
	hist refreshHistoryRecord,
	input RefreshInput,
) (refreshOutcome, error) {
	sess, err := s.store.GetSessionByIDForUpdate(txCtx, hist.SessionID)
	if errors.Is(err, errSessionNotFound) {
		return refreshOutcome{authErr: apperr.Unauthorized(apperr.CodeUnauthorized)}, nil
	}
	if err != nil {
		return refreshOutcome{}, apperr.Internal(fmt.Errorf("lookup historical session: %w", err))
	}

	now := s.now().UTC()
	if sess.RevokedAt == nil && !now.Before(sess.ExpiresAt) {
		return refreshOutcome{authErr: apperr.Unauthorized(apperr.CodeUnauthorized)}, nil
	}

	// Outside concurrent refresh grace window: treat as token theft/replay and revoke entire session.
	if now.Sub(hist.ReplacedAt) > refreshGraceWindow {
		if sess.RevokedAt == nil {
			if _, _, revErr := s.store.RevokeSession(txCtx, sess.ID); revErr != nil {
				return refreshOutcome{}, apperr.Internal(fmt.Errorf("revoke replayed session: %w", revErr))
			}
		}
		userID := sess.UserID
		s.log.Warn("refresh token replay detected; session revoked",
			"session_id", sess.ID.String(),
			"user_id", sess.UserID.String(),
		)
		return refreshOutcome{
			authErr: refreshReusedError(),
			pendingAudit: &AuditEntry{
				ActorID:    &userID,
				Action:     "auth.refresh_replay",
				Resource:   "session",
				ResourceID: sess.ID.String(),
				Status:     "failure",
				IPAddress:  input.ClientIP,
				UserAgent:  input.UserAgent,
				Metadata: map[string]any{
					"reason":    "refresh_token_reused",
					"sessionId": sess.ID.String(),
				},
			},
		}, nil
	}

	// Inside grace window, an already-revoked session (e.g. concurrent tab refresh after logout)
	// fails cleanly as 401 AUTH_UNAUTHORIZED rather than misreporting token theft.
	if sess.RevokedAt != nil {
		return refreshOutcome{authErr: apperr.Unauthorized(apperr.CodeUnauthorized)}, nil
	}

	user, outcome, err := s.verifySessionUserActive(txCtx, sess, input)
	if err != nil || outcome.authErr != nil {
		return outcome, err
	}

	accessToken, accessExp, err := s.jwt.Sign(user.ID, sess.ID)
	if err != nil {
		return refreshOutcome{}, apperr.Internal(fmt.Errorf("sign grace-window access token: %w", err))
	}

	return refreshOutcome{
		result: RefreshResult{
			AccessToken:  accessToken,
			RotateCookie: false,
			ExpiresAt:    accessExp,
		},
	}, nil
}

func (s *Service) verifySessionUserActive(
	txCtx context.Context,
	sess sessionRecord,
	input RefreshInput,
) (User, refreshOutcome, error) {
	user, err := s.identity.GetByID(txCtx, sess.UserID)
	if err != nil {
		if appErr, ok := apperr.As(err); ok && appErr.Status == http.StatusNotFound {
			if _, _, revErr := s.store.RevokeSession(txCtx, sess.ID); revErr != nil {
				return User{}, refreshOutcome{}, apperr.Internal(fmt.Errorf("revoke deleted user session: %w", revErr))
			}
			return User{}, refreshOutcome{authErr: apperr.Unauthorized(apperr.CodeUnauthorized)}, nil
		}
		return User{}, refreshOutcome{}, err
	}
	if user.Status == userStatusDisabled {
		if revErr := s.store.RevokeAllUserSessions(txCtx, sess.UserID); revErr != nil {
			return User{}, refreshOutcome{}, apperr.Internal(fmt.Errorf("revoke disabled user sessions: %w", revErr))
		}
		userID := user.ID
		return User{}, refreshOutcome{
			authErr: accountDisabledError(),
			pendingAudit: &AuditEntry{
				ActorID:       &userID,
				ActorUsername: user.Username,
				Action:        "auth.refresh",
				Resource:      "session",
				ResourceID:    sess.ID.String(),
				Status:        "failure",
				IPAddress:     input.ClientIP,
				UserAgent:     input.UserAgent,
				Metadata:      map[string]any{"reason": "account_disabled"},
			},
		}, nil
	}
	return user, refreshOutcome{}, nil
}

// Logout revokes the current session identified by refresh token (current or historical) or access token/session ID
// and writes an audit entry only when an active session was actually revoked.
func (s *Service) Logout(ctx context.Context, input LogoutInput) error {
	sessionID := input.SessionID
	if sessionID == uuid.Nil && input.AccessToken != "" && s.jwt != nil {
		if claims, err := s.jwt.Verify(input.AccessToken); err == nil {
			sessionID = claims.SessionID
		}
	}

	var revoked []revokedSession
	err := s.withinTx(ctx, func(txCtx context.Context) error {
		seen := make(map[uuid.UUID]struct{}, 2)
		recordRevoked := func(rev revokedSession) {
			if _, dup := seen[rev.ID]; dup {
				return
			}
			seen[rev.ID] = struct{}{}
			revoked = append(revoked, rev)
		}

		if trimmed := strings.TrimSpace(input.RefreshToken); trimmed != "" {
			hash := security.HashTokenSHA256(trimmed)
			rev, ok, revErr := s.store.RevokeSessionByRefreshHash(txCtx, hash)
			if revErr != nil {
				return apperr.Internal(fmt.Errorf("revoke session by token: %w", revErr))
			}
			if ok {
				recordRevoked(rev)
			} else {
				hist, histErr := s.store.GetRefreshTokenHistoryByHash(txCtx, hash)
				if histErr != nil && !errors.Is(histErr, errHistoryNotFound) {
					return apperr.Internal(fmt.Errorf("lookup refresh history for logout: %w", histErr))
				}
				if histErr == nil {
					histRev, histOK, revHistErr := s.store.RevokeSession(txCtx, hist.SessionID)
					if revHistErr != nil {
						return apperr.Internal(fmt.Errorf("revoke session by historical token: %w", revHistErr))
					}
					if histOK {
						recordRevoked(histRev)
					}
				}
			}
		}

		if sessionID != uuid.Nil {
			if _, alreadyRevoked := seen[sessionID]; !alreadyRevoked {
				rev, ok, revErr := s.store.RevokeSession(txCtx, sessionID)
				if revErr != nil {
					return apperr.Internal(fmt.Errorf("revoke session by id: %w", revErr))
				}
				if ok {
					recordRevoked(rev)
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	for _, rev := range revoked {
		actorID := rev.UserID
		if actorID == uuid.Nil {
			actorID = input.UserID
		}
		var actorPtr *uuid.UUID
		var actorUsername string
		if actorID != uuid.Nil {
			idCopy := actorID
			actorPtr = &idCopy
			actorUsername = s.lookupUsernameBestEffort(ctx, actorID)
		}
		s.recordAuditBestEffort(ctx, AuditEntry{
			ActorID:       actorPtr,
			ActorUsername: actorUsername,
			Action:        "auth.logout",
			Resource:      "session",
			ResourceID:    rev.ID.String(),
			Status:        "success",
			IPAddress:     input.ClientIP,
			UserAgent:     input.UserAgent,
		})
	}

	return nil
}

func (s *Service) lookupUsernameBestEffort(ctx context.Context, userID uuid.UUID) string {
	if userID == uuid.Nil || s.identity == nil {
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
		if appErr, ok := apperr.As(err); ok && appErr.Status == http.StatusNotFound {
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
			return totalDeleted, apperr.Internal(err)
		}
		deleted, err := s.store.DeleteExpiredRefreshTokenHistoryBatch(ctx, cutoff, batchSize)
		if err != nil {
			return totalDeleted, apperr.Internal(fmt.Errorf("delete expired refresh token history batch: %w", err))
		}
		totalDeleted += deleted
		if deleted < int64(batchSize) {
			break
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return totalDeleted, apperr.Internal(err)
		}
		deleted, err := s.store.DeleteExpiredUserSessionsBatch(ctx, cutoff, batchSize)
		if err != nil {
			return totalDeleted, apperr.Internal(fmt.Errorf("delete expired user sessions batch: %w", err))
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

func (s *Service) failLockForKey(key string) *sync.Mutex {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return &s.failLocks[h.Sum32()%failLockShardCount]
}

func (s *Service) checkLoginCooldownByKeys(ctx context.Context, keys []string) int {
	if s.cache == nil {
		return 0
	}
	now := s.now().UTC()
	var maxRetry int
	for _, key := range keys {
		raw, err := s.cache.Get(ctx, key)
		if err != nil {
			if !errors.Is(err, cache.ErrNotFound) {
				s.log.Warn("read login cooldown cache failed", "key", key, "error", err)
			}
			continue
		}
		var st loginFailureState
		if err := json.Unmarshal(raw, &st); err != nil {
			s.log.Warn("unmarshal login cooldown state failed", "key", key, "error", err)
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

func (s *Service) recordLoginFailureByKeys(ctx context.Context, keys []string) {
	if s.cache == nil {
		return
	}
	now := s.now().UTC()
	for _, key := range keys {
		mu := s.failLockForKey(key)
		mu.Lock()
		var st loginFailureState
		raw, err := s.cache.Get(ctx, key)
		if err == nil {
			if unmarshalErr := json.Unmarshal(raw, &st); unmarshalErr != nil {
				s.log.Warn("unmarshal login cooldown state failed", "key", key, "error", unmarshalErr)
				st = loginFailureState{}
			} else if !st.BlockedUntil.IsZero() && !now.Before(st.BlockedUntil) {
				st = loginFailureState{}
			}
		} else if !errors.Is(err, cache.ErrNotFound) {
			s.log.Warn("read login cooldown cache failed", "key", key, "error", err)
		}

		st.Count++
		ttl := loginFailWindowDuration
		if st.Count >= maxLoginFailures {
			st.BlockedUntil = now.Add(loginCooldownDuration)
			ttl = loginCooldownDuration
		}
		encoded, marshalErr := json.Marshal(st)
		if marshalErr != nil {
			s.log.Warn("marshal login cooldown state failed", "key", key, "error", marshalErr)
		} else if setErr := s.cache.Set(ctx, key, encoded, ttl); setErr != nil {
			s.log.Warn("write login cooldown cache failed", "key", key, "error", setErr)
		}
		mu.Unlock()
	}
}

func (s *Service) clearLoginFailuresByKeys(ctx context.Context, keys []string) {
	if s.cache == nil {
		return
	}
	for _, key := range keys {
		mu := s.failLockForKey(key)
		mu.Lock()
		if err := s.cache.Delete(ctx, key); err != nil {
			s.log.Warn("delete login cooldown cache failed", "key", key, "error", err)
		}
		mu.Unlock()
	}
}

func preLookupFailKeys(ip, identifier string) []string {
	keys := make([]string, 0, 2)
	if identifier != "" {
		keys = append(keys, "auth:fail:acct:"+identifier)
		if ip != "" {
			keys = append(keys, "auth:fail:ipacct:"+ip+":"+identifier)
		}
	}
	return keys
}

func canonicalFailKeys(ip, identifier string, user User) []string {
	seen := make(map[string]struct{}, 8)
	keys := make([]string, 0, 8)
	addSubject := func(subject string) {
		if subject == "" {
			return
		}
		k1 := "auth:fail:acct:" + subject
		if _, dup := seen[k1]; !dup {
			seen[k1] = struct{}{}
			keys = append(keys, k1)
		}
		if ip != "" {
			k2 := "auth:fail:ipacct:" + ip + ":" + subject
			if _, dup := seen[k2]; !dup {
				seen[k2] = struct{}{}
				keys = append(keys, k2)
			}
		}
	}

	if user.ID != uuid.Nil {
		addSubject("uid:" + user.ID.String())
	}
	if uname := strings.ToLower(strings.TrimSpace(user.Username)); uname != "" {
		addSubject(uname)
	}
	if email := strings.ToLower(strings.TrimSpace(user.Email)); email != "" {
		addSubject(email)
	}
	if ident := strings.ToLower(strings.TrimSpace(identifier)); ident != "" {
		addSubject(ident)
	}
	return keys
}

func (s *Service) withinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if s.tx == nil {
		return fn(ctx)
	}
	err := s.tx.WithinTx(ctx, fn)
	if err != nil {
		if _, ok := apperr.As(err); !ok {
			return apperr.Internal(err)
		}
	}
	return err
}

func truncateUA(ua string) string {
	if ua == "" {
		return ""
	}
	cleaned := strings.ReplaceAll(ua, "\x00", "")
	if !utf8.ValidString(cleaned) {
		cleaned = strings.ToValidUTF8(cleaned, "")
	}
	cleaned = strings.TrimSpace(cleaned)
	if len(cleaned) <= maxUserAgentBytes {
		return cleaned
	}
	cut := maxUserAgentBytes
	for cut > 0 && !utf8.RuneStart(cleaned[cut]) {
		cut--
	}
	return cleaned[:cut]
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return make([]string, 0)
	}
	return in
}
