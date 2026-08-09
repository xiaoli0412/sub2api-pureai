package admin

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// ModelPricingHandler exposes the built-in catalog plus administrator overrides.
type ModelPricingHandler struct {
	pricingService *service.PricingService
	settingRepo    service.SettingRepository
	channelService *service.ChannelService
}

func NewModelPricingHandler(pricingService *service.PricingService, settingRepo service.SettingRepository, channelService *service.ChannelService) *ModelPricingHandler {
	return &ModelPricingHandler{pricingService: pricingService, settingRepo: settingRepo, channelService: channelService}
}

type modelPricingResponse struct {
	Models []modelPricingItem `json:"models"`
}

type modelPricingItem struct {
	Name      string                       `json:"name"`
	Platform  string                       `json:"platform"`
	BuiltIn   *service.LiteLLMModelPricing `json:"built_in"`
	Override  service.ModelPricingOverride `json:"override"`
	Effective *service.LiteLLMModelPricing `json:"effective"`
}

type modelPricingUpdateRequest struct {
	InputCostPerToken                   *float64 `json:"input_cost_per_token" binding:"omitempty,min=0"`
	InputCostPerTokenPriority           *float64 `json:"input_cost_per_token_priority" binding:"omitempty,min=0"`
	OutputCostPerToken                  *float64 `json:"output_cost_per_token" binding:"omitempty,min=0"`
	OutputCostPerTokenPriority          *float64 `json:"output_cost_per_token_priority" binding:"omitempty,min=0"`
	CacheCreationInputTokenCost         *float64 `json:"cache_creation_input_token_cost" binding:"omitempty,min=0"`
	CacheCreationInputTokenCostPriority *float64 `json:"cache_creation_input_token_cost_priority" binding:"omitempty,min=0"`
	CacheCreationInputTokenCostAbove1hr *float64 `json:"cache_creation_input_token_cost_above_1hr" binding:"omitempty,min=0"`
	CacheReadInputTokenCost             *float64 `json:"cache_read_input_token_cost" binding:"omitempty,min=0"`
	CacheReadInputTokenCostPriority     *float64 `json:"cache_read_input_token_cost_priority" binding:"omitempty,min=0"`
	LongContextInputTokenThreshold      *int     `json:"long_context_input_token_threshold" binding:"omitempty,min=0"`
	LongContextInputCostMultiplier      *float64 `json:"long_context_input_cost_multiplier" binding:"omitempty,min=0"`
	LongContextOutputCostMultiplier     *float64 `json:"long_context_output_cost_multiplier" binding:"omitempty,min=0"`
	OutputCostPerImage                  *float64 `json:"output_cost_per_image" binding:"omitempty,min=0"`
	OutputCostPerImageToken             *float64 `json:"output_cost_per_image_token" binding:"omitempty,min=0"`
	InputCostPerImageToken              *float64 `json:"input_cost_per_image_token" binding:"omitempty,min=0"`
}

func (r modelPricingUpdateRequest) toOverride() service.ModelPricingOverride {
	return service.ModelPricingOverride{
		InputCostPerToken: r.InputCostPerToken, InputCostPerTokenPriority: r.InputCostPerTokenPriority,
		OutputCostPerToken: r.OutputCostPerToken, OutputCostPerTokenPriority: r.OutputCostPerTokenPriority,
		CacheCreationInputTokenCost: r.CacheCreationInputTokenCost, CacheCreationInputTokenCostPriority: r.CacheCreationInputTokenCostPriority,
		CacheCreationInputTokenCostAbove1hr: r.CacheCreationInputTokenCostAbove1hr, CacheReadInputTokenCost: r.CacheReadInputTokenCost,
		CacheReadInputTokenCostPriority: r.CacheReadInputTokenCostPriority, LongContextInputTokenThreshold: r.LongContextInputTokenThreshold,
		LongContextInputCostMultiplier: r.LongContextInputCostMultiplier, LongContextOutputCostMultiplier: r.LongContextOutputCostMultiplier,
		OutputCostPerImage: r.OutputCostPerImage, OutputCostPerImageToken: r.OutputCostPerImageToken, InputCostPerImageToken: r.InputCostPerImageToken,
	}
}

func requireModelPricingIdempotencyKey(c *gin.Context) bool {
	key, err := service.NormalizeIdempotencyKey(c.GetHeader("Idempotency-Key"))
	if err != nil {
		response.ErrorFrom(c, err)
		return false
	}
	if key == "" {
		response.ErrorFrom(c, service.ErrIdempotencyKeyRequired)
		return false
	}
	return true
}

func (h *ModelPricingHandler) List(c *gin.Context) {
	models, err := h.visibleModels(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	out := make([]modelPricingItem, 0, len(models))
	for _, model := range models {
		out = append(out, h.modelPricingItem(model.name, model.platform))
	}
	response.Success(c, modelPricingResponse{Models: out})
}

func (h *ModelPricingHandler) Update(c *gin.Context) {
	model := strings.TrimSpace(c.Param("model"))
	models, err := h.visibleModels(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var platform string
	found := false
	for _, item := range models {
		if strings.EqualFold(item.name, model) {
			model, platform, found = item.name, item.platform, true
			break
		}
	}
	if !found {
		response.NotFound(c, "model is not available")
		return
	}
	var req modelPricingUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid model pricing")
		return
	}
	if !requireModelPricingIdempotencyKey(c) {
		return
	}

	payload := gin.H{"model": model, "pricing": req}
	executeAdminIdempotentJSON(c, "admin.model_pricing.update", payload, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		if err := h.pricingService.SetModelPricingOverride(ctx, h.settingRepo, model, req.toOverride()); err != nil {
			return nil, err
		}
		return h.modelPricingItem(model, platform), nil
	})
}

// ResetDefault removes the administrator override and restores built-in pricing.
func (h *ModelPricingHandler) ResetDefault(c *gin.Context) {
	model := strings.TrimSpace(c.Param("model"))
	models, err := h.visibleModels(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var platform string
	found := false
	for _, item := range models {
		if strings.EqualFold(item.name, model) {
			model, platform, found = item.name, item.platform, true
			break
		}
	}
	if !found {
		response.NotFound(c, "model is not available")
		return
	}
	if !requireModelPricingIdempotencyKey(c) {
		return
	}

	payload := gin.H{"model": model}
	executeAdminIdempotentJSON(c, "admin.model_pricing.reset", payload, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		if err := h.pricingService.ResetModelPricingOverride(ctx, h.settingRepo, model); err != nil {
			return nil, err
		}
		return h.modelPricingItem(model, platform), nil
	})
}

func (h *ModelPricingHandler) modelPricingItem(model, platform string) modelPricingItem {
	return modelPricingItem{
		Name:     model,
		Platform: platform,
		BuiltIn:  h.pricingService.GetModelPricingDefault(model),
		Override: func() service.ModelPricingOverride {
			override, _ := h.pricingService.GetModelPricingOverride(model)
			return override
		}(),
		Effective: h.pricingService.GetModelPricing(model),
	}
}

type visiblePricingModel struct{ name, platform string }

func (h *ModelPricingHandler) visibleModels(ctx context.Context) ([]visiblePricingModel, error) {
	if h.channelService == nil {
		return nil, fmt.Errorf("channel service is unavailable")
	}
	channels, err := h.channelService.ListAvailable(ctx)
	if err != nil {
		return nil, fmt.Errorf("list visible models: %w", err)
	}
	seen := make(map[string]visiblePricingModel)
	for _, channel := range channels {
		if channel.Status != service.StatusActive {
			continue
		}
		for _, model := range channel.SupportedModels {
			key := strings.ToLower(strings.TrimSpace(model.Platform + "\x00" + model.Name))
			if model.Name != "" {
				seen[key] = visiblePricingModel{name: model.Name, platform: model.Platform}
			}
		}
	}
	out := make([]visiblePricingModel, 0, len(seen))
	for _, model := range seen {
		out = append(out, model)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].name) < strings.ToLower(out[j].name) })
	return out, nil
}
