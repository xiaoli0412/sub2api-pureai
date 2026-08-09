package service

import (
	"context"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var ErrUsageModelAuditUnavailable = infraerrors.ServiceUnavailable("USAGE_MODEL_AUDIT_UNAVAILABLE", "model audit storage is not available")

// AstrBotModelAuditFilter contains bounded, read-only audit filters.
type AstrBotModelAuditFilter struct {
	StartTime             time.Time
	EndTime               time.Time
	RequestedModel        string
	UpstreamModel         string
	UpstreamResponseModel string
	AccountID             int64
	ChannelID             int64
	GroupID               int64
	UserID                int64
	Mismatch              *bool
	Page                  int
	PageSize              int
	ResourceAllowlist     AstrBotResourceAllowlist
}

type AstrBotModelAuditAggregate struct {
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

// AstrBotModelAuditSample deliberately excludes credentials, prompts, headers,
// arbitrary JSON, proxy data, and raw upstream error bodies.
type AstrBotModelAuditSample struct {
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

type AstrBotModelAuditResult struct {
	TotalRequests        int64
	ObservedRequests     int64
	MismatchCount        int64
	MatchCount           int64
	NoResponseModelCount int64
	MismatchRate         float64
	Aggregates           []AstrBotModelAuditAggregate
	Samples              []AstrBotModelAuditSample
	Total                int64
	Page                 int
	PageSize             int
}

// AstrBotModelAuditReader is an optional repository extension. Keeping it
// separate preserves the broad UsageLogRepository contract for existing mocks.
type AstrBotModelAuditReader interface {
	GetAstrBotModelAudit(context.Context, AstrBotModelAuditFilter) (*AstrBotModelAuditResult, error)
}

func (s *UsageService) GetAstrBotModelAudit(ctx context.Context, filter AstrBotModelAuditFilter) (*AstrBotModelAuditResult, error) {
	reader, ok := s.usageRepo.(AstrBotModelAuditReader)
	if !ok {
		return nil, ErrUsageModelAuditUnavailable
	}
	result, err := reader.GetAstrBotModelAudit(ctx, filter)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return &AstrBotModelAuditResult{
			Aggregates: []AstrBotModelAuditAggregate{},
			Samples:    []AstrBotModelAuditSample{},
			Page:       filter.Page,
			PageSize:   filter.PageSize,
		}, nil
	}
	if result.Aggregates == nil {
		result.Aggregates = []AstrBotModelAuditAggregate{}
	}
	if result.Samples == nil {
		result.Samples = []AstrBotModelAuditSample{}
	}
	return result, nil
}

func CalculateAstrBotMismatchRate(mismatch, observed int64) float64 {
	if observed <= 0 || mismatch <= 0 {
		return 0
	}
	return float64(mismatch) / float64(observed)
}

var _ = infraerrors.ServiceUnavailable
