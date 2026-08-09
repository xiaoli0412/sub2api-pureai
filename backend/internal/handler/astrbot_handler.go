package handler

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// AstrBotHandler exposes the dedicated AstrBot control-plane API.
// Every response is built from explicit allowlists; service entities containing
// credentials, proxy data, or arbitrary JSON are never serialized directly.
type AstrBotHandler struct {
	tokens      *service.AstrBotTokenService
	admin       service.AdminService
	accounts    *service.AccountService
	channels    *service.ChannelService
	usage       *service.UsageService
	pricing     *service.PricingService
	idempotency *service.IdempotencyCoordinator
	audit       *service.AuditLogService
	operations  *service.AstrBotOperationService
	ops         *service.OpsService
}

func NewAstrBotHandler(
	tokens *service.AstrBotTokenService,
	admin service.AdminService,
	accounts *service.AccountService,
	channels *service.ChannelService,
	usage *service.UsageService,
	pricing *service.PricingService,
	idempotency *service.IdempotencyCoordinator,
	audit *service.AuditLogService,
	operations *service.AstrBotOperationService,
	ops *service.OpsService,
) *AstrBotHandler {
	return &AstrBotHandler{tokens: tokens, admin: admin, accounts: accounts, channels: channels, usage: usage, pricing: pricing, idempotency: idempotency, audit: audit, operations: operations, ops: ops}
}

type astrBotTokenDTO struct {
	ID                int64                            `json:"id"`
	TokenID           string                           `json:"token_id"`
	Name              string                           `json:"name"`
	Scopes            []string                         `json:"scopes"`
	InstallationID    string                           `json:"installation_id,omitempty"`
	ResourceAllowlist service.AstrBotResourceAllowlist `json:"resource_allowlist,omitempty"`
	ExpiresAt         *time.Time                       `json:"expires_at,omitempty"`
	RevokedAt         *time.Time                       `json:"revoked_at,omitempty"`
	LastUsedAt        *time.Time                       `json:"last_used_at,omitempty"`
	CreatedAt         time.Time                        `json:"created_at"`
	UpdatedAt         time.Time                        `json:"updated_at"`
}

type createAstrBotTokenRequest struct {
	Name              string                           `json:"name" binding:"required"`
	Scopes            []string                         `json:"scopes" binding:"required"`
	InstallationID    string                           `json:"installation_id" binding:"required"`
	ResourceAllowlist service.AstrBotResourceAllowlist `json:"resource_allowlist,omitempty"`
	ExpiresAt         *time.Time                       `json:"expires_at"`
}

type createdAstrBotTokenDTO struct {
	Token              astrBotTokenDTO `json:"token"`
	RawToken           string          `json:"raw_token"`
	InstallationSecret string          `json:"installation_secret"`
}

func toAstrBotTokenDTO(token *service.AstrBotToken) astrBotTokenDTO {
	if token == nil {
		return astrBotTokenDTO{}
	}
	return astrBotTokenDTO{
		ID: token.ID, TokenID: token.TokenID, Name: token.Name,
		Scopes:         append([]string(nil), token.Scopes...),
		InstallationID: token.InstallationID,
		ResourceAllowlist: service.AstrBotResourceAllowlist{
			AccountIDs: append([]int64(nil), token.ResourceAllowlist.AccountIDs...),
			ChannelIDs: append([]int64(nil), token.ResourceAllowlist.ChannelIDs...),
			GroupIDs:   append([]int64(nil), token.ResourceAllowlist.GroupIDs...),
			UserIDs:    append([]int64(nil), token.ResourceAllowlist.UserIDs...),
			LogSources: append([]string(nil), token.ResourceAllowlist.LogSources...),
		},
		ExpiresAt: token.ExpiresAt, RevokedAt: token.RevokedAt, LastUsedAt: token.LastUsedAt,
		CreatedAt: token.CreatedAt, UpdatedAt: token.UpdatedAt,
	}
}

func astrBotTokenFromContext(c *gin.Context) (*service.AstrBotToken, bool) {
	value, ok := c.Get(middleware.AstrBotTokenKey)
	token, tokenOK := value.(*service.AstrBotToken)
	return token, ok && tokenOK && token != nil
}

func requireAstrBotGlobalResource(c *gin.Context) bool {
	token, ok := astrBotTokenFromContext(c)
	if !ok || !token.ResourceAllowlist.IsUnrestricted() {
		response.NotFound(c, "resource not found")
		return false
	}
	return true
}

func requireAstrBotAccountResource(c *gin.Context, id int64) bool {
	token, ok := astrBotTokenFromContext(c)
	if !ok || !token.ResourceAllowlist.AllowsAccount(id) {
		response.NotFound(c, "resource not found")
		return false
	}
	return true
}

func requireAstrBotChannelResource(c *gin.Context, id int64) bool {
	token, ok := astrBotTokenFromContext(c)
	if !ok || !token.ResourceAllowlist.AllowsChannel(id) {
		response.NotFound(c, "resource not found")
		return false
	}
	return true
}

func paginateAstrBotItems[T any](items []T, page, pageSize int) ([]T, int64) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	start := (page - 1) * pageSize
	if start >= len(items) {
		return []T{}, int64(len(items))
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	return items[start:end], int64(len(items))
}

func astrBotAccountMatches(account service.Account, platform, accountType, status, search string) bool {
	if platform != "" && account.Platform != platform {
		return false
	}
	if accountType != "" && account.Type != accountType {
		return false
	}
	if status != "" && account.Status != status {
		return false
	}
	if search != "" && !strings.Contains(strings.ToLower(account.Name), strings.ToLower(search)) {
		return false
	}
	return true
}

func astrBotChannelMatches(channel service.Channel, status, search string) bool {
	if status != "" && channel.Status != status {
		return false
	}
	if search != "" && !strings.Contains(strings.ToLower(channel.Name), strings.ToLower(search)) {
		return false
	}
	return true
}

func (h *AstrBotHandler) listAllowedAccounts(c *gin.Context, token *service.AstrBotToken, page, pageSize int) bool {
	if len(token.ResourceAllowlist.AccountIDs) == 0 {
		response.NotFound(c, "resource not found")
		return true
	}
	accounts, err := h.admin.GetAccountsByIDs(c.Request.Context(), token.ResourceAllowlist.AccountIDs)
	if err != nil {
		response.ErrorFrom(c, err)
		return true
	}
	filtered := make([]service.Account, 0, len(accounts))
	platform := strings.TrimSpace(c.Query("platform"))
	accountType := strings.TrimSpace(c.Query("type"))
	status := strings.TrimSpace(c.Query("status"))
	search := strings.TrimSpace(c.Query("search"))
	for _, account := range accounts {
		if account != nil && astrBotAccountMatches(*account, platform, accountType, status, search) {
			filtered = append(filtered, *account)
		}
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].ID < filtered[j].ID })
	items, total := paginateAstrBotItems(filtered, page, pageSize)
	response.Paginated(c, items, total, page, pageSize)
	return true
}

func (h *AstrBotHandler) listAllowedChannels(c *gin.Context, token *service.AstrBotToken, page, pageSize int) bool {
	if len(token.ResourceAllowlist.ChannelIDs) == 0 {
		response.NotFound(c, "resource not found")
		return true
	}
	channels := make([]service.Channel, 0, len(token.ResourceAllowlist.ChannelIDs))
	for _, id := range token.ResourceAllowlist.ChannelIDs {
		channel, err := h.channels.GetByID(c.Request.Context(), id)
		if err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			response.ErrorFrom(c, err)
			return true
		}
		if channel != nil && astrBotChannelMatches(*channel, strings.TrimSpace(c.Query("status")), strings.TrimSpace(c.Query("search"))) {
			channels = append(channels, *channel)
		}
	}
	sort.Slice(channels, func(i, j int) bool { return channels[i].ID < channels[j].ID })
	items, total := paginateAstrBotItems(channels, page, pageSize)
	response.Paginated(c, items, total, page, pageSize)
	return true
}

func requireAstrBotOperationResource(c *gin.Context, operationType string, targetID *int64) bool {
	switch operationType {
	case service.AstrBotOperationTypeAccountToggle, service.AstrBotOperationTypeAccountRateMultiplier:
		if targetID == nil {
			response.NotFound(c, "resource not found")
			return false
		}
		return requireAstrBotAccountResource(c, *targetID)
	case service.AstrBotOperationTypeChannelToggle, service.AstrBotOperationTypeChannelPricing:
		if targetID == nil {
			response.NotFound(c, "resource not found")
			return false
		}
		return requireAstrBotChannelResource(c, *targetID)
	case service.AstrBotOperationTypeCacheRefresh:
		return requireAstrBotGlobalResource(c)
	default:
		response.NotFound(c, "resource not found")
		return false
	}
}

// ListTokens lists safe token metadata owned by the authenticated user.
func (h *AstrBotHandler) ListTokens(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "unauthorized")
		return
	}
	tokens, err := h.tokens.List(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	out := make([]astrBotTokenDTO, 0, len(tokens))
	for _, token := range tokens {
		out = append(out, toAstrBotTokenDTO(token))
	}
	response.Success(c, gin.H{"tokens": out})
}

// CreateToken returns the raw bearer exactly once per idempotency key. The
// replay record contains only encrypted token material and is never returned
// directly by the generic idempotency response cache.
func (h *AstrBotHandler) CreateToken(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "unauthorized")
		return
	}
	key, err := service.NormalizeIdempotencyKey(c.GetHeader("Idempotency-Key"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if key == "" {
		response.ErrorFrom(c, service.ErrIdempotencyKeyRequired)
		return
	}
	var req createAstrBotTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid token request")
		return
	}
	actorScope := "admin-user:" + strconv.FormatInt(subject.UserID, 10)
	fingerprint, err := service.BuildIdempotencyFingerprint("POST", c.FullPath(), actorScope, req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	actorRole, _ := middleware.GetUserRoleFromContext(c)
	actorEmail := c.GetString(middleware.ContextKeyAuthEmail)
	requestID, _ := c.Request.Context().Value(ctxkey.RequestID).(string)
	auditEntry := &service.AuditLog{
		ActorUserID: subjectUserIDPtr(subject.UserID),
		ActorEmail:  actorEmail,
		ActorRole:   actorRole,
		AuthMethod:  c.GetString("auth_method"),
		Action:      service.AuditActionAstrBotTokenCreate,
		Method:      c.Request.Method,
		Path:        c.FullPath(),
		RequestID:   requestID,
		ClientIP:    middleware.SecurityClientIP(c),
		UserAgent:   c.Request.UserAgent(),
		StatusCode:  201,
		Extra: map[string]any{
			"token_name":            strings.TrimSpace(req.Name),
			"token_scopes":          append([]string(nil), req.Scopes...),
			"token_installation_id": strings.TrimSpace(req.InstallationID),
			"token_expiry":          formatAuditTime(req.ExpiresAt),
		},
	}
	created, err := h.tokens.Create(c.Request.Context(), service.CreateAstrBotTokenInput{
		UserID: subject.UserID, Name: req.Name, Scopes: req.Scopes,
		InstallationID: req.InstallationID, ResourceAllowlist: req.ResourceAllowlist,
		ExpiresAt:          req.ExpiresAt,
		IdempotencyScope:   "astrbot:token:create:" + strconv.FormatInt(subject.UserID, 10),
		IdempotencyKeyHash: service.HashIdempotencyKey(key), RequestFingerprint: fingerprint,
		Audit: auditEntry,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if created.Replayed {
		c.Header("X-Idempotency-Replayed", "true")
	}
	response.Created(c, createdAstrBotTokenDTO{
		Token: toAstrBotTokenDTO(created.Token), RawToken: created.RawToken,
		InstallationSecret: created.InstallationSecret,
	})
}

func formatAuditTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func subjectUserIDPtr(userID int64) *int64 {
	return &userID
}

func (h *AstrBotHandler) RevokeToken(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "unauthorized")
		return
	}
	if err := h.tokens.Revoke(c.Request.Context(), subject.UserID, strings.TrimSpace(c.Param("token_id"))); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"revoked": true})
}

type astrBotTimeRange struct {
	start time.Time
	end   time.Time
}

func parseAstrBotTimeRange(c *gin.Context) (astrBotTimeRange, error) {
	now := time.Now().UTC()
	end := now
	start := now.Add(-24 * time.Hour)
	if raw := strings.TrimSpace(c.Query("start")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return astrBotTimeRange{}, errors.BadRequest("INVALID_TIME_RANGE", "start must be RFC3339")
		}
		start = parsed
	}
	if raw := strings.TrimSpace(c.Query("end")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return astrBotTimeRange{}, errors.BadRequest("INVALID_TIME_RANGE", "end must be RFC3339")
		}
		end = parsed
	}
	if !start.Before(end) || end.Sub(start) > 366*24*time.Hour {
		return astrBotTimeRange{}, errors.BadRequest("INVALID_TIME_RANGE", "invalid time range")
	}
	return astrBotTimeRange{start: start, end: end}, nil
}

func (h *AstrBotHandler) Costs(c *gin.Context) {
	if !requireAstrBotGlobalResource(c) {
		return
	}
	rangeValue, err := parseAstrBotTimeRange(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	stats, err := h.usage.GetGlobalStats(c.Request.Context(), rangeValue.start, rangeValue.end)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"start": rangeValue.start, "end": rangeValue.end, "standard_cost": stats.TotalCost, "actual_cost": stats.TotalActualCost, "account_cost": stats.TotalAccountCost, "requests": stats.TotalRequests})
}

func (h *AstrBotHandler) Profit(c *gin.Context) {
	if !requireAstrBotGlobalResource(c) {
		return
	}
	rangeValue, err := parseAstrBotTimeRange(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	stats, err := h.usage.GetGlobalStats(c.Request.Context(), rangeValue.start, rangeValue.end)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	accountCost := 0.0
	if stats.TotalAccountCost != nil {
		accountCost = *stats.TotalAccountCost
	}
	response.Success(c, gin.H{"start": rangeValue.start, "end": rangeValue.end, "revenue": stats.TotalActualCost, "cost": accountCost, "profit": stats.TotalActualCost - accountCost, "requests": stats.TotalRequests})
}

func (h *AstrBotHandler) Consumption(c *gin.Context) {
	if !requireAstrBotGlobalResource(c) {
		return
	}
	rangeValue, err := parseAstrBotTimeRange(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	stats, err := h.usage.GetGlobalStats(c.Request.Context(), rangeValue.start, rangeValue.end)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"start": rangeValue.start, "end": rangeValue.end, "requests": stats.TotalRequests, "input_tokens": stats.TotalInputTokens, "output_tokens": stats.TotalOutputTokens, "cache_tokens": stats.TotalCacheTokens, "total_tokens": stats.TotalTokens})
}

type astrBotAccountDTO struct {
	ID             int64      `json:"id"`
	Name           string     `json:"name"`
	Platform       string     `json:"platform"`
	Type           string     `json:"type"`
	Status         string     `json:"status"`
	Schedulable    bool       `json:"schedulable"`
	RateMultiplier float64    `json:"rate_multiplier"`
	Concurrency    int        `json:"concurrency"`
	Priority       int        `json:"priority"`
	GroupIDs       []int64    `json:"group_ids,omitempty"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
}

func toAstrBotAccountDTO(account *service.Account) astrBotAccountDTO {
	if account == nil {
		return astrBotAccountDTO{}
	}
	return astrBotAccountDTO{ID: account.ID, Name: account.Name, Platform: account.Platform, Type: account.Type, Status: account.Status, Schedulable: account.Schedulable, RateMultiplier: account.BillingRateMultiplier(), Concurrency: account.Concurrency, Priority: account.Priority, GroupIDs: append([]int64(nil), account.GroupIDs...), LastUsedAt: account.LastUsedAt, ExpiresAt: account.ExpiresAt}
}

func (h *AstrBotHandler) AccountsStatus(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	token, ok := astrBotTokenFromContext(c)
	if !ok {
		response.NotFound(c, "resource not found")
		return
	}
	if !token.ResourceAllowlist.IsUnrestricted() {
		h.listAllowedAccounts(c, token, page, pageSize)
		return
	}
	accounts, total, err := h.admin.ListAccounts(c.Request.Context(), page, pageSize, strings.TrimSpace(c.Query("platform")), strings.TrimSpace(c.Query("type")), strings.TrimSpace(c.Query("status")), strings.TrimSpace(c.Query("search")), 0, "", "id", "asc")
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	items := make([]astrBotAccountDTO, 0, len(accounts))
	for i := range accounts {
		items = append(items, toAstrBotAccountDTO(&accounts[i]))
	}
	response.Paginated(c, items, total, page, pageSize)
}

type astrBotChannelDTO struct {
	ID                 int64   `json:"id"`
	Name               string  `json:"name"`
	Status             string  `json:"status"`
	BillingModelSource string  `json:"billing_model_source"`
	RestrictModels     bool    `json:"restrict_models"`
	GroupIDs           []int64 `json:"group_ids,omitempty"`
	ModelCount         int     `json:"model_count"`
}

func toAstrBotChannelDTO(channel *service.Channel) astrBotChannelDTO {
	if channel == nil {
		return astrBotChannelDTO{}
	}
	return astrBotChannelDTO{ID: channel.ID, Name: channel.Name, Status: channel.Status, BillingModelSource: channel.BillingModelSource, RestrictModels: channel.RestrictModels, GroupIDs: append([]int64(nil), channel.GroupIDs...), ModelCount: len(channel.ModelPricing)}
}

func (h *AstrBotHandler) ChannelsStatus(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	token, ok := astrBotTokenFromContext(c)
	if !ok {
		response.NotFound(c, "resource not found")
		return
	}
	if !token.ResourceAllowlist.IsUnrestricted() {
		h.listAllowedChannels(c, token, page, pageSize)
		return
	}
	channels, result, err := h.channels.List(c.Request.Context(), pagination.PaginationParams{Page: page, PageSize: pageSize, SortBy: "id", SortOrder: pagination.SortOrderAsc}, strings.TrimSpace(c.Query("status")), strings.TrimSpace(c.Query("search")))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	items := make([]astrBotChannelDTO, 0, len(channels))
	for i := range channels {
		items = append(items, toAstrBotChannelDTO(&channels[i]))
	}
	if result == nil {
		response.Paginated(c, items, 0, page, pageSize)
		return
	}
	response.Paginated(c, items, result.Total, result.Page, result.PageSize)
}

type astrBotPricingDTO struct {
	Model            string  `json:"model"`
	Provider         string  `json:"provider"`
	InputPrice       float64 `json:"input_price"`
	OutputPrice      float64 `json:"output_price"`
	CacheWritePrice  float64 `json:"cache_write_price"`
	CacheReadPrice   float64 `json:"cache_read_price"`
	ImageInputPrice  float64 `json:"image_input_price"`
	ImageOutputPrice float64 `json:"image_output_price"`
	Mode             string  `json:"mode"`
}

func (h *AstrBotHandler) ModelPrices(c *gin.Context) {
	if !requireAstrBotGlobalResource(c) {
		return
	}
	catalog := h.pricing.ListModelPricing()
	items := make([]astrBotPricingDTO, 0, len(catalog))
	for model, price := range catalog {
		items = append(items, astrBotPricingDTO{Model: model, Provider: price.LiteLLMProvider, InputPrice: price.InputCostPerToken, OutputPrice: price.OutputCostPerToken, CacheWritePrice: price.CacheCreationInputTokenCost, CacheReadPrice: price.CacheReadInputTokenCost, ImageInputPrice: price.InputCostPerImageToken, ImageOutputPrice: price.OutputCostPerImage, Mode: price.Mode})
	}
	response.Success(c, gin.H{"models": items, "status": h.pricing.GetStatus()})
}

type astrBotToggleRequest struct {
	Enabled bool `json:"enabled"`
}

type astrBotRateMultiplierRequest struct {
	RateMultiplier float64 `json:"rate_multiplier"`
}

type astrBotPricingIntervalRequest struct {
	MinTokens       int      `json:"min_tokens"`
	MaxTokens       *int     `json:"max_tokens,omitempty"`
	TierLabel       string   `json:"tier_label,omitempty"`
	InputPrice      *float64 `json:"input_price,omitempty"`
	OutputPrice     *float64 `json:"output_price,omitempty"`
	CacheWritePrice *float64 `json:"cache_write_price,omitempty"`
	CacheReadPrice  *float64 `json:"cache_read_price,omitempty"`
	PerRequestPrice *float64 `json:"per_request_price,omitempty"`
	SortOrder       int      `json:"sort_order,omitempty"`
}

type astrBotPricingEntryRequest struct {
	Platform         string                          `json:"platform"`
	Models           []string                        `json:"models" binding:"required"`
	BillingMode      service.BillingMode             `json:"billing_mode"`
	InputPrice       *float64                        `json:"input_price,omitempty"`
	OutputPrice      *float64                        `json:"output_price,omitempty"`
	CacheWritePrice  *float64                        `json:"cache_write_price,omitempty"`
	CacheReadPrice   *float64                        `json:"cache_read_price,omitempty"`
	ImageInputPrice  *float64                        `json:"image_input_price,omitempty"`
	ImageOutputPrice *float64                        `json:"image_output_price,omitempty"`
	PerRequestPrice  *float64                        `json:"per_request_price,omitempty"`
	Intervals        []astrBotPricingIntervalRequest `json:"intervals,omitempty"`
}

type astrBotPricingRequest struct {
	Pricing []astrBotPricingEntryRequest `json:"pricing" binding:"required"`
}

func validAstrBotPrice(value *float64) bool {
	return value == nil || (!math.IsNaN(*value) && !math.IsInf(*value, 0))
}

func (r astrBotPricingRequest) toServicePricing() ([]service.ChannelModelPricing, error) {
	if len(r.Pricing) == 0 {
		return nil, errors.BadRequest("INVALID_PRICING", "pricing must contain at least one entry")
	}
	pricing := make([]service.ChannelModelPricing, 0, len(r.Pricing))
	for _, entry := range r.Pricing {
		platform := strings.TrimSpace(entry.Platform)
		if platform == "" || len(entry.Models) == 0 {
			return nil, errors.BadRequest("INVALID_PRICING", "each pricing entry requires a platform and models")
		}
		for _, price := range []*float64{
			entry.InputPrice, entry.OutputPrice, entry.CacheWritePrice, entry.CacheReadPrice,
			entry.ImageInputPrice, entry.ImageOutputPrice, entry.PerRequestPrice,
		} {
			if !validAstrBotPrice(price) {
				return nil, errors.BadRequest("INVALID_PRICING", "pricing values must be finite numbers")
			}
		}
		models := make([]string, 0, len(entry.Models))
		for _, model := range entry.Models {
			model = strings.TrimSpace(model)
			if model == "" {
				return nil, errors.BadRequest("INVALID_PRICING", "model names must not be empty")
			}
			models = append(models, model)
		}
		intervals := make([]service.PricingInterval, 0, len(entry.Intervals))
		for _, interval := range entry.Intervals {
			if interval.MinTokens < 0 || (interval.MaxTokens != nil && *interval.MaxTokens <= interval.MinTokens) {
				return nil, errors.BadRequest("INVALID_PRICING", "pricing interval bounds are invalid")
			}
			for _, price := range []*float64{
				interval.InputPrice, interval.OutputPrice, interval.CacheWritePrice,
				interval.CacheReadPrice, interval.PerRequestPrice,
			} {
				if !validAstrBotPrice(price) {
					return nil, errors.BadRequest("INVALID_PRICING", "pricing values must be finite numbers")
				}
			}
			intervals = append(intervals, service.PricingInterval{
				MinTokens: interval.MinTokens, MaxTokens: interval.MaxTokens, TierLabel: interval.TierLabel,
				InputPrice: interval.InputPrice, OutputPrice: interval.OutputPrice,
				CacheWritePrice: interval.CacheWritePrice, CacheReadPrice: interval.CacheReadPrice,
				PerRequestPrice: interval.PerRequestPrice, SortOrder: interval.SortOrder,
			})
		}
		pricing = append(pricing, service.ChannelModelPricing{
			Platform: platform, Models: models, BillingMode: entry.BillingMode,
			InputPrice: entry.InputPrice, OutputPrice: entry.OutputPrice, CacheWritePrice: entry.CacheWritePrice,
			CacheReadPrice: entry.CacheReadPrice, ImageInputPrice: entry.ImageInputPrice,
			ImageOutputPrice: entry.ImageOutputPrice, PerRequestPrice: entry.PerRequestPrice, Intervals: intervals,
		})
	}
	return pricing, nil
}

func (h *AstrBotHandler) executeWrite(c *gin.Context, payload any, execute func(context.Context) (any, error)) {
	binding, err := astrBotOperationBindingFromContext(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	key, err := astrBotOperationRequestKey(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	operationID := strings.TrimSpace(c.GetHeader("X-AstrBot-Operation-ID"))
	if operationID == "" {
		response.ErrorFrom(c, service.ErrAstrBotOperationState)
		return
	}
	operationType, targetID, err := astrBotOperationIdentity(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if !requireAstrBotOperationResource(c, operationType, targetID) {
		return
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		response.ErrorFrom(c, service.ErrAstrBotOperationPayload)
		return
	}
	input := service.AstrBotOperationExecuteInput{
		OperationID: operationID, UserID: binding.Principal.UserID, TokenID: binding.Principal.TokenID,
		InstallationID: binding.InstallationID, PlatformUserHash: binding.PlatformUserHash,
		SessionHash: binding.SessionHash, ConversationHash: binding.ConversationHash,
		OperationType: operationType, TargetID: targetID, Payload: json.RawMessage(rawPayload),
		IdempotencyKeyHash: service.HashIdempotencyKey(key),
	}
	result, err := h.executeConfirmedWrite(c, input, payload, execute)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if result.Replayed {
		c.Header("X-Idempotency-Replayed", "true")
	}
	response.Success(c, result.Data)
}

func astrBotOperationIdentity(c *gin.Context) (string, *int64, error) {
	targetID := (*int64)(nil)
	if raw := strings.TrimSpace(c.Param("id")); raw != "" {
		id, err := parseAstrBotID(raw)
		if err != nil {
			return "", nil, err
		}
		targetID = &id
	}
	switch c.FullPath() {
	case "/api/v1/bot/channels/:id/toggle":
		return service.AstrBotOperationTypeChannelToggle, targetID, nil
	case "/api/v1/bot/accounts/:id/toggle":
		return service.AstrBotOperationTypeAccountToggle, targetID, nil
	case "/api/v1/bot/accounts/:id/rate-multiplier":
		return service.AstrBotOperationTypeAccountRateMultiplier, targetID, nil
	case "/api/v1/bot/channels/:id/pricing":
		return service.AstrBotOperationTypeChannelPricing, targetID, nil
	case "/api/v1/bot/cache/refresh":
		return service.AstrBotOperationTypeCacheRefresh, nil, nil
	default:
		return "", nil, service.ErrAstrBotOperationType
	}
}

func (h *AstrBotHandler) executeConfirmedWrite(c *gin.Context, input service.AstrBotOperationExecuteInput, payload any, execute func(context.Context) (any, error)) (*service.IdempotencyExecuteResult, error) {
	if h.operations == nil {
		return nil, service.ErrAstrBotOperationStore
	}
	if h.idempotency == nil {
		return nil, service.ErrIdempotencyStoreUnavail
	}
	key, err := astrBotOperationRequestKey(c)
	if err != nil {
		return nil, err
	}
	return h.idempotency.Execute(c.Request.Context(), service.IdempotencyExecuteOptions{
		Scope:      "astrbot:operation:" + input.OperationID,
		ActorScope: "astrbot-token:" + input.TokenID,
		Method:     c.Request.Method, Route: c.FullPath(), IdempotencyKey: key,
		Payload: payload, TTL: service.DefaultWriteIdempotencyTTL(), RequireKey: true,
	}, func(ctx context.Context) (any, error) {
		op, claimed, claimErr := h.operations.Claim(ctx, input)
		if claimErr != nil {
			return nil, claimErr
		}
		if !claimed {
			return service.DecodeAstrBotOperationResponse(op)
		}
		data, mutationErr := execute(ctx)
		if mutationErr != nil {
			_ = h.operations.MarkFailedRetryable(ctx, op.OperationID, mutationErr)
			return nil, mutationErr
		}
		if err := h.operations.MarkSucceeded(ctx, op.OperationID, data); err != nil {
			return nil, err
		}
		return data, nil
	})
}

func (h *AstrBotHandler) ToggleChannel(c *gin.Context) {
	id, err := parseAstrBotID(c.Param("id"))

	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var req astrBotToggleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid toggle request")
		return
	}
	h.executeWrite(c, req, func(ctx context.Context) (any, error) {
		status := service.StatusDisabled
		if req.Enabled {
			status = service.StatusActive
		}
		channel, err := h.channels.Update(ctx, id, &service.UpdateChannelInput{Status: status})
		if err != nil {
			return nil, err
		}
		return gin.H{"channel": toAstrBotChannelDTO(channel), "enabled": req.Enabled}, nil
	})
}

func (h *AstrBotHandler) ToggleAccount(c *gin.Context) {
	id, err := parseAstrBotID(c.Param("id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var req astrBotToggleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid toggle request")
		return
	}
	h.executeWrite(c, req, func(ctx context.Context) (any, error) {
		status := service.StatusDisabled
		if req.Enabled {
			status = service.StatusActive
		}
		account, err := h.admin.UpdateAccount(ctx, id, &service.UpdateAccountInput{Status: status})
		if err != nil {
			return nil, err
		}
		return gin.H{"account": toAstrBotAccountDTO(account), "enabled": req.Enabled}, nil
	})
}

func (h *AstrBotHandler) UpdateAccountRateMultiplier(c *gin.Context) {
	id, err := parseAstrBotID(c.Param("id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var req astrBotRateMultiplierRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.RateMultiplier < 0 {
		response.BadRequest(c, "invalid rate multiplier")
		return
	}
	h.executeWrite(c, req, func(ctx context.Context) (any, error) {
		rateMultiplier := req.RateMultiplier
		account, err := h.admin.UpdateAccount(ctx, id, &service.UpdateAccountInput{RateMultiplier: &rateMultiplier})
		if err != nil {
			return nil, err
		}
		return gin.H{"account": toAstrBotAccountDTO(account)}, nil
	})
}

func (h *AstrBotHandler) UpdateChannelPricing(c *gin.Context) {
	id, err := parseAstrBotID(c.Param("id"))

	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var req astrBotPricingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid pricing request")
		return
	}
	pricing, err := req.toServicePricing()
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	h.executeWrite(c, req, func(ctx context.Context) (any, error) {
		channel, err := h.channels.Update(ctx, id, &service.UpdateChannelInput{ModelPricing: &pricing})
		if err != nil {
			return nil, err
		}
		return gin.H{"channel": toAstrBotChannelDTO(channel)}, nil
	})
}

func (h *AstrBotHandler) RefreshCache(c *gin.Context) {
	h.executeWrite(c, gin.H{"refresh": true}, func(ctx context.Context) (any, error) {

		if err := h.channels.RefreshCache(ctx); err != nil {
			return nil, err
		}
		if err := h.pricing.ForceUpdate(); err != nil {
			return nil, err
		}
		return gin.H{"refreshed": true}, nil
	})
}

func parseAstrBotID(raw string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.BadRequest("INVALID_ID", "invalid resource id")
	}
	return id, nil
}
