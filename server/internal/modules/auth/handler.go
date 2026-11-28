package auth

import (
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"

	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/authctx"
)

const refreshCookieName = "tianshu_refresh_token"

// Handler exposes HTTP authentication and session endpoints.
type Handler struct {
	service        *Service
	allowedOrigins []string
	cookiePath     string
}

// NewHandler creates an auth Handler. cookiePath is the injected cookie Path (e.g. /api/v1/auth)
// so the handler never hard-codes the API version prefix owned by internal/app.
func NewHandler(service *Service, allowedOrigins []string, cookiePath string) *Handler {
	return &Handler{
		service:        service,
		allowedOrigins: slices.Clone(allowedOrigins),
		cookiePath:     cookiePath,
	}
}

// shouldClearRefreshCookie reports whether a failed refresh should drop the client's refresh cookie.
// It covers rejected/expired/reused tokens (401) and disabled accounts (403) so the browser stops replaying it.
func shouldClearRefreshCookie(err error) bool {
	appErr, ok := apperr.As(err)
	if !ok {
		return false
	}
	return appErr.Status == http.StatusUnauthorized || appErr.Code == ErrCodeAccountDisabled
}

func (h *Handler) login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(err)
		return
	}

	res, err := h.service.Login(c.Request.Context(), LoginInput{
		Identifier: req.Identifier,
		Password:   req.Password,
		ClientIP:   c.ClientIP(),
		UserAgent:  c.GetHeader("User-Agent"),
	})
	if err != nil {
		// Retry-After for 429 is rendered uniformly by errorHandlerMiddleware from apperr params.
		_ = c.Error(err)
		return
	}

	h.setRefreshCookie(c, res.RefreshToken)
	c.JSON(http.StatusOK, toLoginResponse(res))
}

func (h *Handler) refresh(c *gin.Context) {
	cookieToken, _ := c.Cookie(refreshCookieName)
	res, err := h.service.Refresh(c.Request.Context(), RefreshInput{
		RefreshToken: cookieToken,
		ClientIP:     c.ClientIP(),
		UserAgent:    c.GetHeader("User-Agent"),
	})
	if err != nil {
		if cookieToken != "" && shouldClearRefreshCookie(err) {
			h.clearRefreshCookie(c)
		}
		_ = c.Error(err)
		return
	}

	if res.RotateCookie && res.RefreshToken != "" {
		h.setRefreshCookie(c, res.RefreshToken)
	}
	c.JSON(http.StatusOK, toRefreshResponse(res))
}

func (h *Handler) logout(c *gin.Context) {
	cookieToken, _ := c.Cookie(refreshCookieName)
	accessToken, _ := extractBearerToken(c.GetHeader("Authorization"))

	err := h.service.Logout(c.Request.Context(), LogoutInput{
		RefreshToken: cookieToken,
		AccessToken:  accessToken,
		ClientIP:     c.ClientIP(),
		UserAgent:    c.GetHeader("User-Agent"),
	})
	h.clearRefreshCookie(c)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) me(c *gin.Context) {
	identity, ok := authctx.IdentityFromContext(c.Request.Context())
	if !ok {
		_ = c.Error(apperr.Unauthorized(apperr.CodeUnauthorized))
		return
	}

	res, err := h.service.Me(c.Request.Context(), identity.UserID, identity.SessionID)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toMeResponse(res))
}

func (h *Handler) setRefreshCookie(c *gin.Context, token string) {
	maxAge := int(h.service.refreshTokenTTL().Seconds())
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		refreshCookieName,
		token,
		maxAge,
		h.cookiePath,
		"",
		h.service.cookieSecure(),
		true,
	)
}

func (h *Handler) clearRefreshCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		refreshCookieName,
		"",
		-1,
		h.cookiePath,
		"",
		h.service.cookieSecure(),
		true,
	)
}
