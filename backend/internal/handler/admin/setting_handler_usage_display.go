package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// GetUsageDisplayConfig returns the usage table display defaults.
// GET /api/v1/admin/settings/usage-display
func (h *SettingHandler) GetUsageDisplayConfig(c *gin.Context) {
	config, err := h.settingService.GetUsageDisplayConfig(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, config)
}

// UpdateUsageDisplayConfig partially updates usage display defaults. Omitted
// properties retain their current/default value, while explicit false values
// are preserved by pointer fields in the service patch.
// PUT /api/v1/admin/settings/usage-display
func (h *SettingHandler) UpdateUsageDisplayConfig(c *gin.Context) {
	var patch service.UsageDisplayConfigPatch
	if err := c.ShouldBindJSON(&patch); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := h.settingService.UpdateUsageDisplayConfig(c.Request.Context(), patch); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	config, err := h.settingService.GetUsageDisplayConfig(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, config)
}
