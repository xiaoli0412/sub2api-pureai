package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// RegisterAstrBotRoutes registers token lifecycle and dedicated Astrbot API routes.
func RegisterAstrBotRoutes(
	v1 *gin.RouterGroup,
	h *handler.Handlers,
	adminAuth middleware.AdminAuthMiddleware,
	auditLog middleware.AuditLogMiddleware,
	settingService *service.SettingService,
	panelRateLimiter *middleware.PanelRateLimiter,
	astrBotAuth middleware.AstrBotAuthMiddleware,
	astrBotSignature middleware.AstrBotRequestSignatureMiddleware,
	astrBotRateLimit middleware.AstrBotRateLimitMiddleware,
	astrBotRead middleware.AstrBotReadScopeMiddleware,
	astrBotWrite middleware.AstrBotWriteScopeMiddleware,
) {
	tokens := v1.Group("/admin/astrbot-tokens")
	tokens.Use(gin.HandlerFunc(adminAuth))
	tokens.Use(panelRateLimiter.Global())
	tokens.Use(gin.HandlerFunc(auditLog))
	tokens.Use(middleware.AdminComplianceGuard(settingService))
	{
		tokens.GET("", h.AstrBot.ListTokens)
		tokens.POST("", h.AstrBot.CreateToken)
		tokens.POST("/:token_id/revoke", h.AstrBot.RevokeToken)
	}

	bot := v1.Group("/bot")
	bot.Use(gin.HandlerFunc(astrBotAuth))
	bot.Use(gin.HandlerFunc(astrBotSignature))
	bot.Use(gin.HandlerFunc(astrBotRateLimit))
	bot.Use(gin.HandlerFunc(auditLog))
	{
		read := bot.Group("")
		read.Use(gin.HandlerFunc(astrBotRead))
		read.GET("/costs", h.AstrBot.Costs)
		read.GET("/profit", h.AstrBot.Profit)
		read.GET("/consumption", h.AstrBot.Consumption)
		read.GET("/channels/status", h.AstrBot.ChannelsStatus)
		read.GET("/accounts/status", h.AstrBot.AccountsStatus)
		read.GET("/models/prices", h.AstrBot.ModelPrices)

		write := bot.Group("")
		write.Use(gin.HandlerFunc(astrBotWrite))
		write.POST("/operations/prepare", h.AstrBot.PrepareOperation)
		write.POST("/operations/:id/confirm", h.AstrBot.ConfirmOperation)
		write.POST("/operations/:id/confirm-again", h.AstrBot.ConfirmOperationAgain)
		write.POST("/operations/:id/execute", h.AstrBot.ExecuteOperation)
		write.POST("/operations/:id/cancel", h.AstrBot.CancelOperation)
		write.POST("/channels/:id/toggle", h.AstrBot.ToggleChannel)
		write.POST("/accounts/:id/toggle", h.AstrBot.ToggleAccount)
		write.PUT("/accounts/:id/rate-multiplier", h.AstrBot.UpdateAccountRateMultiplier)
		write.PUT("/channels/:id/pricing", h.AstrBot.UpdateChannelPricing)
		write.POST("/cache/refresh", h.AstrBot.RefreshCache)
	}
}
