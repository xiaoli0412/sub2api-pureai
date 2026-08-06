package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

// GetChannelMonitorLogStatusBatch 按监控实例批量聚合最近 window 内的请求日志。
// ChannelID 为 0 时保持账号级聚合兼容行为；非 0 时只统计指定渠道。
// 成功请求 = actual_cost > 0（与 dashboard 一致，覆盖 token / 按次 / 按图计费）。
// duration_ms 用 AVG 聚合得到平均延迟，NULL 时不参与平均。
//
// 返回 map[monitorID]map[model]*ChannelMonitorLogStatus。无日志的目标不出现在结果里。
func (r *usageLogRepository) GetChannelMonitorLogStatusBatch(
	ctx context.Context,
	targets []service.ChannelMonitorLogTarget,
	window time.Duration,
) (map[int64]map[string]*service.ChannelMonitorLogStatus, error) {
	out := make(map[int64]map[string]*service.ChannelMonitorLogStatus, len(targets))
	if len(targets) == 0 || window <= 0 {
		return out, nil
	}

	monitorIDs := make([]int64, 0, len(targets))
	accountIDs := make([]int64, 0, len(targets))
	channelIDs := make([]int64, 0, len(targets))
	models := make([]string, 0, len(targets))
	for _, target := range targets {
		if target.MonitorID == 0 || target.AccountID == 0 {
			continue
		}
		channelID := int64(0)
		if target.ChannelID != nil {
			channelID = *target.ChannelID
		}
		targetModels := target.Models
		if len(targetModels) == 0 {
			monitorIDs = append(monitorIDs, target.MonitorID)
			accountIDs = append(accountIDs, target.AccountID)
			channelIDs = append(channelIDs, channelID)
			models = append(models, "")
			continue
		}
		for _, model := range targetModels {
			model = strings.TrimSpace(model)
			if model == "" {
				continue
			}
			monitorIDs = append(monitorIDs, target.MonitorID)
			accountIDs = append(accountIDs, target.AccountID)
			channelIDs = append(channelIDs, channelID)
			models = append(models, model)
		}
	}
	if len(monitorIDs) == 0 {
		return out, nil
	}

	cutoff := time.Now().Add(-window)
	const q = `
		WITH targets AS (
		    SELECT unnest($1::bigint[]) AS monitor_id,
		           unnest($2::bigint[]) AS account_id,
		           unnest($3::bigint[]) AS channel_id,
		           unnest($4::text[])   AS model
		)
		SELECT
		    t.monitor_id,
		    ul.account_id,
		    ul.model,
		    COUNT(*)                                                       AS total_requests,
		    COUNT(*) FILTER (WHERE ul.actual_cost > 0)                     AS success_requests,
		    CASE WHEN COUNT(ul.duration_ms) > 0
		         THEN AVG(ul.duration_ms)
		         ELSE NULL END                                             AS avg_latency_ms,
		    MAX(ul.created_at)                                             AS last_request_at
		FROM usage_logs ul
		JOIN targets t
		  ON t.account_id = ul.account_id
		 AND (t.channel_id = 0 OR ul.channel_id = t.channel_id)
		 AND (t.model = '' OR t.model = ul.model)
		WHERE ul.created_at >= $5
		GROUP BY t.monitor_id, ul.account_id, ul.model
	`
	rows, err := r.sql.QueryContext(
		ctx,
		q,
		pq.Array(monitorIDs),
		pq.Array(accountIDs),
		pq.Array(channelIDs),
		pq.Array(models),
		cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("query channel monitor log status batch: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			monitorID       int64
			accountID       int64
			model           sql.NullString
			totalRequests   int64
			successRequests int64
			avgLatency      sql.NullFloat64
			lastRequestAt   sql.NullTime
		)
		if err := rows.Scan(&monitorID, &accountID, &model, &totalRequests, &successRequests, &avgLatency, &lastRequestAt); err != nil {
			return nil, fmt.Errorf("scan channel monitor log status row: %w", err)
		}
		if !model.Valid || strings.TrimSpace(model.String) == "" {
			continue
		}
		entry := &service.ChannelMonitorLogStatus{
			AccountID:       accountID,
			Model:           model.String,
			TotalRequests:   int(totalRequests),
			SuccessRequests: int(successRequests),
		}
		if avgLatency.Valid {
			v := int(avgLatency.Float64)
			entry.AvgLatencyMs = &v
		}
		if lastRequestAt.Valid {
			t := lastRequestAt.Time
			entry.LastRequestAt = &t
		}
		if _, ok := out[monitorID]; !ok {
			out[monitorID] = make(map[string]*service.ChannelMonitorLogStatus)
		}
		out[monitorID][model.String] = entry
	}
	return out, rows.Err()
}
