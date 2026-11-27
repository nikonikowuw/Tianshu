package httpserver

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"

	"tianshu/internal/platform/config"
	"tianshu/internal/platform/i18n"
	"tianshu/internal/platform/logger"
	"tianshu/internal/shared/apperr"
)

const headerRequestID = "X-Request-Id"

var (
	requestIDPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
	registerValidator sync.Once
)

// Options configures the Gin HTTP engine and global middleware chain.
type Options struct {
	Env                string
	TrustedProxies     []string
	CORS               config.CORSConfig
	Logger             *slog.Logger
	I18n               *i18n.Bundle
	RedactPathPrefixes []string
}

// ConfigureMode sets Gin's global runtime mode once during process or test initialization.
func ConfigureMode(env string) {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "production":
		gin.SetMode(gin.ReleaseMode)
	case "test":
		gin.SetMode(gin.TestMode)
	default:
		gin.SetMode(gin.DebugMode)
	}
}

// ReadinessCheck validates an external runtime dependency for /readyz.
type ReadinessCheck func(ctx context.Context) error

type errorResponse struct {
	Code    string              `json:"code"`
	Message string              `json:"message"`
	Errors  []apperr.FieldError `json:"errors,omitempty"`
}

// NewEngine initializes a Gin engine with the contract-fixed middleware order:
// RequestID -> Recovery -> Logger -> SecurityHeaders -> CORS (optional) -> Locale -> ErrorHandler.
func NewEngine(opts Options) (*gin.Engine, error) {
	configureValidatorTagNames()

	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	redactPrefixes := normalizeRedactPrefixes(opts.RedactPathPrefixes)

	r := gin.New()
	if err := r.SetTrustedProxies(opts.TrustedProxies); err != nil {
		return nil, err
	}

	r.Use(requestIDMiddleware())
	r.Use(recoveryMiddleware(log, opts.I18n, redactPrefixes))
	r.Use(loggerMiddleware(log, redactPrefixes))
	r.Use(securityHeadersMiddleware())
	if opts.CORS.Enabled {
		r.Use(corsMiddleware(opts.CORS.AllowedOrigins))
	}
	r.Use(localeMiddleware())
	r.Use(errorHandlerMiddleware(log, opts.I18n))

	return r, nil
}

// RegisterHealthRoutes mounts /healthz (liveness) and /readyz (dependency readiness).
func RegisterHealthRoutes(r *gin.Engine, checks map[string]ReadinessCheck) {
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	r.GET("/readyz", func(c *gin.Context) {
		results := make(map[string]string, len(checks))
		healthy := true
		for name, fn := range checks {
			if fn == nil {
				results[name] = "ok"
				continue
			}
			if err := fn(c.Request.Context()); err != nil {
				results[name] = "error"
				healthy = false
				continue
			}
			results[name] = "ok"
		}

		status := http.StatusOK
		state := "ok"
		if !healthy {
			status = http.StatusServiceUnavailable
			state = "degraded"
		}
		c.JSON(status, gin.H{
			"status": state,
			"checks": results,
		})
	})
}

func configureValidatorTagNames() {
	registerValidator.Do(func() {
		v, ok := binding.Validator.Engine().(*validator.Validate)
		if !ok {
			return
		}
		v.RegisterTagNameFunc(func(fld reflect.StructField) string {
			tag := fld.Tag.Get("json")
			if tag == "" {
				tag = fld.Tag.Get("form")
			}
			if tag == "" {
				return fld.Name
			}
			name := strings.Split(tag, ",")[0]
			if name == "-" || name == "" {
				return fld.Name
			}
			return name
		})
	})
}

func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		reqID := strings.TrimSpace(c.GetHeader(headerRequestID))
		if !requestIDPattern.MatchString(reqID) {
			reqID = uuid.NewString()
		}
		c.Header(headerRequestID, reqID)
		c.Request = c.Request.WithContext(logger.WithRequestID(c.Request.Context(), reqID))
		c.Next()
	}
}

func recoveryMiddleware(log *slog.Logger, bundle *i18n.Bundle, redactPrefixes []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				reqLog := logger.FromContext(c.Request.Context(), log)
				reqLog.Error("http panic recovered",
					"panic", rec,
					"method", c.Request.Method,
					"path", requestLogPath(c, redactPrefixes),
				)
				if !c.Writer.Written() {
					lang := i18n.LocaleFromContext(c.Request.Context())
					msg := translate(bundle, lang, apperr.CodeInternal, nil)
					c.AbortWithStatusJSON(http.StatusInternalServerError, errorResponse{
						Code:    apperr.CodeInternal,
						Message: msg,
					})
					return
				}
				c.Abort()
			}
		}()
		c.Next()
	}
}

func loggerMiddleware(log *slog.Logger, redactPrefixes []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		reqLog := logger.FromContext(c.Request.Context(), log)
		reqLog.Info("http request",
			"method", c.Request.Method,
			"path", requestLogPath(c, redactPrefixes),
			"status", c.Writer.Status(),
			"latency_ms", time.Since(start).Milliseconds(),
			"client_ip", c.ClientIP(),
		)
	}
}

func normalizeRedactPrefixes(prefixes []string) []string {
	if len(prefixes) == 0 {
		return nil
	}
	out := make([]string, 0, len(prefixes))
	for _, raw := range prefixes {
		cleaned := path.Clean("/" + strings.TrimSpace(raw))
		if cleaned == "/" {
			continue
		}
		out = append(out, strings.TrimRight(cleaned, "/")+"/")
	}
	return out
}

// requestLogPath returns the matched route template so bearer credentials embedded in path
// parameters (for example signed file URLs) never reach access or recovery logs.
func requestLogPath(c *gin.Context, redactPrefixes []string) string {
	if route := c.FullPath(); route != "" {
		return route
	}
	cleaned := path.Clean("/" + c.Request.URL.Path)
	for _, prefix := range redactPrefixes {
		if strings.HasPrefix(cleaned, prefix) || cleaned+"/" == prefix {
			// NoRoute/405/uncleaned paths can still carry bearer tokens in the path;
			// never fall back to the raw URL for configured sensitive prefixes.
			return prefix + ":token"
		}
	}
	return c.Request.URL.Path
}

func securityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		c.Next()
	}
}

func corsMiddleware(allowedOrigins []string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		trimmed := strings.TrimSpace(o)
		if trimmed != "" && trimmed != "*" {
			allowed[trimmed] = struct{}{}
		}
	}

	return func(c *gin.Context) {
		c.Header("Vary", "Origin")
		origin := c.GetHeader("Origin")
		if origin != "" {
			if _, ok := allowed[origin]; ok {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Access-Control-Allow-Credentials", "true")
				c.Header("Access-Control-Expose-Headers", headerRequestID+", Retry-After, Content-Disposition")
				c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept-Language, X-Request-Id")
				c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			}
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func localeMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		lang := i18n.ParseAcceptLanguage(c.GetHeader("Accept-Language"))
		c.Request = c.Request.WithContext(i18n.WithLocale(c.Request.Context(), lang))
		c.Next()
	}
}

func errorHandlerMiddleware(log *slog.Logger, bundle *i18n.Bundle) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if len(c.Errors) == 0 || c.Writer.Written() {
			return
		}

		err := c.Errors.Last().Err
		lang := i18n.LocaleFromContext(c.Request.Context())

		if appErr, ok := apperr.As(err); ok {
			if appErr.Status >= http.StatusInternalServerError {
				logger.FromContext(c.Request.Context(), log).Error("application internal error",
					"code", appErr.Code,
					"error", appErr.Cause,
				)
			}
			var translatedFields []apperr.FieldError
			if len(appErr.Fields) > 0 {
				translatedFields = make([]apperr.FieldError, len(appErr.Fields))
				for i, f := range appErr.Fields {
					translatedFields[i] = apperr.FieldError{
						Field:   f.Field,
						Message: translate(bundle, lang, f.Message, nil),
					}
				}
			}
			c.JSON(appErr.Status, errorResponse{
				Code:    appErr.Code,
				Message: translate(bundle, lang, appErr.Code, appErr.Params),
				Errors:  translatedFields,
			})
			return
		}

		var valErrs validator.ValidationErrors
		if errors.As(err, &valErrs) {
			fields := translateValidationErrors(valErrs, bundle, lang)
			c.JSON(http.StatusUnprocessableEntity, errorResponse{
				Code:    apperr.CodeValidationFailed,
				Message: translate(bundle, lang, apperr.CodeValidationFailed, nil),
				Errors:  fields,
			})
			return
		}

		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			c.JSON(http.StatusRequestEntityTooLarge, errorResponse{
				Code:    apperr.CodeTooLarge,
				Message: translate(bundle, lang, apperr.CodeTooLarge, nil),
			})
			return
		}

		var syntaxErr *json.SyntaxError
		var typeErr *json.UnmarshalTypeError
		var numErr *strconv.NumError
		var timeErr *time.ParseError
		if errors.As(err, &syntaxErr) ||
			errors.As(err, &typeErr) ||
			errors.As(err, &numErr) ||
			errors.As(err, &timeErr) ||
			errors.Is(err, io.EOF) ||
			errors.Is(err, io.ErrUnexpectedEOF) {
			c.JSON(http.StatusBadRequest, errorResponse{
				Code:    apperr.CodeBadRequest,
				Message: translate(bundle, lang, apperr.CodeBadRequest, nil),
			})
			return
		}

		logger.FromContext(c.Request.Context(), log).Error("unhandled error", "error", err)
		c.JSON(http.StatusInternalServerError, errorResponse{
			Code:    apperr.CodeInternal,
			Message: translate(bundle, lang, apperr.CodeInternal, nil),
		})
	}
}

func translateValidationErrors(errs validator.ValidationErrors, bundle *i18n.Bundle, lang string) []apperr.FieldError {
	byField := make(map[string]apperr.FieldError, len(errs))
	for _, fe := range errs {
		fieldPath := normalizeFieldPath(fe.Namespace())
		if _, exists := byField[fieldPath]; exists {
			continue
		}
		byField[fieldPath] = apperr.FieldError{
			Field:   fieldPath,
			Message: translateFieldError(fe, bundle, lang),
		}
	}

	result := make([]apperr.FieldError, 0, len(byField))
	for _, item := range byField {
		result = append(result, item)
	}
	slices.SortFunc(result, func(a, b apperr.FieldError) int {
		return cmp.Compare(a.Field, b.Field)
	})
	return result
}

func normalizeFieldPath(namespace string) string {
	// Strip the leading root struct name (e.g. "createUserRequest.profile.email" -> "profile.email").
	if _, after, ok := strings.Cut(namespace, "."); ok && after != "" {
		return after
	}
	return namespace
}

func translateFieldError(fe validator.FieldError, bundle *i18n.Bundle, lang string) string {
	switch fe.Tag() {
	case "required":
		return translate(bundle, lang, "VALIDATION_REQUIRED", nil)
	case "email":
		return translate(bundle, lang, "VALIDATION_EMAIL", nil)
	case "min":
		return translate(bundle, lang, "VALIDATION_MIN", map[string]any{"min": fe.Param()})
	case "max":
		return translate(bundle, lang, "VALIDATION_MAX", map[string]any{"max": fe.Param()})
	case "oneof":
		return translate(bundle, lang, "VALIDATION_ONE_OF", nil)
	case "uuid":
		return translate(bundle, lang, "VALIDATION_UUID", nil)
	default:
		return translate(bundle, lang, "VALIDATION_DEFAULT", nil)
	}
}

func translate(bundle *i18n.Bundle, lang, key string, params map[string]any) string {
	if bundle == nil {
		return key
	}
	return bundle.Translate(lang, key, params)
}
