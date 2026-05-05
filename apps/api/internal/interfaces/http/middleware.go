package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/visionloop/api/internal/application"
	"github.com/visionloop/api/internal/domain"
	"github.com/visionloop/api/internal/observability"
)

// Middleware order mirrors section 9 of the spec:
//
//   1. Request ID
//   2. CORS (per-org allowlist; in Phase 1 we use the WEB_ORIGIN env var)
//   3. Rate limiting (Phase 8 implements the token bucket; reserved hook here)
//   4. Session auth
//   5. RBAC (per-handler — see RequireRole)
//   6. Input sanitisation (handler-level via binding tags)
//   7. File validation (handler-level on /v1/images:presign)
//
// Each middleware runs `c.Next()` so handler errors flow back through
// the chain unmodified.

const (
	headerRequestID = "X-Request-ID"
	sessionCookie   = "better-auth.session_token"
	contextCaller   = "vl.caller"
)

// RequestID generates or accepts an X-Request-ID and attaches it to the
// request context for structured logging.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := strings.TrimSpace(c.GetHeader(headerRequestID))
		if id == "" {
			id = uuid.NewString()
		}
		c.Writer.Header().Set(headerRequestID, id)
		c.Request = c.Request.WithContext(observability.WithRequestID(c.Request.Context(), id))
		c.Set("vl.request_id", id)
		c.Next()
	}
}

// AccessLog emits one structured log line per request.
func AccessLog(base *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		dur := time.Since(start)
		log := observability.FromContext(c.Request.Context(), base)
		log.LogAttrs(c.Request.Context(), slog.LevelInfo, "request",
			slog.String("method", c.Request.Method),
			slog.String("path", c.FullPath()),
			slog.Int("status", c.Writer.Status()),
			slog.Int64("duration_ms", dur.Milliseconds()),
			slog.Int("bytes", c.Writer.Size()),
		)
	}
}

// AuthRequired resolves the current caller from the session cookie. The
// resolved Caller is stored on the gin context for handlers and the request
// context is enriched with user/org IDs for the access log.
func AuthRequired(sessions application.SessionStore, dir application.UserDirectory) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := readSessionToken(c)
		if token == "" {
			abortJSON(c, http.StatusUnauthorized, "unauthorized", "missing session")
			return
		}

		userID, _, err := sessions.LookupSession(c.Request.Context(), token)
		if err != nil {
			abortJSON(c, http.StatusUnauthorized, "unauthorized", "invalid session")
			return
		}

		membership, err := dir.PrimaryMembership(c.Request.Context(), userID)
		if err != nil {
			// A user without a membership cannot transact. This should never
			// happen because signup creates the membership in the same
			// transaction (ADR-0003).
			abortJSON(c, http.StatusForbidden, "forbidden", "no active organisation")
			return
		}

		caller := domain.Caller{
			UserID: userID,
			OrgID:  membership.OrgID,
			Role:   membership.Role,
		}
		c.Set(contextCaller, caller)
		ctx := observability.WithUserID(c.Request.Context(), caller.UserID.String())
		ctx = observability.WithOrgID(ctx, caller.OrgID.String())
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// RequireRole guards a handler with one of the supplied roles.
func RequireRole(allowed ...domain.Role) gin.HandlerFunc {
	allowedSet := make(map[domain.Role]struct{}, len(allowed))
	for _, r := range allowed {
		allowedSet[r] = struct{}{}
	}
	return func(c *gin.Context) {
		caller, ok := callerFrom(c)
		if !ok {
			abortJSON(c, http.StatusUnauthorized, "unauthorized", "missing caller")
			return
		}
		if _, ok := allowedSet[caller.Role]; !ok {
			abortJSON(c, http.StatusForbidden, "forbidden", "insufficient role")
			return
		}
		c.Next()
	}
}

// readSessionToken extracts the Better Auth session token. We accept both
// the cookie (browser flow) and a Bearer token (CLI/API flow) so that
// future API-key scoping (Phase 8) can hang off the same handler.
func readSessionToken(c *gin.Context) string {
	if cookie, err := c.Cookie(sessionCookie); err == nil && cookie != "" {
		return cookie
	}
	auth := c.GetHeader("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}
	return ""
}

func callerFrom(c *gin.Context) (domain.Caller, bool) {
	v, ok := c.Get(contextCaller)
	if !ok {
		return domain.Caller{}, false
	}
	caller, ok := v.(domain.Caller)
	return caller, ok
}

// abortJSON writes a uniform error envelope.
func abortJSON(c *gin.Context, status int, code, msg string) {
	c.AbortWithStatusJSON(status, gin.H{
		"error": gin.H{
			"code":       code,
			"message":    msg,
			"request_id": c.GetString("vl.request_id"),
		},
	})
}

// httpStatusFor maps domain errors to HTTP status codes.
func httpStatusFor(err error) (int, string) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, domain.ErrUnauthorized):
		return http.StatusUnauthorized, "unauthorized"
	case errors.Is(err, domain.ErrForbidden):
		return http.StatusForbidden, "forbidden"
	case errors.Is(err, domain.ErrConflict):
		return http.StatusConflict, "conflict"
	case errors.Is(err, domain.ErrInvalidInput):
		return http.StatusBadRequest, "invalid_input"
	case errors.Is(err, domain.ErrTooLarge):
		return http.StatusRequestEntityTooLarge, "too_large"
	case errors.Is(err, domain.ErrUnsupportedCT):
		return http.StatusUnsupportedMediaType, "unsupported_media_type"
	default:
		return http.StatusInternalServerError, "internal"
	}
}
