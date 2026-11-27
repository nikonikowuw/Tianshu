package identity

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"tianshu/internal/platform/i18n"
	"tianshu/internal/shared/apperr"
)

// Handler exposes HTTP endpoints for user management and self-service profile settings.
type Handler struct {
	service *Service
	i18n    *i18n.Bundle
}

// NewHandler creates an identity Handler backed by service and i18n bundle.
func NewHandler(service *Service, bundle *i18n.Bundle) *Handler {
	return &Handler{
		service: service,
		i18n:    bundle,
	}
}

func (h *Handler) listUsers(c *gin.Context) {
	filter, err := bindListUsersRequest(c)
	if err != nil {
		_ = c.Error(err)
		return
	}

	result, err := h.service.ListUsers(c.Request.Context(), filter)
	if err != nil {
		_ = c.Error(err)
		return
	}

	locale := i18n.LocaleFromContext(c.Request.Context())
	c.JSON(http.StatusOK, toListUsersResponse(result, locale, h.i18n))
}

func (h *Handler) getUser(c *gin.Context) {
	userID, err := parseIDParam(c)
	if err != nil {
		_ = c.Error(err)
		return
	}

	user, err := h.service.GetUser(c.Request.Context(), userID)
	if err != nil {
		_ = c.Error(err)
		return
	}

	locale := i18n.LocaleFromContext(c.Request.Context())
	c.JSON(http.StatusOK, toUserResponse(user, locale, h.i18n))
}

func (h *Handler) createUser(c *gin.Context) {
	identity, meta, ok := extractAuthIdentityAndMeta(c)
	if !ok {
		_ = c.Error(apperr.Unauthorized(apperr.CodeUnauthorized))
		return
	}

	var req createUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(err)
		return
	}

	roleIDs, err := parseRoleIDStrings(req.RoleIDs)
	if err != nil {
		_ = c.Error(err)
		return
	}

	actorID := identity.UserID
	user, err := h.service.CreateUser(c.Request.Context(), CreateUserInput{
		Username:       req.Username,
		Email:          req.Email,
		DisplayName:    req.DisplayName,
		Password:       req.Password,
		Status:         req.Status,
		RoleIDs:        roleIDs,
		ActorID:        &actorID,
		ActorSessionID: identity.SessionID,
		Meta:           meta,
	})
	if err != nil {
		_ = c.Error(err)
		return
	}

	locale := i18n.LocaleFromContext(c.Request.Context())
	c.JSON(http.StatusCreated, toUserResponse(user, locale, h.i18n))
}

func (h *Handler) updateUser(c *gin.Context) {
	identity, meta, ok := extractAuthIdentityAndMeta(c)
	if !ok {
		_ = c.Error(apperr.Unauthorized(apperr.CodeUnauthorized))
		return
	}

	userID, err := parseIDParam(c)
	if err != nil {
		_ = c.Error(err)
		return
	}

	var req updateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(err)
		return
	}

	var roleIDsPtr *[]uuid.UUID
	if req.RoleIDs != nil {
		parsed, err := parseRoleIDStrings(*req.RoleIDs)
		if err != nil {
			_ = c.Error(err)
			return
		}
		roleIDsPtr = &parsed
	}

	actorID := identity.UserID
	user, err := h.service.UpdateUser(c.Request.Context(), UpdateUserInput{
		UserID:         userID,
		Email:          req.Email,
		DisplayName:    req.DisplayName,
		Status:         req.Status,
		Password:       req.Password,
		RoleIDs:        roleIDsPtr,
		ActorID:        &actorID,
		ActorSessionID: identity.SessionID,
		Meta:           meta,
	})
	if err != nil {
		_ = c.Error(err)
		return
	}

	locale := i18n.LocaleFromContext(c.Request.Context())
	c.JSON(http.StatusOK, toUserResponse(user, locale, h.i18n))
}

func (h *Handler) deleteUser(c *gin.Context) {
	identity, meta, ok := extractAuthIdentityAndMeta(c)
	if !ok {
		_ = c.Error(apperr.Unauthorized(apperr.CodeUnauthorized))
		return
	}

	userID, err := parseIDParam(c)
	if err != nil {
		_ = c.Error(err)
		return
	}

	actorID := identity.UserID
	if err := h.service.DeleteUser(c.Request.Context(), DeleteUserInput{
		UserID:         userID,
		ActorID:        &actorID,
		ActorSessionID: identity.SessionID,
		Meta:           meta,
	}); err != nil {
		_ = c.Error(err)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *Handler) getProfile(c *gin.Context) {
	identity, _, ok := extractAuthIdentityAndMeta(c)
	if !ok {
		_ = c.Error(apperr.Unauthorized(apperr.CodeUnauthorized))
		return
	}

	profile, err := h.service.GetProfile(c.Request.Context(), identity.UserID, identity.SessionID)
	if err != nil {
		_ = c.Error(err)
		return
	}

	locale := i18n.LocaleFromContext(c.Request.Context())
	c.JSON(http.StatusOK, toProfileResponse(profile, locale, h.i18n))
}

func (h *Handler) updateProfile(c *gin.Context) {
	identity, meta, ok := extractAuthIdentityAndMeta(c)
	if !ok {
		_ = c.Error(apperr.Unauthorized(apperr.CodeUnauthorized))
		return
	}

	var req updateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(err)
		return
	}

	avatarID, clearAvatar, err := parseOptionalAvatarFileID(req.AvatarFileID)
	if err != nil {
		_ = c.Error(err)
		return
	}

	profile, err := h.service.UpdateProfile(c.Request.Context(), UpdateProfileInput{
		UserID:           identity.UserID,
		CurrentSessionID: identity.SessionID,
		Email:            req.Email,
		DisplayName:      req.DisplayName,
		AvatarFileID:     avatarID,
		ClearAvatar:      clearAvatar,
		Meta:             meta,
	})
	if err != nil {
		_ = c.Error(err)
		return
	}

	locale := i18n.LocaleFromContext(c.Request.Context())
	c.JSON(http.StatusOK, toProfileResponse(profile, locale, h.i18n))
}

func (h *Handler) changePassword(c *gin.Context) {
	identity, meta, ok := extractAuthIdentityAndMeta(c)
	if !ok {
		_ = c.Error(apperr.Unauthorized(apperr.CodeUnauthorized))
		return
	}

	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(err)
		return
	}

	res, err := h.service.ChangePassword(c.Request.Context(), ChangePasswordInput{
		UserID:              identity.UserID,
		CurrentSessionID:    identity.SessionID,
		CurrentPassword:     req.CurrentPassword,
		NewPassword:         req.NewPassword,
		RevokeOtherSessions: req.RevokeOtherSessions,
		Meta:                meta,
	})
	if err != nil {
		_ = c.Error(err)
		return
	}

	c.JSON(http.StatusOK, changePasswordResponse(res))
}

func (h *Handler) listProfileSessions(c *gin.Context) {
	identity, _, ok := extractAuthIdentityAndMeta(c)
	if !ok {
		_ = c.Error(apperr.Unauthorized(apperr.CodeUnauthorized))
		return
	}

	sessions, err := h.service.ListProfileSessions(c.Request.Context(), identity.UserID, identity.SessionID)
	if err != nil {
		_ = c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toSessionsListResponse(sessions))
}

func (h *Handler) revokeProfileSession(c *gin.Context) {
	identity, meta, ok := extractAuthIdentityAndMeta(c)
	if !ok {
		_ = c.Error(apperr.Unauthorized(apperr.CodeUnauthorized))
		return
	}

	sessionID, err := parseIDParam(c)
	if err != nil {
		_ = c.Error(err)
		return
	}

	if err := h.service.RevokeProfileSession(c.Request.Context(), RevokeSessionInput{
		UserID:           identity.UserID,
		CurrentSessionID: identity.SessionID,
		SessionID:        sessionID,
		Meta:             meta,
	}); err != nil {
		_ = c.Error(err)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *Handler) revokeOtherProfileSessions(c *gin.Context) {
	identity, meta, ok := extractAuthIdentityAndMeta(c)
	if !ok {
		_ = c.Error(apperr.Unauthorized(apperr.CodeUnauthorized))
		return
	}

	count, err := h.service.RevokeOtherProfileSessions(c.Request.Context(), RevokeOtherSessionsInput{
		UserID:           identity.UserID,
		CurrentSessionID: identity.SessionID,
		Meta:             meta,
	})
	if err != nil {
		_ = c.Error(err)
		return
	}

	c.JSON(http.StatusOK, revokeOtherSessionsResponse{
		RevokedSessions: count,
	})
}
