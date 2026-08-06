package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	internalmiddleware "github.com/Wei-Shaw/sub2api/internal/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

const (
	AstrBotPrincipalKey = "astrbot_principal"
	AstrBotTokenKey     = "astrbot_token"
	AstrBotAuthMethod   = service.AuditAuthMethodAstrBotToken
	astrBotRPM          = 60
)

type AstrBotPrincipal struct {
	TokenID        string
	UserID         int64
	Scopes         []string
	InstallationID string
	PlatformUser   string
	SessionID      string
	ConversationID string
}

type AstrBotAuthMiddleware gin.HandlerFunc
type AstrBotScopeMiddleware gin.HandlerFunc
type AstrBotRateLimitMiddleware gin.HandlerFunc

type astrBotRateLimiter interface {
	Allow(context.Context, string, int, time.Duration) (internalmiddleware.AllowResult, error)
}

func NewAstrBotAuthMiddleware(tokens *service.AstrBotTokenService, users *service.UserService) AstrBotAuthMiddleware {
	return AstrBotAuthMiddleware(func(c *gin.Context) {
		if tokens == nil || users == nil {
			abortAstrBotUnauthorized(c)
			return
		}
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
			abortAstrBotUnauthorized(c)
			return
		}
		token, err := tokens.Authenticate(c.Request.Context(), strings.TrimSpace(parts[1]))
		if err != nil || token == nil {
			abortAstrBotUnauthorized(c)
			return
		}
		user, err := users.GetByID(c.Request.Context(), token.UserID)
		if err != nil || user == nil || !user.IsActive() || !user.IsAdmin() {
			abortAstrBotUnauthorized(c)
			return
		}
		c.Set(AstrBotTokenKey, token)
		c.Set(AstrBotPrincipalKey, AstrBotPrincipal{TokenID: token.TokenID, UserID: token.UserID, Scopes: append([]string(nil), token.Scopes...), InstallationID: token.InstallationID})
		c.Set(string(ContextKeyUser), AuthSubject{UserID: user.ID, Concurrency: user.Concurrency})
		c.Set(string(ContextKeyUserRole), user.Role)
		c.Set(ContextKeyAuthEmail, user.Email)
		c.Set("auth_method", AstrBotAuthMethod)
		c.Next()
	})
}

func NewAstrBotScopeMiddleware(scope string) AstrBotScopeMiddleware {
	return AstrBotScopeMiddleware(func(c *gin.Context) {
		value, ok := c.Get(AstrBotPrincipalKey)
		principal, principalOK := value.(AstrBotPrincipal)
		if !ok || !principalOK || !hasAstrBotScope(principal.Scopes, scope) {
			c.AbortWithStatusJSON(http.StatusForbidden, map[string]any{
				"code": http.StatusForbidden, "message": "missing required bot scope", "reason": "BOT_SCOPE_REQUIRED",
			})
			return
		}
		c.Next()
	})
}

func NewAstrBotRateLimitMiddleware(redisClient *redis.Client) AstrBotRateLimitMiddleware {
	limiter := internalmiddleware.NewRateLimiter(redisClient)
	return NewAstrBotRateLimitMiddlewareWithLimiter(limiter)
}
func NewAstrBotRateLimitMiddlewareWithLimiter(limiter astrBotRateLimiter) AstrBotRateLimitMiddleware {
	return AstrBotRateLimitMiddleware(func(c *gin.Context) {
		value, ok := c.Get(AstrBotPrincipalKey)
		principal, principalOK := value.(AstrBotPrincipal)
		if !ok || !principalOK || principal.TokenID == "" || limiter == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, map[string]any{"code": http.StatusUnauthorized, "message": "unauthorized", "reason": "UNAUTHORIZED"})
			return
		}
		result, err := limiter.Allow(c.Request.Context(), "astrbot:token:"+principal.TokenID, astrBotRPM, time.Minute)
		if err != nil || !result.Allowed {
			c.Header("Retry-After", "60")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, map[string]any{"code": http.StatusTooManyRequests, "message": "too many requests", "reason": "BOT_RATE_LIMITED"})
			return
		}
		c.Next()
	})
}

func hasAstrBotScope(scopes []string, required string) bool {
	for _, scope := range scopes {
		if scope == required {
			return true
		}
	}
	return false
}

func GetAstrBotPrincipal(c *gin.Context) (AstrBotPrincipal, bool) {
	value, ok := c.Get(AstrBotPrincipalKey)
	principal, principalOK := value.(AstrBotPrincipal)
	return principal, ok && principalOK
}

func abortAstrBotUnauthorized(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, map[string]any{
		"code": http.StatusUnauthorized, "message": "unauthorized", "reason": "UNAUTHORIZED",
	})
}
