package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// GetUsageDisplayConfig returns the public, read-only usage display defaults.
// GET /api/v1/settings/usage-display
func (h *SettingHandler) GetUsageDisplayConfig(c *gin.Context) {
	config, err := h.settingService.GetUsageDisplayConfig(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, config)
}
