package handler

import (
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const (
	astrBotModelAuditDefaultPageSize = 20
	astrBotModelAuditMaxPageSize     = 100
)

func parseAstrBotStrictPositiveInt(c *gin.Context, name string, defaultValue, maxValue int) (int, error) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > maxValue {
		return 0, infraerrors.BadRequest("INVALID_PAGINATION", name+" must be a positive integer within the allowed range")
	}
	return value, nil
}

func parseAstrBotModelAuditFilter(c *gin.Context) (service.AstrBotModelAuditFilter, error) {
	rangeValue, err := parseAstrBotTimeRange(c)
	if err != nil {
		return service.AstrBotModelAuditFilter{}, err
	}
	page, err := parseAstrBotStrictPositiveInt(c, "page", 1, 1000000)
	if err != nil {
		return service.AstrBotModelAuditFilter{}, err
	}
	pageSize, err := parseAstrBotStrictPositiveInt(c, "page_size", astrBotModelAuditDefaultPageSize, astrBotModelAuditMaxPageSize)
	if err != nil {
		return service.AstrBotModelAuditFilter{}, err
	}
	filter := service.AstrBotModelAuditFilter{
		StartTime:             rangeValue.start,
		EndTime:               rangeValue.end,
		RequestedModel:        boundedAstrBotModelQuery(c, "requested_model", "model", 100),
		UpstreamModel:         boundedAstrBotModelQuery(c, "upstream_model", "", 100),
		UpstreamResponseModel: boundedAstrBotModelQuery(c, "upstream_response_model", "", 200),
		Page:                  page,
		PageSize:              pageSize,
	}
	for name, target := range map[string]*int64{
		"account_id": &filter.AccountID,
		"channel_id": &filter.ChannelID,
		"group_id":   &filter.GroupID,
		"user_id":    &filter.UserID,
	} {
		if value, present, parseErr := parseAstrBotOptionalID(c, name); parseErr != nil {
			return service.AstrBotModelAuditFilter{}, parseErr
		} else if present {
			*target = value
		}
	}
	if raw := strings.TrimSpace(c.Query("mismatch")); raw != "" {
		value, parseErr := strconv.ParseBool(raw)
		if parseErr != nil {
			return service.AstrBotModelAuditFilter{}, infraerrors.BadRequest("INVALID_MISMATCH", "mismatch must be true or false")
		}
		filter.Mismatch = &value
	}
	if token, ok := astrBotTokenFromContext(c); ok {
		if filter.AccountID > 0 && !token.ResourceAllowlist.AllowsAccount(filter.AccountID) {
			return service.AstrBotModelAuditFilter{}, infraerrors.NotFound("RESOURCE_NOT_FOUND", "resource not found")
		}
		if filter.ChannelID > 0 && !token.ResourceAllowlist.AllowsChannel(filter.ChannelID) {
			return service.AstrBotModelAuditFilter{}, infraerrors.NotFound("RESOURCE_NOT_FOUND", "resource not found")
		}
		if filter.GroupID > 0 && !token.ResourceAllowlist.AllowsGroup(filter.GroupID) {
			return service.AstrBotModelAuditFilter{}, infraerrors.NotFound("RESOURCE_NOT_FOUND", "resource not found")
		}
		if filter.UserID > 0 && !token.ResourceAllowlist.AllowsUser(filter.UserID) {
			return service.AstrBotModelAuditFilter{}, infraerrors.NotFound("RESOURCE_NOT_FOUND", "resource not found")
		}
		filter.ResourceAllowlist = token.ResourceAllowlist
	}
	return filter, nil
}

func boundedAstrBotModelQuery(c *gin.Context, primary, fallback string, max int) string {
	value := strings.TrimSpace(c.Query(primary))
	if value == "" && fallback != "" {
		value = strings.TrimSpace(c.Query(fallback))
	}
	if len(value) > max {
		return value[:max]
	}
	return value
}

func parseAstrBotOptionalID(c *gin.Context, name string) (int64, bool, error) {
	raw, present := c.GetQuery(name)
	if !present || strings.TrimSpace(raw) == "" {
		return 0, false, nil
	}
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value <= 0 {
		return 0, false, infraerrors.BadRequest("INVALID_RESOURCE_ID", name+" must be a positive integer")
	}
	return value, true, nil
}

type astrBotModelAuditAggregateDTO struct {
	RequestedModel        string  `json:"requested_model"`
	UpstreamModel         *string `json:"upstream_model,omitempty"`
	UpstreamResponseModel *string `json:"upstream_response_model,omitempty"`
	Mismatch              *bool   `json:"upstream_model_mismatch"`
	Requests              int64   `json:"requests"`
	ObservedRequests      int64   `json:"observed_requests"`
	MismatchCount         int64   `json:"mismatch_count"`
	MatchCount            int64   `json:"match_count"`
	NoResponseModelCount  int64   `json:"no_response_model_count"`
	MismatchRate          float64 `json:"mismatch_rate"`
}

type astrBotModelAuditSampleDTO struct {
	ID                    int64     `json:"id"`
	CreatedAt             time.Time `json:"created_at"`
	RequestID             string    `json:"request_id,omitempty"`
	RequestedModel        string    `json:"requested_model"`
	UpstreamModel         *string   `json:"upstream_model,omitempty"`
	UpstreamResponseModel *string   `json:"upstream_response_model,omitempty"`
	Mismatch              *bool     `json:"upstream_model_mismatch"`
	AccountID             int64     `json:"account_id"`
	ChannelID             *int64    `json:"channel_id,omitempty"`
	GroupID               *int64    `json:"group_id,omitempty"`
	UserID                int64     `json:"user_id"`
	Provider              string    `json:"provider,omitempty"`
	UpstreamEndpoint      string    `json:"upstream_endpoint,omitempty"`
}

func toAstrBotModelAuditAggregateDTO(value service.AstrBotModelAuditAggregate) astrBotModelAuditAggregateDTO {
	return astrBotModelAuditAggregateDTO{
		RequestedModel:        value.RequestedModel,
		UpstreamModel:         value.UpstreamModel,
		UpstreamResponseModel: value.UpstreamResponseModel,
		Mismatch:              value.Mismatch,
		Requests:              value.Requests,
		ObservedRequests:      value.ObservedRequests,
		MismatchCount:         value.MismatchCount,
		MatchCount:            value.MatchCount,
		NoResponseModelCount:  value.NoResponseModelCount,
		MismatchRate:          value.MismatchRate,
	}
}

func toAstrBotModelAuditSampleDTO(value service.AstrBotModelAuditSample) astrBotModelAuditSampleDTO {
	return astrBotModelAuditSampleDTO{
		ID:                    value.ID,
		CreatedAt:             value.CreatedAt,
		RequestID:             value.RequestID,
		RequestedModel:        value.RequestedModel,
		UpstreamModel:         value.UpstreamModel,
		UpstreamResponseModel: value.UpstreamResponseModel,
		Mismatch:              value.Mismatch,
		AccountID:             value.AccountID,
		ChannelID:             value.ChannelID,
		GroupID:               value.GroupID,
		UserID:                value.UserID,
		Provider:              value.Provider,
		UpstreamEndpoint:      value.UpstreamEndpoint,
	}
}

func (h *AstrBotHandler) ModelAudit(c *gin.Context) {
	filter, err := parseAstrBotModelAuditFilter(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result, err := h.usage.GetAstrBotModelAudit(c.Request.Context(), filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	aggregates := make([]astrBotModelAuditAggregateDTO, 0, len(result.Aggregates))
	for _, value := range result.Aggregates {
		aggregates = append(aggregates, toAstrBotModelAuditAggregateDTO(value))
	}
	samples := make([]astrBotModelAuditSampleDTO, 0, len(result.Samples))
	for _, value := range result.Samples {
		samples = append(samples, toAstrBotModelAuditSampleDTO(value))
	}
	response.Success(c, gin.H{
		"start":                   filter.StartTime,
		"end":                     filter.EndTime,
		"requested_model":         filter.RequestedModel,
		"upstream_model":          filter.UpstreamModel,
		"upstream_response_model": filter.UpstreamResponseModel,
		"mismatch":                filter.Mismatch,
		"total_requests":          result.TotalRequests,
		"observed_requests":       result.ObservedRequests,
		"mismatch_count":          result.MismatchCount,
		"match_count":             result.MatchCount,
		"mismatch_rate":           result.MismatchRate,
		"no_response_model_count": result.NoResponseModelCount,
		"aggregates":              aggregates,
		"samples":                 samples,
		"sample_total":            result.Total,
		"sample_page":             result.Page,
		"sample_page_size":        result.PageSize,
	})
}
