package identity

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/platform/database"
	"tianshu/internal/platform/security"
	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/pagination"
)

const (
	// usersSuperAdminGuardKey serializes concurrent mutations that could remove the last active super_admin
	// or concurrently demote/disable an administrator performing user mutations.
	usersSuperAdminGuardKey = int64(0x7469616e73687502)

	superAdminRoleCode                  = "super_admin"
	auditResourceUser                   = "user"
	auditResourceSession                = "session"
	auditActionCreate                   = "user.create"
	auditActionUpdate                   = "user.update"
	auditActionDelete                   = "user.delete"
	auditActionProfileUpdate            = "profile.update"
	auditActionProfileChangePassword    = "profile.change_password"
	auditActionProfileSessionRevoke     = "profile.session_revoke"
	auditActionProfileSessionsRevokeAll = "profile.sessions_revoke_others"
	auditStatusSuccess                  = "success"
	auditStatusFailure                  = "failure"

	minUsernameLen  = 3
	maxUsernameLen  = 64
	maxEmailLen     = 255
	maxDisplayRunes = 100
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9._-]+$`)

// ServiceDeps holds optional cross-cutting dependencies for identity.Service.
type ServiceDeps struct {
	Tx       TxRunner
	Roles    RoleManager
	Sessions SessionManager
	Files    AvatarFileManager
	Audit    AuditRecorder
	Log      *slog.Logger
}

// Service owns user lifecycle persistence and invariants.
type Service struct {
	store    store
	hasher   PasswordHasher
	tx       TxRunner
	roles    RoleManager
	sessions SessionManager
	files    AvatarFileManager
	audit    AuditRecorder
	log      *slog.Logger
}

// NewService creates an identity Service backed by PostgreSQL.
func NewService(pool *pgxpool.Pool, hasher PasswordHasher, deps ...ServiceDeps) *Service {
	var d ServiceDeps
	if len(deps) > 0 {
		d = deps[0]
	}
	return newServiceWithStore(newStore(pool), hasher, d)
}

func newServiceWithStore(s store, hasher PasswordHasher, deps ...ServiceDeps) *Service {
	var d ServiceDeps
	if len(deps) > 0 {
		d = deps[0]
	}
	log := d.Log
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		store:    s,
		hasher:   hasher,
		tx:       d.Tx,
		roles:    d.Roles,
		sessions: d.Sessions,
		files:    d.Files,
		audit:    d.Audit,
		log:      log,
	}
}

// CountUsers returns the number of non-deleted user accounts.
func (s *Service) CountUsers(ctx context.Context) (int64, error) {
	count, err := s.store.CountUsers(ctx)
	if err != nil {
		return 0, apperr.Internal(fmt.Errorf("count users: %w", err))
	}
	return count, nil
}

// ListUsernamesByIDs returns a map of user ID to username for batch actor/uploader projections.
// Soft-deleted users are included so historical references retain their original username.
func (s *Service) ListUsernamesByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	deduped := dedupeUUIDs(ids)
	if len(deduped) == 0 {
		return make(map[uuid.UUID]string), nil
	}
	m, err := s.store.ListUsernamesByIDs(ctx, deduped)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("list usernames by ids: %w", err))
	}
	return m, nil
}

// IsAvatarFileInUse reports whether any non-deleted user currently references fileID as their avatar.
func (s *Service) IsAvatarFileInUse(ctx context.Context, fileID uuid.UUID) (bool, error) {
	if fileID == uuid.Nil {
		return false, nil
	}
	count, err := s.store.CountActiveUsersByAvatarFileID(ctx, fileID)
	if err != nil {
		return false, apperr.Internal(fmt.Errorf("check avatar file references: %w", err))
	}
	return count > 0, nil
}

// CreateInitialAdmin creates the first active administrator account using UUIDv7 and Argon2id.
// Callers are expected to coordinate transaction and advisory locking at the orchestration boundary.
func (s *Service) CreateInitialAdmin(ctx context.Context, input CreateInitialAdminInput) (User, error) {
	var fieldErrs []apperr.FieldError
	username, ok := normalizeUsername(input.Username)
	if !ok {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "username", Message: ErrCodeInvalidUsername})
	}
	email, ok := normalizeEmail(input.Email)
	if !ok {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "email", Message: ErrCodeInvalidEmail})
	}
	displayName, ok := normalizeDisplayName(input.DisplayName, username)
	if !ok {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "displayName", Message: ErrCodeInvalidDisplay})
	}
	if err := security.ValidatePasswordLength(input.Password); errors.Is(err, security.ErrInvalidPasswordLength) {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "password", Message: ErrCodeInvalidPassword})
	}
	if len(fieldErrs) > 0 {
		return User{}, apperr.Validation(fieldErrs)
	}

	hash, err := s.hasher.Hash(input.Password)
	if errors.Is(err, security.ErrInvalidPasswordLength) {
		return User{}, apperr.Validation([]apperr.FieldError{{Field: "password", Message: ErrCodeInvalidPassword}})
	}
	if err != nil {
		return User{}, apperr.Internal(fmt.Errorf("hash initial admin password: %w", err))
	}

	userID, err := uuid.NewV7()
	if err != nil {
		return User{}, apperr.Internal(fmt.Errorf("generate user uuid: %w", err))
	}

	user, err := s.store.CreateUser(ctx, createUserRecord{
		ID:           userID,
		Username:     username,
		Email:        email,
		PasswordHash: hash,
		DisplayName:  displayName,
		Status:       StatusActive,
	})
	if errors.Is(err, errUsernameConflict) {
		return User{}, usernameExistsError()
	}
	if errors.Is(err, errEmailConflict) {
		return User{}, emailExistsError()
	}
	if err != nil {
		return User{}, apperr.Internal(fmt.Errorf("create initial admin: %w", err))
	}
	return user, nil
}

// GetAuthCredentials looks up an active/disabled (non-deleted) user by normalized username or email.
func (s *Service) GetAuthCredentials(ctx context.Context, identifier string) (AuthCredentials, error) {
	normalized := strings.ToLower(strings.TrimSpace(identifier))
	if normalized == "" || !isValidText(normalized) {
		return AuthCredentials{}, userNotFoundError()
	}
	creds, err := s.store.GetUserForAuthByIdentifier(ctx, normalized)
	if errors.Is(err, errUserNotFound) {
		return AuthCredentials{}, userNotFoundError()
	}
	if err != nil {
		return AuthCredentials{}, apperr.Internal(fmt.Errorf("lookup user credentials: %w", err))
	}
	return creds, nil
}

// GetByID returns a non-deleted user by ID.
func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (User, error) {
	user, err := s.store.GetUserByID(ctx, id)
	if errors.Is(err, errUserNotFound) {
		return User{}, userNotFoundError()
	}
	if err != nil {
		return User{}, apperr.Internal(fmt.Errorf("get user %s: %w", id, err))
	}
	return user, nil
}

// RecordLogin updates last_login_at for id.
func (s *Service) RecordLogin(ctx context.Context, id uuid.UUID) error {
	if err := s.store.UpdateUserLastLogin(ctx, id); err != nil {
		return apperr.Internal(fmt.Errorf("update last login: %w", err))
	}
	return nil
}

// UpdatePasswordHash replaces the stored password hash (e.g. transparent Argon2id parameter upgrade).
func (s *Service) UpdatePasswordHash(ctx context.Context, id uuid.UUID, newHash string) error {
	if err := s.store.UpdateUserPasswordHash(ctx, id, newHash); err != nil {
		return apperr.Internal(fmt.Errorf("update password hash: %w", err))
	}
	return nil
}

// CompleteLogin locks the user row FOR UPDATE within the caller's login transaction,
// re-verifies that the user is active and non-deleted with an unchanged password hash,
// optionally applies a compare-and-swap password rehash, and updates last_login_at.
func (s *Service) CompleteLogin(ctx context.Context, input CompleteLoginInput) error {
	locked, err := s.store.GetUserPasswordHashByIDForUpdate(ctx, input.UserID)
	if errors.Is(err, errUserNotFound) {
		return userNotFoundError()
	}
	if err != nil {
		return apperr.Internal(fmt.Errorf("lock user %s for login: %w", input.UserID, err))
	}
	if locked.Status == StatusDisabled {
		return accountDisabledError()
	}
	if locked.PasswordHash != input.ExpectedPasswordHash {
		return apperr.Unauthorized(apperr.CodeUnauthorized)
	}
	if input.UpgradedPasswordHash != "" {
		updated, upErr := s.store.UpdateUserPasswordHashIfMatches(ctx, locked.ID, input.ExpectedPasswordHash, input.UpgradedPasswordHash)
		if upErr != nil {
			return apperr.Internal(fmt.Errorf("upgrade password hash %s: %w", locked.ID, upErr))
		}
		if updated == 0 {
			return apperr.Unauthorized(apperr.CodeUnauthorized)
		}
	}
	if err := s.store.UpdateUserLastLogin(ctx, locked.ID); err != nil {
		return apperr.Internal(fmt.Errorf("update last login %s: %w", locked.ID, err))
	}
	return nil
}

// ListUsers returns a paginated list of non-deleted users with their roles and audit actors.
func (s *Service) ListUsers(ctx context.Context, filter UserListFilter) (pagination.Result[UserDetail], error) {
	q := strings.TrimSpace(filter.Q)
	if !isValidText(q) {
		return pagination.Result[UserDetail]{}, apperr.Validation([]apperr.FieldError{{
			Field:   "q",
			Message: "VALIDATION_DEFAULT",
		}})
	}

	pageParams := pagination.Normalize(filter.Pagination.Page, filter.Pagination.PageSize)

	var (
		filterIDs bool
		userIDs   []uuid.UUID
	)
	if len(filter.RoleIDs) > 0 && s.roles != nil {
		filterIDs = true
		ids, err := s.roles.ListUserIDsByRoleIDs(ctx, dedupeUUIDs(filter.RoleIDs))
		if err != nil {
			return pagination.Result[UserDetail]{}, wrapAppErr(err)
		}
		if len(ids) == 0 {
			return pagination.NewResult(make([]UserDetail, 0), 0, pageParams), nil
		}
		userIDs = ids
	}

	query := listUsersQuery{
		Q:         q,
		Statuses:  normalizeStatusesFilter(filter.Statuses),
		FilterIDs: filterIDs,
		IDs:       userIDs,
		Sort:      normalizeUserSort(filter.Sort),
		Order:     normalizeUserOrder(filter.Order),
		Offset:    pageParams.Offset(),
		Limit:     pageParams.Limit(),
	}

	total, err := s.store.CountUsersFiltered(ctx, query)
	if err != nil {
		return pagination.Result[UserDetail]{}, apperr.Internal(fmt.Errorf("count users: %w", err))
	}
	if total == 0 {
		return pagination.NewResult(make([]UserDetail, 0), 0, pageParams), nil
	}

	items, err := s.store.ListUsers(ctx, query)
	if err != nil {
		return pagination.Result[UserDetail]{}, apperr.Internal(fmt.Errorf("list users: %w", err))
	}
	if err := s.attachUserRoles(ctx, items); err != nil {
		return pagination.Result[UserDetail]{}, err
	}
	return pagination.NewResult(items, total, pageParams), nil
}

// GetUser returns a single user detail with roles and audit actors.
func (s *Service) GetUser(ctx context.Context, id uuid.UUID) (UserDetail, error) {
	detail, err := s.store.GetUserDetailByID(ctx, id)
	if errors.Is(err, errUserNotFound) {
		return UserDetail{}, userNotFoundError()
	}
	if err != nil {
		return UserDetail{}, apperr.Internal(fmt.Errorf("get user detail %s: %w", id, err))
	}
	slice := []UserDetail{detail}
	if err := s.attachUserRoles(ctx, slice); err != nil {
		return UserDetail{}, err
	}
	return slice[0], nil
}

// CreateUser creates a new user account, assigns initial roles, and records an audit entry.
func (s *Service) CreateUser(ctx context.Context, input CreateUserInput) (UserDetail, error) {
	validated, err := s.validateCreateUserInput(ctx, input)
	if err != nil {
		return UserDetail{}, err
	}

	needsSuperAdmin := hasRoleCode(validated.roles, superAdminRoleCode)
	// Fast pre-checks before expensive 64 MiB Argon2id hashing; re-verified authoritatively inside tx.
	if _, err := s.verifyActiveActor(ctx, input.ActorID, input.ActorSessionID, false); err != nil {
		return UserDetail{}, err
	}
	if needsSuperAdmin {
		isSuper, err := s.actorIsSuperAdmin(ctx, input.ActorID)
		if err != nil {
			return UserDetail{}, err
		}
		if !isSuper {
			return UserDetail{}, superAdminProtectedError()
		}
	}

	hash, hashErr := s.hasher.Hash(input.Password)
	if errors.Is(hashErr, security.ErrInvalidPasswordLength) {
		return UserDetail{}, apperr.Validation([]apperr.FieldError{{Field: "password", Message: ErrCodeInvalidPassword}})
	}
	if hashErr != nil {
		return UserDetail{}, apperr.Internal(fmt.Errorf("hash user password: %w", hashErr))
	}

	userID, err := uuid.NewV7()
	if err != nil {
		return UserDetail{}, apperr.Internal(fmt.Errorf("generate user uuid: %w", err))
	}

	var created UserDetail
	err = s.withinTx(ctx, func(txCtx context.Context) error {
		if err := database.AcquireAdvisoryXactLock(txCtx, usersSuperAdminGuardKey); err != nil {
			return apperr.Internal(err)
		}

		actorUsername, err := s.verifyActiveActor(txCtx, input.ActorID, input.ActorSessionID, true)
		if err != nil {
			return err
		}
		if needsSuperAdmin {
			isSuper, err := s.actorIsSuperAdmin(txCtx, input.ActorID)
			if err != nil {
				return err
			}
			if !isSuper {
				return superAdminProtectedError()
			}
		}

		user, createErr := s.store.CreateUser(txCtx, createUserRecord{
			ID:           userID,
			Username:     validated.username,
			Email:        validated.email,
			PasswordHash: hash,
			DisplayName:  validated.displayName,
			Status:       validated.status,
			CreatedBy:    ptrToNullUUID(input.ActorID),
			UpdatedBy:    ptrToNullUUID(input.ActorID),
		})
		if errors.Is(createErr, errUsernameConflict) {
			return usernameExistsError()
		}
		if errors.Is(createErr, errEmailConflict) {
			return emailExistsError()
		}
		if createErr != nil {
			return apperr.Internal(fmt.Errorf("create user %s: %w", validated.username, createErr))
		}

		if len(validated.roleIDs) > 0 && s.roles != nil {
			if err := s.roles.ReplaceUserRoles(txCtx, user.ID, validated.roleIDs); err != nil {
				return wrapAppErr(err)
			}
		}

		detail, detailErr := s.store.GetUserDetailByID(txCtx, user.ID)
		if detailErr != nil {
			return apperr.Internal(fmt.Errorf("load created user %s: %w", user.ID, detailErr))
		}
		detail.Roles = nonNilRoleRefs(validated.roles)
		created = detail

		return s.recordAuditEntry(txCtx, AuditEntry{
			ActorID:       input.ActorID,
			ActorUsername: actorUsername,
			Action:        auditActionCreate,
			Resource:      auditResourceUser,
			ResourceID:    user.ID.String(),
			Status:        auditStatusSuccess,
			IPAddress:     input.Meta.IPAddress,
			UserAgent:     input.Meta.UserAgent,
			Metadata: map[string]any{
				"username": user.Username,
				"status":   user.Status,
				"roles":    roleCodes(validated.roles),
			},
		})
	})
	if err != nil {
		return UserDetail{}, err
	}
	return created, nil
}

// UpdateUser updates a user's profile, status, password, and/or roles with super-admin and self-operation guards.
func (s *Service) UpdateUser(ctx context.Context, input UpdateUserInput) (UserDetail, error) {
	validated, err := s.validateUpdateUserInput(ctx, input)
	if err != nil {
		return UserDetail{}, err
	}

	var newPasswordHash *string
	if input.Password != nil {
		// Fast actor check before burning 64 MiB Argon2id RAM; re-verified inside tx.
		if _, err := s.verifyActiveActor(ctx, input.ActorID, input.ActorSessionID, false); err != nil {
			return UserDetail{}, err
		}
		hash, hashErr := s.hasher.Hash(*input.Password)
		if errors.Is(hashErr, security.ErrInvalidPasswordLength) {
			return UserDetail{}, apperr.Validation([]apperr.FieldError{{Field: "password", Message: ErrCodeInvalidPassword}})
		}
		if hashErr != nil {
			return UserDetail{}, apperr.Internal(fmt.Errorf("hash reset password: %w", hashErr))
		}
		newPasswordHash = &hash
	}

	var updated UserDetail
	err = s.withinTx(ctx, func(txCtx context.Context) error {
		if err := database.AcquireAdvisoryXactLock(txCtx, usersSuperAdminGuardKey); err != nil {
			return apperr.Internal(err)
		}

		existing, actorUsername, err := s.lockTargetAndVerifyActor(txCtx, input.UserID, input.ActorID, input.ActorSessionID, "update")
		if err != nil {
			return err
		}

		currentRoles, err := s.getUserRoles(txCtx, existing.ID)
		if err != nil {
			return err
		}
		finalRoles := currentRoles
		if validated.updateRoles {
			finalRoles = nonNilRoleRefs(validated.roles)
		}
		rolesChanged := validated.updateRoles && !sameRoleSet(currentRoles, finalRoles)

		finalStatus := existing.Status
		if validated.status != nil {
			finalStatus = *validated.status
		}
		if err := s.enforceUserUpdateGuards(txCtx, userUpdateGuardState{
			existing:     existing,
			currentRoles: currentRoles,
			finalRoles:   finalRoles,
			finalStatus:  finalStatus,
			actorID:      input.ActorID,
		}); err != nil {
			return err
		}

		finalEmail, finalDisplay := resolveFinalEmailAndDisplay(existing, validated.email, validated.displayName)
		if err := s.store.UpdateUserProfile(txCtx, updateUserProfileRecord{
			ID:          existing.ID,
			Email:       finalEmail,
			DisplayName: finalDisplay,
			Status:      finalStatus,
			UpdatedBy:   input.ActorID,
		}); err != nil {
			if errors.Is(err, errUserNotFound) {
				return userNotFoundError()
			}
			if errors.Is(err, errEmailConflict) {
				return emailExistsError()
			}
			return apperr.Internal(fmt.Errorf("update user profile %s: %w", existing.ID, err))
		}

		if newPasswordHash != nil {
			if err := s.store.UpdateUserPasswordWithActor(txCtx, existing.ID, *newPasswordHash, input.ActorID); err != nil {
				return apperr.Internal(fmt.Errorf("reset user password %s: %w", existing.ID, err))
			}
		}

		if rolesChanged && s.roles != nil {
			if err := s.roles.ReplaceUserRoles(txCtx, existing.ID, validated.roleIDs); err != nil {
				return wrapAppErr(err)
			}
		}

		if (finalStatus == StatusDisabled || newPasswordHash != nil) && s.sessions != nil {
			if err := s.sessions.RevokeAllUserSessions(txCtx, existing.ID); err != nil {
				return wrapAppErr(err)
			}
		}

		detail, detailErr := s.store.GetUserDetailByID(txCtx, existing.ID)
		if detailErr != nil {
			return apperr.Internal(fmt.Errorf("reload user %s: %w", existing.ID, detailErr))
		}
		detail.Roles = finalRoles
		updated = detail

		meta := buildUserUpdateAuditMeta(userUpdateAuditDiff{
			existing:      existing,
			finalEmail:    finalEmail,
			finalDisplay:  finalDisplay,
			finalStatus:   finalStatus,
			emailSet:      validated.email != nil,
			displaySet:    validated.displayName != nil,
			statusSet:     validated.status != nil,
			passwordReset: newPasswordHash != nil,
			rolesChanged:  rolesChanged,
			currentRoles:  currentRoles,
			finalRoles:    finalRoles,
		})
		return s.recordAuditEntry(txCtx, AuditEntry{
			ActorID:       input.ActorID,
			ActorUsername: actorUsername,
			Action:        auditActionUpdate,
			Resource:      auditResourceUser,
			ResourceID:    existing.ID.String(),
			Status:        auditStatusSuccess,
			IPAddress:     input.Meta.IPAddress,
			UserAgent:     input.Meta.UserAgent,
			Metadata:      meta,
		})
	})
	if err != nil {
		return UserDetail{}, err
	}
	return updated, nil
}

// DeleteUser soft-deletes a user, clears role bindings, revokes all sessions, and writes an audit entry.
func (s *Service) DeleteUser(ctx context.Context, input DeleteUserInput) error {
	return s.withinTx(ctx, func(txCtx context.Context) error {
		if err := database.AcquireAdvisoryXactLock(txCtx, usersSuperAdminGuardKey); err != nil {
			return apperr.Internal(err)
		}

		existing, actorUsername, err := s.lockTargetAndVerifyActor(txCtx, input.UserID, input.ActorID, input.ActorSessionID, "delete")
		if err != nil {
			return err
		}
		if input.ActorID != nil && *input.ActorID == existing.ID {
			return selfOperationError()
		}

		currentRoles, err := s.getUserRoles(txCtx, existing.ID)
		if err != nil {
			return err
		}
		if hasRoleCode(currentRoles, superAdminRoleCode) {
			isSuper, err := s.actorIsSuperAdmin(txCtx, input.ActorID)
			if err != nil {
				return err
			}
			if !isSuper {
				return superAdminProtectedError()
			}
			if existing.Status == StatusActive {
				if err := s.ensureOtherActiveSuperAdmin(txCtx, existing.ID); err != nil {
					return err
				}
			}
		}

		if s.roles != nil {
			if err := s.roles.ReplaceUserRoles(txCtx, existing.ID, nil); err != nil {
				return wrapAppErr(err)
			}
		}
		if s.sessions != nil {
			if err := s.sessions.RevokeAllUserSessions(txCtx, existing.ID); err != nil {
				return wrapAppErr(err)
			}
		}
		if err := s.store.SoftDeleteUser(txCtx, existing.ID, input.ActorID); err != nil {
			return apperr.Internal(fmt.Errorf("soft delete user %s: %w", existing.ID, err))
		}

		return s.recordAuditEntry(txCtx, AuditEntry{
			ActorID:       input.ActorID,
			ActorUsername: actorUsername,
			Action:        auditActionDelete,
			Resource:      auditResourceUser,
			ResourceID:    existing.ID.String(),
			Status:        auditStatusSuccess,
			IPAddress:     input.Meta.IPAddress,
			UserAgent:     input.Meta.UserAgent,
			Metadata: map[string]any{
				"username":     existing.Username,
				"removedRoles": roleCodes(currentRoles),
			},
		})
	})
}

// GetProfile returns the authenticated user's profile including localized roles and signed avatar URL.
func (s *Service) GetProfile(ctx context.Context, userID, currentSessionID uuid.UUID) (Profile, error) {
	detail, err := s.store.GetUserDetailByID(ctx, userID)
	if errors.Is(err, errUserNotFound) {
		return Profile{}, apperr.Unauthorized(apperr.CodeUnauthorized)
	}
	if err != nil {
		return Profile{}, apperr.Internal(fmt.Errorf("get profile %s: %w", userID, err))
	}
	if detail.Status == StatusDisabled {
		return Profile{}, accountDisabledError()
	}
	if err := s.ensureActiveSession(ctx, userID, currentSessionID, false); err != nil {
		return Profile{}, err
	}

	slice := []UserDetail{detail}
	if err := s.attachUserRoles(ctx, slice); err != nil {
		return Profile{}, err
	}
	return s.toProfile(ctx, slice[0]), nil
}

// UpdateProfile updates the authenticated user's display name, email, and/or avatar with FOR SHARE file locking.
func (s *Service) UpdateProfile(ctx context.Context, input UpdateProfileInput) (Profile, error) {
	nextEmail, nextDisplay, fieldErrs := validateOptionalEmailAndDisplay(input.Email, input.DisplayName)
	if len(fieldErrs) > 0 {
		return Profile{}, apperr.Validation(fieldErrs)
	}

	var updated UserDetail
	err := s.withinTx(ctx, func(txCtx context.Context) error {
		existing, err := s.lockActiveProfileUser(txCtx, input.UserID, input.CurrentSessionID, "profile update")
		if err != nil {
			return err
		}

		finalEmail, finalDisplay := resolveFinalEmailAndDisplay(existing, nextEmail, nextDisplay)
		finalAvatar, avatarChanged, err := s.resolveProfileAvatar(txCtx, existing, input.AvatarFileID, input.ClearAvatar)
		if err != nil {
			return err
		}

		if err := s.store.UpdateOwnProfile(txCtx, updateOwnProfileRecord{
			ID:           existing.ID,
			Email:        finalEmail,
			DisplayName:  finalDisplay,
			AvatarFileID: finalAvatar,
		}); err != nil {
			if errors.Is(err, errUserNotFound) {
				return apperr.Unauthorized(apperr.CodeUnauthorized)
			}
			if errors.Is(err, errEmailConflict) {
				return emailExistsError()
			}
			return apperr.Internal(fmt.Errorf("update own profile %s: %w", existing.ID, err))
		}

		detail, detailErr := s.store.GetUserDetailByID(txCtx, existing.ID)
		if detailErr != nil {
			return apperr.Internal(fmt.Errorf("reload profile %s: %w", existing.ID, detailErr))
		}
		slice := []UserDetail{detail}
		if err := s.attachUserRoles(txCtx, slice); err != nil {
			return err
		}
		updated = slice[0]

		changed := make([]string, 0, 3)
		if nextEmail != nil && finalEmail != existing.Email {
			changed = append(changed, "email")
		}
		if nextDisplay != nil && finalDisplay != existing.DisplayName {
			changed = append(changed, "displayName")
		}
		if avatarChanged {
			changed = append(changed, "avatarFileId")
		}
		actorID := existing.ID
		return s.recordAuditEntry(txCtx, AuditEntry{
			ActorID:       &actorID,
			ActorUsername: existing.Username,
			Action:        auditActionProfileUpdate,
			Resource:      auditResourceUser,
			ResourceID:    existing.ID.String(),
			Status:        auditStatusSuccess,
			IPAddress:     input.Meta.IPAddress,
			UserAgent:     input.Meta.UserAgent,
			Metadata: map[string]any{
				"changedFields": changed,
			},
		})
	})
	if err != nil {
		return Profile{}, err
	}
	return s.toProfile(ctx, updated), nil
}

// ChangePassword verifies the caller's current password, stores the new Argon2id hash,
// and optionally revokes all other active sessions.
func (s *Service) ChangePassword(ctx context.Context, input ChangePasswordInput) (ChangePasswordResult, error) {
	if err := validateChangePasswordInput(input); err != nil {
		return ChangePasswordResult{}, err
	}

	creds, err := s.verifyCurrentPasswordSnapshot(ctx, input)
	if err != nil {
		return ChangePasswordResult{}, err
	}

	newHash, hashErr := s.hasher.Hash(input.NewPassword)
	if errors.Is(hashErr, security.ErrInvalidPasswordLength) {
		return ChangePasswordResult{}, apperr.Validation([]apperr.FieldError{{
			Field:   "newPassword",
			Message: ErrCodeInvalidPassword,
		}})
	}
	if hashErr != nil {
		return ChangePasswordResult{}, apperr.Internal(fmt.Errorf("hash new password %s: %w", input.UserID, hashErr))
	}

	revokeOthers := true
	if input.RevokeOtherSessions != nil {
		revokeOthers = *input.RevokeOtherSessions
	}

	var (
		revokedCount      int64
		stalePasswordHash bool
	)
	err = s.withinTx(ctx, func(txCtx context.Context) error {
		locked, lockErr := s.store.GetUserPasswordHashByIDForUpdate(txCtx, input.UserID)
		if errors.Is(lockErr, errUserNotFound) {
			return apperr.Unauthorized(apperr.CodeUnauthorized)
		}
		if lockErr != nil {
			return apperr.Internal(fmt.Errorf("lock user %s for password change: %w", input.UserID, lockErr))
		}
		if locked.Status == StatusDisabled {
			return accountDisabledError()
		}
		if err := s.ensureActiveSession(txCtx, locked.ID, input.CurrentSessionID, true); err != nil {
			return err
		}
		if locked.PasswordHash != creds.PasswordHash {
			stalePasswordHash = true
			return invalidCurrentPasswordError()
		}

		actorID := locked.ID
		if err := s.store.UpdateUserPasswordWithActor(txCtx, locked.ID, newHash, &actorID); err != nil {
			return apperr.Internal(fmt.Errorf("update own password %s: %w", locked.ID, err))
		}
		if revokeOthers && s.sessions != nil {
			n, revErr := s.sessions.RevokeOtherUserSessions(txCtx, locked.ID, input.CurrentSessionID)
			if revErr != nil {
				return wrapAppErr(revErr)
			}
			revokedCount = n
		}
		return s.recordAuditEntry(txCtx, AuditEntry{
			ActorID:       &actorID,
			ActorUsername: locked.Username,
			Action:        auditActionProfileChangePassword,
			Resource:      auditResourceUser,
			ResourceID:    locked.ID.String(),
			Status:        auditStatusSuccess,
			IPAddress:     input.Meta.IPAddress,
			UserAgent:     input.Meta.UserAgent,
			Metadata: map[string]any{
				"revokeOtherSessions": revokeOthers,
				"revokedSessionCount": revokedCount,
			},
		})
	})
	if err != nil {
		if stalePasswordHash {
			s.recordPasswordFailureAudit(ctx, creds, input.Meta, "password_changed_concurrently")
		}
		return ChangePasswordResult{}, err
	}
	return ChangePasswordResult{RevokedSessions: revokedCount}, nil
}

// ListProfileSessions returns all active sessions owned by userID with the current session pinned first.
func (s *Service) ListProfileSessions(ctx context.Context, userID, currentSessionID uuid.UUID) ([]UserSession, error) {
	user, err := s.store.GetUserByID(ctx, userID)
	if errors.Is(err, errUserNotFound) {
		return nil, apperr.Unauthorized(apperr.CodeUnauthorized)
	}
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("load user %s for sessions: %w", userID, err))
	}
	if user.Status == StatusDisabled {
		return nil, accountDisabledError()
	}
	if err := s.ensureActiveSession(ctx, userID, currentSessionID, false); err != nil {
		return nil, err
	}
	if s.sessions == nil {
		return make([]UserSession, 0), nil
	}

	sessions, err := s.sessions.ListActiveUserSessions(ctx, userID)
	if err != nil {
		return nil, wrapAppErr(err)
	}
	out := make([]UserSession, len(sessions))
	for i, sess := range sessions {
		sess.IsCurrent = currentSessionID != uuid.Nil && sess.ID == currentSessionID
		out[i] = sess
	}
	slices.SortStableFunc(out, func(a, b UserSession) int {
		if a.IsCurrent == b.IsCurrent {
			return 0
		}
		if a.IsCurrent {
			return -1
		}
		return 1
	})
	return out, nil
}

// RevokeProfileSession revokes a single non-current active session belonging to the caller.
func (s *Service) RevokeProfileSession(ctx context.Context, input RevokeSessionInput) error {
	return s.withinTx(ctx, func(txCtx context.Context) error {
		user, err := s.lockActiveProfileUser(txCtx, input.UserID, input.CurrentSessionID, "session revoke")
		if err != nil {
			return err
		}
		if input.SessionID == uuid.Nil {
			return profileSessionNotFoundError()
		}
		if input.CurrentSessionID != uuid.Nil && input.SessionID == input.CurrentSessionID {
			return currentSessionRevokeError()
		}
		if s.sessions == nil {
			return profileSessionNotFoundError()
		}

		revoked, revErr := s.sessions.RevokeUserSession(txCtx, user.ID, input.SessionID)
		if revErr != nil {
			return wrapAppErr(revErr)
		}
		if !revoked {
			return profileSessionNotFoundError()
		}
		actorID := user.ID
		return s.recordAuditEntry(txCtx, AuditEntry{
			ActorID:       &actorID,
			ActorUsername: user.Username,
			Action:        auditActionProfileSessionRevoke,
			Resource:      auditResourceSession,
			ResourceID:    input.SessionID.String(),
			Status:        auditStatusSuccess,
			IPAddress:     input.Meta.IPAddress,
			UserAgent:     input.Meta.UserAgent,
		})
	})
}

// RevokeOtherProfileSessions revokes every active session belonging to the caller except CurrentSessionID.
func (s *Service) RevokeOtherProfileSessions(ctx context.Context, input RevokeOtherSessionsInput) (int64, error) {
	var count int64
	err := s.withinTx(ctx, func(txCtx context.Context) error {
		user, err := s.lockActiveProfileUser(txCtx, input.UserID, input.CurrentSessionID, "bulk session revoke")
		if err != nil {
			return err
		}
		if s.sessions == nil {
			return nil
		}

		n, revErr := s.sessions.RevokeOtherUserSessions(txCtx, user.ID, input.CurrentSessionID)
		if revErr != nil {
			return wrapAppErr(revErr)
		}
		count = n
		if count == 0 {
			return nil
		}
		actorID := user.ID
		return s.recordAuditEntry(txCtx, AuditEntry{
			ActorID:       &actorID,
			ActorUsername: user.Username,
			Action:        auditActionProfileSessionsRevokeAll,
			Resource:      auditResourceSession,
			Status:        auditStatusSuccess,
			IPAddress:     input.Meta.IPAddress,
			UserAgent:     input.Meta.UserAgent,
			Metadata: map[string]any{
				"revokedCount": count,
			},
		})
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

type validatedCreateUser struct {
	username    string
	email       string
	displayName string
	status      string
	roleIDs     []uuid.UUID
	roles       []RoleRef
}

func (s *Service) validateCreateUserInput(ctx context.Context, input CreateUserInput) (validatedCreateUser, error) {
	var fieldErrs []apperr.FieldError
	username, ok := normalizeUsername(input.Username)
	if !ok {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "username", Message: ErrCodeInvalidUsername})
	}
	email, ok := normalizeEmail(input.Email)
	if !ok {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "email", Message: ErrCodeInvalidEmail})
	}
	displayName, ok := normalizeDisplayName(input.DisplayName, username)
	if !ok {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "displayName", Message: ErrCodeInvalidDisplay})
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = StatusActive
	}
	if status != StatusActive && status != StatusDisabled {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "status", Message: ErrCodeInvalidStatus})
	}
	if err := security.ValidatePasswordLength(input.Password); errors.Is(err, security.ErrInvalidPasswordLength) {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "password", Message: ErrCodeInvalidPassword})
	}

	roleIDs := dedupeUUIDs(input.RoleIDs)
	resolvedRoles, rolesValid, roleErr := s.resolveValidatedRoles(ctx, roleIDs)
	if roleErr != nil {
		return validatedCreateUser{}, roleErr
	}
	if !rolesValid {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "roleIds", Message: ErrCodeInvalidRole})
	}

	if len(fieldErrs) > 0 {
		return validatedCreateUser{}, apperr.Validation(fieldErrs)
	}
	return validatedCreateUser{
		username:    username,
		email:       email,
		displayName: displayName,
		status:      status,
		roleIDs:     roleIDs,
		roles:       sortRoleRefs(resolvedRoles),
	}, nil
}

type validatedUpdateUser struct {
	email       *string
	displayName *string
	status      *string
	updateRoles bool
	roleIDs     []uuid.UUID
	roles       []RoleRef
}

func (s *Service) validateUpdateUserInput(ctx context.Context, input UpdateUserInput) (validatedUpdateUser, error) {
	nextEmail, nextDisplay, fieldErrs := validateOptionalEmailAndDisplay(input.Email, input.DisplayName)

	var nextStatus *string
	if input.Status != nil {
		st := strings.TrimSpace(*input.Status)
		if st != StatusActive && st != StatusDisabled {
			fieldErrs = append(fieldErrs, apperr.FieldError{Field: "status", Message: ErrCodeInvalidStatus})
		} else {
			nextStatus = &st
		}
	}

	if input.Password != nil {
		if err := security.ValidatePasswordLength(*input.Password); errors.Is(err, security.ErrInvalidPasswordLength) {
			fieldErrs = append(fieldErrs, apperr.FieldError{Field: "password", Message: ErrCodeInvalidPassword})
		}
	}

	var (
		nextRoleIDs   []uuid.UUID
		resolvedRoles []RoleRef
		updateRoles   bool
	)
	if input.RoleIDs != nil {
		updateRoles = true
		nextRoleIDs = dedupeUUIDs(*input.RoleIDs)
		var (
			rolesValid bool
			roleErr    error
		)
		resolvedRoles, rolesValid, roleErr = s.resolveValidatedRoles(ctx, nextRoleIDs)
		if roleErr != nil {
			return validatedUpdateUser{}, roleErr
		}
		if !rolesValid {
			fieldErrs = append(fieldErrs, apperr.FieldError{Field: "roleIds", Message: ErrCodeInvalidRole})
		}
	}

	if len(fieldErrs) > 0 {
		return validatedUpdateUser{}, apperr.Validation(fieldErrs)
	}
	return validatedUpdateUser{
		email:       nextEmail,
		displayName: nextDisplay,
		status:      nextStatus,
		updateRoles: updateRoles,
		roleIDs:     nextRoleIDs,
		roles:       sortRoleRefs(resolvedRoles),
	}, nil
}

type userUpdateGuardState struct {
	existing     User
	currentRoles []RoleRef
	finalRoles   []RoleRef
	finalStatus  string
	actorID      *uuid.UUID
}

func (s *Service) enforceUserUpdateGuards(ctx context.Context, st userUpdateGuardState) error {
	targetIsSuper := hasRoleCode(st.currentRoles, superAdminRoleCode)
	nextIsSuper := hasRoleCode(st.finalRoles, superAdminRoleCode)

	if st.actorID != nil && *st.actorID == st.existing.ID {
		if st.finalStatus == StatusDisabled {
			return selfOperationError()
		}
		if targetIsSuper && !nextIsSuper {
			return selfOperationError()
		}
	}

	if targetIsSuper || nextIsSuper {
		isSuper, err := s.actorIsSuperAdmin(ctx, st.actorID)
		if err != nil {
			return err
		}
		if !isSuper {
			return superAdminProtectedError()
		}
	}

	wasActiveSuper := st.existing.Status == StatusActive && targetIsSuper
	willBeActiveSuper := st.finalStatus == StatusActive && nextIsSuper
	if wasActiveSuper && !willBeActiveSuper {
		if err := s.ensureOtherActiveSuperAdmin(ctx, st.existing.ID); err != nil {
			return err
		}
	}
	return nil
}

type userUpdateAuditDiff struct {
	existing      User
	finalEmail    string
	finalDisplay  string
	finalStatus   string
	emailSet      bool
	displaySet    bool
	statusSet     bool
	passwordReset bool
	rolesChanged  bool
	currentRoles  []RoleRef
	finalRoles    []RoleRef
}

func buildUserUpdateAuditMeta(d userUpdateAuditDiff) map[string]any {
	changed := make([]string, 0, 5)
	meta := map[string]any{"username": d.existing.Username}
	if d.emailSet && d.finalEmail != d.existing.Email {
		changed = append(changed, "email")
	}
	if d.displaySet && d.finalDisplay != d.existing.DisplayName {
		changed = append(changed, "displayName")
	}
	if d.statusSet && d.finalStatus != d.existing.Status {
		changed = append(changed, "status")
		meta["status"] = map[string]string{"from": d.existing.Status, "to": d.finalStatus}
	}
	if d.passwordReset {
		changed = append(changed, "password")
		meta["passwordReset"] = true
	}
	if d.rolesChanged {
		changed = append(changed, "roles")
		meta["roles"] = map[string]any{
			"before": roleCodes(d.currentRoles),
			"after":  roleCodes(d.finalRoles),
		}
	}
	meta["changedFields"] = changed
	return meta
}

func validateOptionalEmailAndDisplay(email, displayName *string) (*string, *string, []apperr.FieldError) {
	var (
		fieldErrs   []apperr.FieldError
		nextEmail   *string
		nextDisplay *string
	)
	if email != nil {
		normalized, ok := normalizeEmail(*email)
		if !ok {
			fieldErrs = append(fieldErrs, apperr.FieldError{Field: "email", Message: ErrCodeInvalidEmail})
		} else {
			nextEmail = &normalized
		}
	}
	if displayName != nil {
		trimmed := strings.TrimSpace(*displayName)
		if !isValidText(trimmed) || utf8.RuneCountInString(trimmed) > maxDisplayRunes {
			fieldErrs = append(fieldErrs, apperr.FieldError{Field: "displayName", Message: ErrCodeInvalidDisplay})
		} else {
			nextDisplay = &trimmed
		}
	}
	return nextEmail, nextDisplay, fieldErrs
}

func resolveFinalEmailAndDisplay(existing User, nextEmail, nextDisplay *string) (string, string) {
	finalEmail := existing.Email
	if nextEmail != nil {
		finalEmail = *nextEmail
	}
	finalDisplay := existing.DisplayName
	if nextDisplay != nil {
		if *nextDisplay == "" {
			finalDisplay = existing.Username
		} else {
			finalDisplay = *nextDisplay
		}
	}
	return finalEmail, finalDisplay
}

func (s *Service) lockTargetAndVerifyActor(
	txCtx context.Context,
	targetID uuid.UUID,
	actorID *uuid.UUID,
	actorSessionID uuid.UUID,
	op string,
) (User, string, error) {
	if actorID != nil && *actorID == targetID {
		// Self-mutation locks the user row before FOR SHARE on user_sessions, matching /profile* lock order.
		existing, err := s.lockActiveProfileUser(txCtx, targetID, actorSessionID, op)
		if err != nil {
			return User{}, "", err
		}
		return existing, existing.Username, nil
	}

	actorUsername, err := s.verifyActiveActor(txCtx, actorID, actorSessionID, true)
	if err != nil {
		return User{}, "", err
	}

	existing, lockErr := s.store.GetUserByIDForUpdate(txCtx, targetID)
	if errors.Is(lockErr, errUserNotFound) {
		return User{}, "", userNotFoundError()
	}
	if lockErr != nil {
		return User{}, "", apperr.Internal(fmt.Errorf("lock user %s for %s: %w", targetID, op, lockErr))
	}
	return existing, actorUsername, nil
}

func (s *Service) verifyActiveActor(ctx context.Context, actorID *uuid.UUID, sessionID uuid.UUID, lockSession bool) (string, error) {
	if actorID == nil || *actorID == uuid.Nil {
		return "", nil
	}
	actor, err := s.store.GetUserByID(ctx, *actorID)
	if errors.Is(err, errUserNotFound) {
		return "", apperr.Unauthorized(apperr.CodeUnauthorized)
	}
	if err != nil {
		return "", apperr.Internal(fmt.Errorf("load actor %s: %w", *actorID, err))
	}
	if actor.Status == StatusDisabled {
		return "", accountDisabledError()
	}
	if err := s.ensureActiveSession(ctx, actor.ID, sessionID, lockSession); err != nil {
		return "", err
	}
	return actor.Username, nil
}

func (s *Service) lockActiveProfileUser(txCtx context.Context, userID, currentSessionID uuid.UUID, op string) (User, error) {
	user, lockErr := s.store.GetUserByIDForUpdate(txCtx, userID)
	if errors.Is(lockErr, errUserNotFound) {
		return User{}, apperr.Unauthorized(apperr.CodeUnauthorized)
	}
	if lockErr != nil {
		return User{}, apperr.Internal(fmt.Errorf("lock user %s for %s: %w", userID, op, lockErr))
	}
	if user.Status == StatusDisabled {
		return User{}, accountDisabledError()
	}
	if err := s.ensureActiveSession(txCtx, user.ID, currentSessionID, true); err != nil {
		return User{}, err
	}
	return user, nil
}

func (s *Service) resolveProfileAvatar(
	txCtx context.Context,
	existing User,
	requestedID *uuid.UUID,
	clearAvatar bool,
) (*uuid.UUID, bool, error) {
	finalAvatar := cloneUUIDPtr(existing.AvatarFileID)
	switch {
	case clearAvatar:
		return nil, existing.AvatarFileID != nil, nil
	case requestedID != nil && !sameUUIDPtr(existing.AvatarFileID, requestedID):
		if s.files == nil {
			return nil, false, apperr.Validation([]apperr.FieldError{{
				Field:   "avatarFileId",
				Message: ErrCodeProfileInvalidAvatar,
			}})
		}
		if err := s.files.ValidateAndLockAvatarFile(txCtx, *requestedID, existing.ID); err != nil {
			if appErr, ok := apperr.As(err); ok && appErr.Status == http.StatusNotFound {
				return nil, false, apperr.Validation([]apperr.FieldError{{
					Field:   "avatarFileId",
					Message: ErrCodeProfileInvalidAvatar,
				}})
			}
			return nil, false, wrapAppErr(err)
		}
		return cloneUUIDPtr(requestedID), true, nil
	default:
		return finalAvatar, false, nil
	}
}

func validateChangePasswordInput(input ChangePasswordInput) error {
	var fieldErrs []apperr.FieldError
	if input.CurrentPassword == "" {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "currentPassword", Message: "VALIDATION_REQUIRED"})
	}
	if err := security.ValidatePasswordLength(input.NewPassword); errors.Is(err, security.ErrInvalidPasswordLength) {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "newPassword", Message: ErrCodeInvalidPassword})
	}
	if len(fieldErrs) > 0 {
		return apperr.Validation(fieldErrs)
	}
	return nil
}

func (s *Service) verifyCurrentPasswordSnapshot(ctx context.Context, input ChangePasswordInput) (userPasswordRecord, error) {
	creds, err := s.store.GetUserPasswordHashByID(ctx, input.UserID)
	if errors.Is(err, errUserNotFound) {
		return userPasswordRecord{}, apperr.Unauthorized(apperr.CodeUnauthorized)
	}
	if err != nil {
		return userPasswordRecord{}, apperr.Internal(fmt.Errorf("load password hash %s: %w", input.UserID, err))
	}
	if creds.Status == StatusDisabled {
		return userPasswordRecord{}, accountDisabledError()
	}
	if err := s.ensureActiveSession(ctx, creds.ID, input.CurrentSessionID, false); err != nil {
		return userPasswordRecord{}, err
	}

	matched, _, verifyErr := s.hasher.Verify(input.CurrentPassword, creds.PasswordHash)
	if verifyErr != nil {
		s.log.Error("stored password hash invalid", "user_id", creds.ID.String(), "error", verifyErr)
	}
	if verifyErr != nil || !matched {
		s.recordPasswordFailureAudit(ctx, creds, input.Meta, "invalid_current_password")
		return userPasswordRecord{}, invalidCurrentPasswordError()
	}

	if input.NewPassword == input.CurrentPassword {
		return userPasswordRecord{}, apperr.Validation([]apperr.FieldError{{
			Field:   "newPassword",
			Message: ErrCodeProfilePasswordUnchanged,
		}})
	}
	return creds, nil
}

func (s *Service) recordPasswordFailureAudit(ctx context.Context, creds userPasswordRecord, meta RequestMeta, reason string) {
	actorID := creds.ID
	s.recordAuditBestEffort(ctx, AuditEntry{
		ActorID:       &actorID,
		ActorUsername: creds.Username,
		Action:        auditActionProfileChangePassword,
		Resource:      auditResourceUser,
		ResourceID:    creds.ID.String(),
		Status:        auditStatusFailure,
		IPAddress:     meta.IPAddress,
		UserAgent:     meta.UserAgent,
		Metadata: map[string]any{
			"reason": reason,
		},
	})
}

func (s *Service) ensureActiveSession(ctx context.Context, userID, sessionID uuid.UUID, lock bool) error {
	if s.sessions == nil {
		return nil
	}
	if sessionID == uuid.Nil {
		return apperr.Unauthorized(apperr.CodeUnauthorized)
	}
	var (
		active bool
		err    error
	)
	if lock {
		active, err = s.sessions.LockActiveSessionForShare(ctx, userID, sessionID)
	} else {
		active, err = s.sessions.IsSessionActive(ctx, userID, sessionID)
	}
	if err != nil {
		return wrapAppErr(err)
	}
	if !active {
		return apperr.Unauthorized(apperr.CodeUnauthorized)
	}
	return nil
}

func (s *Service) toProfile(ctx context.Context, detail UserDetail) Profile {
	var avatarURL *string
	if s.files != nil && detail.AvatarFileID != nil && *detail.AvatarFileID != uuid.Nil {
		url, err := s.files.SignAvatarURL(ctx, *detail.AvatarFileID)
		if err != nil {
			s.log.Warn("sign profile avatar url failed", "user_id", detail.ID.String(), "file_id", detail.AvatarFileID.String(), "error", err)
		} else if strings.TrimSpace(url) != "" {
			avatarURL = &url
		}
	}
	return Profile{
		ID:           detail.ID,
		Username:     detail.Username,
		Email:        detail.Email,
		DisplayName:  detail.DisplayName,
		AvatarFileID: cloneUUIDPtr(detail.AvatarFileID),
		AvatarURL:    avatarURL,
		Status:       detail.Status,
		LastLoginAt:  detail.LastLoginAt,
		Roles:        nonNilRoleRefs(detail.Roles),
		CreatedAt:    detail.CreatedAt,
		UpdatedAt:    detail.UpdatedAt,
	}
}

func (s *Service) resolveValidatedRoles(ctx context.Context, ids []uuid.UUID) ([]RoleRef, bool, error) {
	if len(ids) == 0 {
		return make([]RoleRef, 0), true, nil
	}
	if slices.Contains(ids, uuid.Nil) || s.roles == nil {
		return nil, false, nil
	}
	refs, err := s.roles.ResolveRoles(ctx, ids)
	if err != nil {
		return nil, false, wrapAppErr(err)
	}
	if len(refs) != len(ids) {
		return nil, false, nil
	}
	return refs, true, nil
}

func (s *Service) attachUserRoles(ctx context.Context, users []UserDetail) error {
	if len(users) == 0 || s.roles == nil {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.ID)
	}
	roleMap, err := s.roles.ListRolesByUserIDs(ctx, ids)
	if err != nil {
		return wrapAppErr(err)
	}
	for i := range users {
		users[i].Roles = nonNilRoleRefs(roleMap[users[i].ID])
	}
	return nil
}

func (s *Service) getUserRoles(ctx context.Context, userID uuid.UUID) ([]RoleRef, error) {
	if s.roles == nil {
		return make([]RoleRef, 0), nil
	}
	m, err := s.roles.ListRolesByUserIDs(ctx, []uuid.UUID{userID})
	if err != nil {
		return nil, wrapAppErr(err)
	}
	return nonNilRoleRefs(m[userID]), nil
}

func (s *Service) actorIsSuperAdmin(ctx context.Context, actorID *uuid.UUID) (bool, error) {
	if actorID == nil || *actorID == uuid.Nil || s.roles == nil {
		return false, nil
	}
	roles, err := s.getUserRoles(ctx, *actorID)
	if err != nil {
		return false, err
	}
	return hasRoleCode(roles, superAdminRoleCode), nil
}

func (s *Service) ensureOtherActiveSuperAdmin(ctx context.Context, excludeUserID uuid.UUID) error {
	if s.roles == nil {
		return nil
	}
	ids, err := s.roles.ListUserIDsByRoleCode(ctx, superAdminRoleCode)
	if err != nil {
		return wrapAppErr(err)
	}
	remaining := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id != excludeUserID {
			remaining = append(remaining, id)
		}
	}
	if len(remaining) == 0 {
		return lastSuperAdminError()
	}
	count, err := s.store.CountActiveUsersByIDs(ctx, remaining)
	if err != nil {
		return apperr.Internal(fmt.Errorf("count remaining active super admins: %w", err))
	}
	if count == 0 {
		return lastSuperAdminError()
	}
	return nil
}

func (s *Service) recordAuditEntry(ctx context.Context, entry AuditEntry) error {
	if s.audit == nil {
		return nil
	}
	if err := s.audit.Record(ctx, entry); err != nil {
		return apperr.Internal(fmt.Errorf("record audit %s: %w", entry.Action, err))
	}
	return nil
}

func (s *Service) recordAuditBestEffort(ctx context.Context, entry AuditEntry) {
	if s.audit == nil {
		return
	}
	if err := s.audit.Record(ctx, entry); err != nil {
		s.log.Warn("record identity audit best effort failed", "action", entry.Action, "error", err)
	}
}

func cloneUUIDPtr(id *uuid.UUID) *uuid.UUID {
	if id == nil || *id == uuid.Nil {
		return nil
	}
	copied := *id
	return &copied
}

func sameUUIDPtr(a, b *uuid.UUID) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func (s *Service) withinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	var err error
	if s.tx == nil {
		err = fn(ctx)
	} else {
		err = s.tx.WithinTx(ctx, fn)
	}
	return wrapAppErr(err)
}

func wrapAppErr(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := apperr.As(err); ok {
		return err
	}
	return apperr.Internal(err)
}

func isValidText(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

func normalizeUsername(raw string) (string, bool) {
	u := strings.ToLower(strings.TrimSpace(raw))
	if len(u) < minUsernameLen || len(u) > maxUsernameLen || !usernamePattern.MatchString(u) {
		return "", false
	}
	return u, true
}

func normalizeEmail(raw string) (string, bool) {
	e := strings.ToLower(strings.TrimSpace(raw))
	if e == "" || len(e) > maxEmailLen {
		return "", false
	}
	addr, err := mail.ParseAddress(e)
	if err != nil || addr.Address != e || !strings.Contains(e, "@") {
		return "", false
	}
	return e, true
}

func normalizeDisplayName(raw, fallback string) (string, bool) {
	d := strings.TrimSpace(raw)
	if !isValidText(d) || utf8.RuneCountInString(d) > maxDisplayRunes {
		return "", false
	}
	if d == "" {
		return fallback, true
	}
	return d, true
}

func normalizeStatusesFilter(in []string) []string {
	if len(in) == 0 {
		return make([]string, 0)
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == StatusActive || s == StatusDisabled {
			if _, ok := seen[s]; !ok {
				seen[s] = struct{}{}
				out = append(out, s)
			}
		}
	}
	return out
}

func isAllowedUserSort(sort string) bool {
	switch sort {
	case "created_at", "updated_at", "username", "last_login_at":
		return true
	default:
		return false
	}
}

func normalizeUserSort(sort string) string {
	if isAllowedUserSort(sort) {
		return sort
	}
	return "created_at"
}

func normalizeUserOrder(order string) string {
	if strings.EqualFold(order, "asc") {
		return "asc"
	}
	return "desc"
}

func hasRoleCode(roles []RoleRef, code string) bool {
	for _, r := range roles {
		if r.Code == code {
			return true
		}
	}
	return false
}

func sortRoleRefs(in []RoleRef) []RoleRef {
	if len(in) == 0 {
		return make([]RoleRef, 0)
	}
	out := slices.Clone(in)
	slices.SortFunc(out, func(a, b RoleRef) int {
		if a.IsSystem != b.IsSystem {
			if a.IsSystem {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.Code, b.Code)
	})
	return out
}

func sameRoleSet(a, b []RoleRef) bool {
	if len(a) != len(b) {
		return false
	}
	aIDs := make([]string, len(a))
	for i, r := range a {
		aIDs[i] = r.ID.String()
	}
	bIDs := make([]string, len(b))
	for i, r := range b {
		bIDs[i] = r.ID.String()
	}
	slices.Sort(aIDs)
	slices.Sort(bIDs)
	return slices.Equal(aIDs, bIDs)
}

func roleCodes(roles []RoleRef) []string {
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		out = append(out, r.Code)
	}
	slices.Sort(out)
	return out
}

func nonNilRoleRefs(in []RoleRef) []RoleRef {
	if in == nil {
		return make([]RoleRef, 0)
	}
	return in
}

func dedupeUUIDs(in []uuid.UUID) []uuid.UUID {
	if len(in) == 0 {
		return make([]uuid.UUID, 0)
	}
	seen := make(map[uuid.UUID]struct{}, len(in))
	out := make([]uuid.UUID, 0, len(in))
	for _, id := range in {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}
