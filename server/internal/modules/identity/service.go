package identity

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/platform/database"
	"tianshu/internal/platform/i18n"
	"tianshu/internal/platform/security"
	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/pagination"
)

const (
	// usersSuperAdminGuardKey serializes concurrent mutations that could remove the last active super_admin.
	usersSuperAdminGuardKey = int64(0x7469616e73687502)

	superAdminRoleCode = "super_admin"
	auditResourceUser  = "user"
	auditActionCreate  = "user.create"
	auditActionUpdate  = "user.update"
	auditActionDelete  = "user.delete"
	auditStatusSuccess = "success"

	minUsernameLen  = 3
	maxUsernameLen  = 64
	maxEmailLen     = 255
	maxDisplayRunes = 100
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9._-]+$`)

// PasswordHasher hashes plaintext passwords for user storage.
type PasswordHasher interface {
	Hash(password string) (string, error)
}

// ServiceDeps holds optional cross-cutting dependencies for identity.Service.
type ServiceDeps struct {
	Tx       TxRunner
	Roles    RoleManager
	Sessions SessionRevoker
	Audit    AuditRecorder
	I18n     *i18n.Bundle
	Log      *slog.Logger
}

// Service owns user lifecycle persistence and invariants.
type Service struct {
	store    store
	hasher   PasswordHasher
	tx       TxRunner
	roles    RoleManager
	sessions SessionRevoker
	audit    AuditRecorder
	i18n     *i18n.Bundle
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
		audit:    d.Audit,
		i18n:     d.I18n,
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
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "username",
			Message: ErrCodeInvalidUsername,
		})
	}
	email, ok := normalizeEmail(input.Email)
	if !ok {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "email",
			Message: ErrCodeInvalidEmail,
		})
	}
	displayName, ok := normalizeDisplayName(input.DisplayName, username)
	if !ok {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "displayName",
			Message: ErrCodeInvalidDisplay,
		})
	}
	if len(fieldErrs) > 0 {
		return User{}, apperr.Validation(sortAndDedupeFieldErrors(fieldErrs))
	}

	hash, err := s.hasher.Hash(input.Password)
	if errors.Is(err, security.ErrInvalidPasswordLength) {
		return User{}, invalidPasswordError()
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

// ListUsers returns a paginated list of non-deleted users with their roles and audit actors.
func (s *Service) ListUsers(ctx context.Context, filter UserListFilter) (pagination.Result[UserDetail], error) {
	pageParams := pagination.Normalize(filter.Pagination.Page, filter.Pagination.PageSize)

	var (
		filterIDs bool
		userIDs   []uuid.UUID
	)
	if len(filter.RoleIDs) > 0 && s.roles != nil {
		filterIDs = true
		ids, err := s.roles.ListUserIDsByRoleIDs(ctx, dedupeUUIDs(filter.RoleIDs))
		if err != nil {
			return pagination.Result[UserDetail]{}, err
		}
		if len(ids) == 0 {
			return pagination.NewResult(make([]UserDetail, 0), 0, pageParams), nil
		}
		userIDs = ids
	}

	query := listUsersQuery{
		Q:         strings.TrimSpace(filter.Q),
		Statuses:  normalizeStatusesFilter(filter.Statuses),
		FilterIDs: filterIDs,
		IDs:       userIDs,
		Sort:      normalizeUserSort(filter.Sort),
		Order:     normalizeUserOrder(filter.Order),
		Offset:    int32(pageParams.Offset()),
		Limit:     int32(pageParams.Limit()),
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

	hash, hashErr := s.hasher.Hash(input.Password)
	if errors.Is(hashErr, security.ErrInvalidPasswordLength) {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "password", Message: ErrCodeInvalidPassword})
	} else if hashErr != nil {
		return UserDetail{}, apperr.Internal(fmt.Errorf("hash user password: %w", hashErr))
	}

	roleIDs := dedupeUUIDs(input.RoleIDs)
	resolvedRoles, roleErr := s.resolveValidatedRoles(ctx, roleIDs)
	if roleErr != nil {
		if appErr, isApp := apperr.As(roleErr); isApp && appErr.Code == ErrCodeInvalidRole {
			fieldErrs = append(fieldErrs, apperr.FieldError{Field: "roleIds", Message: ErrCodeInvalidRole})
		} else {
			return UserDetail{}, roleErr
		}
	}

	if len(fieldErrs) > 0 {
		return UserDetail{}, apperr.Validation(sortAndDedupeFieldErrors(fieldErrs))
	}

	if hasRoleCode(resolvedRoles, superAdminRoleCode) {
		isSuper, err := s.actorIsSuperAdmin(ctx, input.ActorID)
		if err != nil {
			return UserDetail{}, err
		}
		if !isSuper {
			return UserDetail{}, superAdminProtectedError()
		}
	}

	userID, err := uuid.NewV7()
	if err != nil {
		return UserDetail{}, apperr.Internal(fmt.Errorf("generate user uuid: %w", err))
	}

	var created UserDetail
	err = s.withinTx(ctx, func(txCtx context.Context) error {
		user, createErr := s.store.CreateUser(txCtx, createUserRecord{
			ID:           userID,
			Username:     username,
			Email:        email,
			PasswordHash: hash,
			DisplayName:  displayName,
			Status:       status,
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
			return apperr.Internal(fmt.Errorf("create user %s: %w", username, createErr))
		}

		if len(roleIDs) > 0 && s.roles != nil {
			if err := s.roles.ReplaceUserRoles(txCtx, user.ID, roleIDs); err != nil {
				return err
			}
		}

		detail, detailErr := s.store.GetUserDetailByID(txCtx, user.ID)
		if detailErr != nil {
			return apperr.Internal(fmt.Errorf("load created user %s: %w", user.ID, detailErr))
		}
		detail.Roles = nonNilRoleRefs(resolvedRoles)
		created = detail

		return s.recordAudit(txCtx, input.ActorID, input.Meta, auditActionCreate, user.ID.String(), map[string]any{
			"username": user.Username,
			"status":   user.Status,
			"roles":    roleCodes(resolvedRoles),
		})
	})
	if err != nil {
		return UserDetail{}, err
	}
	return created, nil
}

// UpdateUser updates a user's profile, status, password, and/or roles with super-admin and self-operation guards.
func (s *Service) UpdateUser(ctx context.Context, input UpdateUserInput) (UserDetail, error) {
	var fieldErrs []apperr.FieldError

	var nextEmail *string
	if input.Email != nil {
		normalized, ok := normalizeEmail(*input.Email)
		if !ok {
			fieldErrs = append(fieldErrs, apperr.FieldError{Field: "email", Message: ErrCodeInvalidEmail})
		} else {
			nextEmail = &normalized
		}
	}

	var nextDisplay *string
	if input.DisplayName != nil {
		trimmed := strings.TrimSpace(*input.DisplayName)
		if utf8.RuneCountInString(trimmed) > maxDisplayRunes {
			fieldErrs = append(fieldErrs, apperr.FieldError{Field: "displayName", Message: ErrCodeInvalidDisplay})
		} else {
			nextDisplay = &trimmed
		}
	}

	var nextStatus *string
	if input.Status != nil {
		st := strings.TrimSpace(*input.Status)
		if st != StatusActive && st != StatusDisabled {
			fieldErrs = append(fieldErrs, apperr.FieldError{Field: "status", Message: ErrCodeInvalidStatus})
		} else {
			nextStatus = &st
		}
	}

	var newPasswordHash *string
	if input.Password != nil {
		hash, hashErr := s.hasher.Hash(*input.Password)
		switch {
		case errors.Is(hashErr, security.ErrInvalidPasswordLength):
			fieldErrs = append(fieldErrs, apperr.FieldError{Field: "password", Message: ErrCodeInvalidPassword})
		case hashErr != nil:
			return UserDetail{}, apperr.Internal(fmt.Errorf("hash reset password: %w", hashErr))
		default:
			newPasswordHash = &hash
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
		var roleErr error
		resolvedRoles, roleErr = s.resolveValidatedRoles(ctx, nextRoleIDs)
		if roleErr != nil {
			if appErr, isApp := apperr.As(roleErr); isApp && appErr.Code == ErrCodeInvalidRole {
				fieldErrs = append(fieldErrs, apperr.FieldError{Field: "roleIds", Message: ErrCodeInvalidRole})
			} else {
				return UserDetail{}, roleErr
			}
		}
	}

	if len(fieldErrs) > 0 {
		return UserDetail{}, apperr.Validation(sortAndDedupeFieldErrors(fieldErrs))
	}

	var updated UserDetail
	err := s.withinTx(ctx, func(txCtx context.Context) error {
		if err := database.AcquireAdvisoryXactLock(txCtx, usersSuperAdminGuardKey); err != nil {
			return apperr.Internal(err)
		}

		existing, lockErr := s.store.GetUserByIDForUpdate(txCtx, input.UserID)
		if errors.Is(lockErr, errUserNotFound) {
			return userNotFoundError()
		}
		if lockErr != nil {
			return apperr.Internal(fmt.Errorf("lock user %s: %w", input.UserID, lockErr))
		}

		currentRoles, err := s.getUserRoles(txCtx, existing.ID)
		if err != nil {
			return err
		}
		targetIsSuper := hasRoleCode(currentRoles, superAdminRoleCode)

		finalRoles := currentRoles
		if updateRoles {
			finalRoles = nonNilRoleRefs(resolvedRoles)
		}
		nextIsSuper := hasRoleCode(finalRoles, superAdminRoleCode)

		finalStatus := existing.Status
		if nextStatus != nil {
			finalStatus = *nextStatus
		}

		isSelf := input.ActorID != nil && *input.ActorID == existing.ID
		if isSelf {
			if finalStatus == StatusDisabled {
				return selfOperationError()
			}
			if targetIsSuper && !nextIsSuper {
				return selfOperationError()
			}
		}

		if targetIsSuper || nextIsSuper {
			isSuper, err := s.actorIsSuperAdmin(txCtx, input.ActorID)
			if err != nil {
				return err
			}
			if !isSuper {
				return superAdminProtectedError()
			}
		}

		wasActiveSuper := existing.Status == StatusActive && targetIsSuper
		willBeActiveSuper := finalStatus == StatusActive && nextIsSuper
		if wasActiveSuper && !willBeActiveSuper {
			if err := s.ensureOtherActiveSuperAdmin(txCtx, existing.ID); err != nil {
				return err
			}
		}

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
			if err := s.store.UpdateUserPasswordByAdmin(txCtx, existing.ID, *newPasswordHash, input.ActorID); err != nil {
				return apperr.Internal(fmt.Errorf("reset user password %s: %w", existing.ID, err))
			}
		}

		if updateRoles && s.roles != nil {
			if err := s.roles.ReplaceUserRoles(txCtx, existing.ID, nextRoleIDs); err != nil {
				return err
			}
		}

		if (finalStatus == StatusDisabled || newPasswordHash != nil) && s.sessions != nil {
			if err := s.sessions.RevokeAllUserSessions(txCtx, existing.ID); err != nil {
				return err
			}
		}

		detail, detailErr := s.store.GetUserDetailByID(txCtx, existing.ID)
		if detailErr != nil {
			return apperr.Internal(fmt.Errorf("reload user %s: %w", existing.ID, detailErr))
		}
		detail.Roles = finalRoles
		updated = detail

		changed := make([]string, 0, 5)
		meta := map[string]any{"username": existing.Username}
		if nextEmail != nil && finalEmail != existing.Email {
			changed = append(changed, "email")
		}
		if nextDisplay != nil && finalDisplay != existing.DisplayName {
			changed = append(changed, "displayName")
		}
		if nextStatus != nil && finalStatus != existing.Status {
			changed = append(changed, "status")
			meta["status"] = map[string]string{"from": existing.Status, "to": finalStatus}
		}
		if newPasswordHash != nil {
			changed = append(changed, "password")
			meta["passwordReset"] = true
		}
		if updateRoles {
			changed = append(changed, "roles")
			meta["roles"] = map[string]any{
				"before": roleCodes(currentRoles),
				"after":  roleCodes(finalRoles),
			}
		}
		meta["changedFields"] = changed

		return s.recordAudit(txCtx, input.ActorID, input.Meta, auditActionUpdate, existing.ID.String(), meta)
	})
	if err != nil {
		return UserDetail{}, err
	}
	return updated, nil
}

// DeleteUser soft-deletes a user, clears role bindings, revokes all sessions, and writes an audit entry.
func (s *Service) DeleteUser(ctx context.Context, input DeleteUserInput) error {
	if input.ActorID != nil && *input.ActorID == input.UserID {
		return selfOperationError()
	}

	return s.withinTx(ctx, func(txCtx context.Context) error {
		if err := database.AcquireAdvisoryXactLock(txCtx, usersSuperAdminGuardKey); err != nil {
			return apperr.Internal(err)
		}

		existing, lockErr := s.store.GetUserByIDForUpdate(txCtx, input.UserID)
		if errors.Is(lockErr, errUserNotFound) {
			return userNotFoundError()
		}
		if lockErr != nil {
			return apperr.Internal(fmt.Errorf("lock user %s for delete: %w", input.UserID, lockErr))
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
				return err
			}
		}
		if s.sessions != nil {
			if err := s.sessions.RevokeAllUserSessions(txCtx, existing.ID); err != nil {
				return err
			}
		}
		if err := s.store.SoftDeleteUser(txCtx, existing.ID, input.ActorID); err != nil {
			return apperr.Internal(fmt.Errorf("soft delete user %s: %w", existing.ID, err))
		}

		return s.recordAudit(txCtx, input.ActorID, input.Meta, auditActionDelete, existing.ID.String(), map[string]any{
			"username":     existing.Username,
			"removedRoles": roleCodes(currentRoles),
		})
	})
}

func (s *Service) resolveValidatedRoles(ctx context.Context, ids []uuid.UUID) ([]RoleRef, error) {
	if len(ids) == 0 {
		return make([]RoleRef, 0), nil
	}
	if slices.Contains(ids, uuid.Nil) || s.roles == nil {
		return nil, apperr.New(422, ErrCodeInvalidRole)
	}
	refs, err := s.roles.ResolveRoles(ctx, ids)
	if err != nil {
		return nil, err
	}
	if len(refs) != len(ids) {
		return nil, apperr.New(422, ErrCodeInvalidRole)
	}
	return refs, nil
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
		return err
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
		return nil, err
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
		return err
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

func (s *Service) recordAudit(
	ctx context.Context,
	actorID *uuid.UUID,
	meta RequestMeta,
	action string,
	resourceID string,
	metadata map[string]any,
) error {
	if s.audit == nil {
		return nil
	}
	actorUsername := ""
	if actorID != nil && *actorID != uuid.Nil {
		if u, err := s.store.GetUserByID(ctx, *actorID); err == nil {
			actorUsername = u.Username
		} else {
			s.log.Warn("resolve actor username for identity audit failed", "actor_id", actorID.String(), "error", err)
		}
	}
	if err := s.audit.Record(ctx, AuditEntry{
		ActorID:       actorID,
		ActorUsername: actorUsername,
		Action:        action,
		Resource:      auditResourceUser,
		ResourceID:    resourceID,
		Status:        auditStatusSuccess,
		IPAddress:     meta.IPAddress,
		UserAgent:     meta.UserAgent,
		Metadata:      metadata,
	}); err != nil {
		return apperr.Internal(fmt.Errorf("record audit %s: %w", action, err))
	}
	return nil
}

func (s *Service) withinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if s.tx == nil {
		return fn(ctx)
	}
	return s.tx.WithinTx(ctx, fn)
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
	if utf8.RuneCountInString(d) > maxDisplayRunes {
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

func normalizeUserSort(sort string) string {
	switch sort {
	case "created_at", "updated_at", "username", "last_login_at":
		return sort
	default:
		return ""
	}
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

func sortAndDedupeFieldErrors(in []apperr.FieldError) []apperr.FieldError {
	byField := make(map[string]apperr.FieldError, len(in))
	for _, fe := range in {
		if _, exists := byField[fe.Field]; !exists {
			byField[fe.Field] = fe
		}
	}
	out := make([]apperr.FieldError, 0, len(byField))
	for _, fe := range byField {
		out = append(out, fe)
	}
	slices.SortFunc(out, func(a, b apperr.FieldError) int {
		return cmp.Compare(a.Field, b.Field)
	})
	return out
}
