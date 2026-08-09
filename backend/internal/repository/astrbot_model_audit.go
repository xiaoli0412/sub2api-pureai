package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

func (r *usageLogRepository) GetAstrBotModelAudit(ctx context.Context, filter service.AstrBotModelAuditFilter) (*service.AstrBotModelAuditResult, error) {
	where, args := buildAstrBotModelAuditWhere(filter)
	aggregateQuery := fmt.Sprintf(`
		SELECT
			COALESCE(NULLIF(TRIM(ul.requested_model), ''), ul.model) AS requested_model,
			NULLIF(TRIM(ul.upstream_model), '') AS upstream_model,
			NULLIF(TRIM(ul.upstream_response_model), '') AS upstream_response_model,
			ul.upstream_model_mismatch,
			COUNT(*) AS requests,
			COUNT(*) FILTER (WHERE ul.upstream_response_model IS NOT NULL AND NULLIF(TRIM(ul.upstream_response_model), '') IS NOT NULL) AS observed_requests,
			COUNT(*) FILTER (WHERE ul.upstream_model_mismatch IS TRUE) AS mismatch_count,
			COUNT(*) FILTER (WHERE ul.upstream_model_mismatch IS FALSE) AS match_count,
			COUNT(*) FILTER (WHERE ul.upstream_response_model IS NULL OR NULLIF(TRIM(ul.upstream_response_model), '') IS NULL) AS no_response_model_count
		FROM usage_logs ul
		%s
		GROUP BY 1, 2, 3, 4
		ORDER BY requests DESC, requested_model ASC, upstream_model ASC NULLS FIRST, upstream_response_model ASC NULLS FIRST`, where)

	rows, err := r.sql.QueryContext(ctx, aggregateQuery, args...)
	if err != nil {
		return nil, err
	}
	aggregates := make([]service.AstrBotModelAuditAggregate, 0)
	var totalRequests, observedRequests, mismatchCount, matchCount, noResponseCount int64
	for rows.Next() {
		var row service.AstrBotModelAuditAggregate
		if err := rows.Scan(&row.RequestedModel, &row.UpstreamModel, &row.UpstreamResponseModel, &row.Mismatch, &row.Requests, &row.ObservedRequests, &row.MismatchCount, &row.MatchCount, &row.NoResponseModelCount); err != nil {
			_ = rows.Close()
			return nil, err
		}
		row.MismatchRate = service.CalculateAstrBotMismatchRate(row.MismatchCount, row.ObservedRequests)
		aggregates = append(aggregates, row)
		totalRequests += row.Requests
		observedRequests += row.ObservedRequests
		mismatchCount += row.MismatchCount
		matchCount += row.MatchCount
		noResponseCount += row.NoResponseModelCount
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	countQuery := "SELECT COUNT(*) FROM usage_logs ul " + where
	var total int64
	if err := scanSingleRow(ctx, r.sql, countQuery, args, &total); err != nil {
		return nil, err
	}

	page := filter.Page
	pageSize := filter.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	limitPos := len(args) + 1
	offsetPos := len(args) + 2
	sampleQuery := fmt.Sprintf(`
		SELECT ul.id, ul.created_at, ul.request_id,
			COALESCE(NULLIF(TRIM(ul.requested_model), ''), ul.model),
			NULLIF(TRIM(ul.upstream_model), ''),
			NULLIF(TRIM(ul.upstream_response_model), ''),
			ul.upstream_model_mismatch, ul.account_id, ul.channel_id, ul.group_id, ul.user_id,
			COALESCE(NULLIF(TRIM(ul.inbound_endpoint), ''), ''),
			COALESCE(NULLIF(TRIM(ul.upstream_endpoint), ''), '')
		FROM usage_logs ul
		%s
		ORDER BY ul.created_at DESC, ul.id DESC
		LIMIT $%d OFFSET $%d`, where, limitPos, offsetPos)
	sampleArgs := append(append([]any{}, args...), pageSize, (page-1)*pageSize)
	sampleRows, err := r.sql.QueryContext(ctx, sampleQuery, sampleArgs...)
	if err != nil {
		return nil, err
	}
	samples := make([]service.AstrBotModelAuditSample, 0)
	for sampleRows.Next() {
		var row service.AstrBotModelAuditSample
		if err := sampleRows.Scan(&row.ID, &row.CreatedAt, &row.RequestID, &row.RequestedModel, &row.UpstreamModel, &row.UpstreamResponseModel, &row.Mismatch, &row.AccountID, &row.ChannelID, &row.GroupID, &row.UserID, &row.Provider, &row.UpstreamEndpoint); err != nil {
			_ = sampleRows.Close()
			return nil, err
		}
		samples = append(samples, row)
	}
	if err := sampleRows.Err(); err != nil {
		_ = sampleRows.Close()
		return nil, err
	}
	if err := sampleRows.Close(); err != nil {
		return nil, err
	}

	return &service.AstrBotModelAuditResult{
		TotalRequests:        totalRequests,
		ObservedRequests:     observedRequests,
		MismatchCount:        mismatchCount,
		MatchCount:           matchCount,
		NoResponseModelCount: noResponseCount,
		MismatchRate:         service.CalculateAstrBotMismatchRate(mismatchCount, observedRequests),
		Aggregates:           aggregates,
		Samples:              samples,
		Total:                total,
		Page:                 page,
		PageSize:             pageSize,
	}, nil
}

func buildAstrBotModelAuditWhere(filter service.AstrBotModelAuditFilter) (string, []any) {
	conditions := []string{"ul.created_at >= $1", "ul.created_at < $2"}
	args := []any{filter.StartTime, filter.EndTime}
	appendString := func(expression, value string) {
		if value = strings.TrimSpace(value); value != "" {
			conditions = append(conditions, fmt.Sprintf("%s = $%d", expression, len(args)+1))
			args = append(args, value)
		}
	}
	appendString("COALESCE(NULLIF(TRIM(ul.requested_model), ''), ul.model)", filter.RequestedModel)
	appendString("NULLIF(TRIM(ul.upstream_model), '')", filter.UpstreamModel)
	appendString("NULLIF(TRIM(ul.upstream_response_model), '')", filter.UpstreamResponseModel)
	if filter.AccountID > 0 {
		conditions = append(conditions, fmt.Sprintf("ul.account_id = $%d", len(args)+1))
		args = append(args, filter.AccountID)
	}
	if filter.ChannelID > 0 {
		conditions = append(conditions, fmt.Sprintf("(ul.channel_id = $%d OR ul.channel_id = 0)", len(args)+1))
		args = append(args, filter.ChannelID)
	}
	if filter.GroupID > 0 {
		conditions = append(conditions, fmt.Sprintf("ul.group_id = $%d", len(args)+1))
		args = append(args, filter.GroupID)
	}
	if filter.UserID > 0 {
		conditions = append(conditions, fmt.Sprintf("ul.user_id = $%d", len(args)+1))
		args = append(args, filter.UserID)
	}
	if filter.Mismatch != nil {
		if *filter.Mismatch {
			conditions = append(conditions, "ul.upstream_model_mismatch IS TRUE")
		} else {
			conditions = append(conditions, "ul.upstream_model_mismatch IS FALSE")
		}
	}
	allowlist := filter.ResourceAllowlist
	if !allowlist.IsUnrestricted() {
		if len(allowlist.AccountIDs) > 0 {
			conditions = append(conditions, fmt.Sprintf("ul.account_id = ANY($%d)", len(args)+1))
			args = append(args, pq.Array(allowlist.AccountIDs))
		}
		if len(allowlist.ChannelIDs) > 0 {
			conditions = append(conditions, fmt.Sprintf("(ul.channel_id = ANY($%d) OR ul.channel_id = 0)", len(args)+1))
			args = append(args, pq.Array(allowlist.ChannelIDs))
		}
		if len(allowlist.GroupIDs) > 0 {
			conditions = append(conditions, fmt.Sprintf("ul.group_id = ANY($%d)", len(args)+1))
			args = append(args, pq.Array(allowlist.GroupIDs))
		}
		if len(allowlist.UserIDs) > 0 {
			conditions = append(conditions, fmt.Sprintf("ul.user_id = ANY($%d)", len(args)+1))
			args = append(args, pq.Array(allowlist.UserIDs))
		}
	}
	return "WHERE " + strings.Join(conditions, " AND "), args
}

var _ = sql.ErrNoRows
