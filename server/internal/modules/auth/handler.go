package auth

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/authctx"
)

const (
	refreshCookieName = "tianshu_refresh_token"
	refreshCookiePath = "/api/v1/auth"
)

// Handler exposes HTTP authentication and session endpoints.
type Handler struct {
	service        *Service
	allowedOrigins []string
}

// NewHandler creates an auth Handler.
func NewHandler(service *Service, allowedOrigins []string) *Handler {
	return &Handler{
		service:        service,
		allowedOrigins: allowedOrigins,
	}
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
		if appErr, ok := apperr.As(err); ok && appErr.Status == http.StatusTooManyRequests {
			if retry, exists := appErr.Params["retry_after"]; exists {
				c.Header("Retry-After", fmt.Sprint(retry))
			}
		}
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
		if cookieToken != "" {
			if appErr, ok := apperr.As(err); ok &&
				(appErr.Status == http.StatusUnauthorized || appErr.Code == ErrCodeAccountDisabled) {
				h.clearRefreshCookie(c)
			}
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

	var sessionID uuid.UUID
	if rawAuth := strings.TrimSpace(c.GetHeader("Authorization")); rawAuth != "" {
		if token, ok := strings.CutPrefix(rawAuth, "Bearer "); ok {
			if claims, err := h.service.JWTManager().Verify(strings.TrimSpace(token)); err == nil {
				sessionID = claims.SessionID
			}
		}
	}

	err := h.service.Logout(c.Request.Context(), cookieToken, sessionID)
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

	res, err := h.service.Me(c.Request.Context(), identity.UserID)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toMeResponse(res))
}

func (h *Handler) setRefreshCookie(c *gin.Context, token string) {
	maxAge := int(h.service.RefreshTokenTTL().Seconds())
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		refreshCookieName,
		token,
		maxAge,
		refreshCookiePath,
		"",
		h.service.CookieSecure(),
		true,
	)
}

func (h *Handler) clearRefreshCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		refreshCookieName,
		"",
		-1,
		refreshCookiePath,
		"",
		h.service.CookieSecure(),
		true,
	)
}
