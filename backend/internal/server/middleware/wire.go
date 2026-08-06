package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/wire"
)

// JWTAuthMiddleware JWT 认证中间件类型
type JWTAuthMiddleware gin.HandlerFunc

// OptionalJWTAuthMiddleware 可选 JWT 认证中间件类型：匿名放行，带 token 严格校验
type OptionalJWTAuthMiddleware gin.HandlerFunc

// AdminAuthMiddleware 管理员认证中间件类型
type AdminAuthMiddleware gin.HandlerFunc

// APIKeyAuthMiddleware API Key 认证中间件类型
type APIKeyAuthMiddleware gin.HandlerFunc

// AstrBotReadScopeMiddleware bot:read scope 校验中间件类型
type AstrBotReadScopeMiddleware gin.HandlerFunc

// AstrBotWriteScopeMiddleware bot:write scope 校验中间件类型
type AstrBotWriteScopeMiddleware gin.HandlerFunc

func ProvideAstrBotReadScopeMiddleware() AstrBotReadScopeMiddleware {
	return AstrBotReadScopeMiddleware(NewAstrBotScopeMiddleware("bot:read"))
}

func ProvideAstrBotWriteScopeMiddleware() AstrBotWriteScopeMiddleware {
	return AstrBotWriteScopeMiddleware(NewAstrBotScopeMiddleware("bot:write"))
}

// ProviderSet 中间件层的依赖注入
var ProviderSet = wire.NewSet(
	NewJWTAuthMiddleware,
	NewOptionalJWTAuthMiddleware,
	NewAdminAuthMiddleware,
	NewAPIKeyAuthMiddleware,
	NewAstrBotAuthMiddleware,
	NewAstrBotRequestSignatureMiddleware,
	ProvideAstrBotReadScopeMiddleware,
	ProvideAstrBotWriteScopeMiddleware,
	NewAstrBotRateLimitMiddleware,
	NewAuditLogMiddleware,
	NewStepUpAuthMiddleware,
)
