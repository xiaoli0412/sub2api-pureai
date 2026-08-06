package service

import (
	"context"
	"fmt"
	"log/slog"
)

// 渠道监控聚合层：把 latest + availability 拼成 admin/user 视图所需的 summary / detail。
// 所有方法都遵守"失败仅日志，返回零值"的原则，避免 N+1 查询失败拖垮列表渲染。

// BatchMonitorStatusSummary 批量聚合多个监控的 latest + 7d 可用率（admin/user list 用，消除 N+1）。
// 失败时返回空 map，错误仅日志，不影响列表渲染。
//
// 当监控关联了 account_id 且开启 use_logs_for_status 时，优先用真实请求日志判定主模型状态；
// 日志样本不足或未关联账号时回退到探测 latest。
//
// 参数 monitors 直接传入监控对象，便于读取 account_id / channel_id 做日志聚合。
func (s *ChannelMonitorService) BatchMonitorStatusSummary(
	ctx context.Context,
	monitors []*ChannelMonitor,
) map[int64]MonitorStatusSummary {
	out := make(map[int64]MonitorStatusSummary, len(monitors))
	if len(monitors) == 0 {
		return out
	}
	ids, _, _ := collectMonitorIndexes(monitors)
	latestMap, err := s.repo.ListLatestForMonitorIDs(ctx, ids)
	if err != nil {
		slog.Warn("channel_monitor: batch load latest failed", "error", err)
		latestMap = map[int64][]*ChannelMonitorLatest{}
	}
	availMap, err := s.repo.ComputeAvailabilityForMonitors(ctx, ids, monitorAvailability7Days)
	if err != nil {
		slog.Warn("channel_monitor: batch compute availability failed", "error", err)
		availMap = map[int64][]*ChannelMonitorAvailability{}
	}
	// 日志驱动状态：批量查询关联账号的请求日志聚合，覆盖 primary + extra models。
	logStatusByID := s.batchLogStatus(ctx, monitors)

	for _, m := range monitors {
		summary := buildStatusSummary(
			indexLatestByModel(latestMap[m.ID]),
			indexAvailabilityByModel(availMap[m.ID]),
			m.PrimaryModel,
			m.ExtraModels,
		)
		// 用日志状态覆盖 primary + extra（日志可用且样本充足时）。
		applyLogStatusToSummary(&summary, logStatusByID[m.ID], m.PrimaryModel, m.ExtraModels)
		out[m.ID] = summary
	}
	return out
}

// batchLogStatus 批量查询关联了 account_id 且开启 use_logs_for_status 的监控的请求日志聚合。
// 返回 map[monitorID]map[model]*ChannelMonitorLogStatus。失败仅日志，返回空 map（回退探测）。
func (s *ChannelMonitorService) batchLogStatus(
	ctx context.Context,
	monitors []*ChannelMonitor,
) map[int64]map[string]*ChannelMonitorLogStatus {
	out := make(map[int64]map[string]*ChannelMonitorLogStatus, len(monitors))
	if s == nil || s.usageLogReader == nil {
		return out
	}
	targets := make([]ChannelMonitorLogTarget, 0, len(monitors))
	for _, m := range monitors {
		if m == nil || !m.UseLogsForStatus || m.AccountID == nil {
			continue
		}
		targets = append(targets, ChannelMonitorLogTarget{
			MonitorID: m.ID,
			AccountID: *m.AccountID,
			ChannelID: cloneInt64Pointer(m.ChannelID),
			Models:    appendUniqueModels([]string{m.PrimaryModel}, m.ExtraModels),
		})
	}
	if len(targets) == 0 {
		return out
	}
	logStatusByMonitor, err := s.usageLogReader.GetChannelMonitorLogStatusBatch(
		ctx, targets, monitorLogStatusWindow,
	)
	if err != nil {
		slog.Warn("channel_monitor: batch log status failed", "error", err)
		return out
	}
	for monitorID, statusByModel := range logStatusByMonitor {
		if len(statusByModel) == 0 {
			continue
		}
		copyMap := make(map[string]*ChannelMonitorLogStatus, len(statusByModel))
		for model, status := range statusByModel {
			copyMap[model] = status
		}
		out[monitorID] = copyMap
	}
	return out
}

// appendUniqueModels 把 src 追加到 dst，去重（大小写敏感），保持顺序。
func appendUniqueModels(dst, src []string) []string {
	seen := make(map[string]struct{}, len(dst)+len(src))
	for _, m := range dst {
		seen[m] = struct{}{}
	}
	for _, m := range src {
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		dst = append(dst, m)
	}
	return dst
}

// applyLogStatusToSummary 用日志聚合结果覆盖 summary 的 primary + extra 状态。
// 仅当日志样本 >= monitorLogStatusMinSamples 时才覆盖；不足则保留探测值（StatusSource 留空）。
func applyLogStatusToSummary(
	summary *MonitorStatusSummary,
	logStatusByModel map[string]*ChannelMonitorLogStatus,
	primary string,
	extras []string,
) {
	if summary == nil || len(logStatusByModel) == 0 {
		return
	}
	if primary != "" {
		if ls := logStatusByModel[primary]; ls != nil && ls.TotalRequests >= monitorLogStatusMinSamples {
			summary.PrimaryStatus = logStatusToMonitorStatus(ls)
			summary.PrimaryLatencyMs = ls.AvgLatencyMs
			summary.StatusSource = MonitorStatusSourceLogs
		}
	}
	for i := range summary.ExtraModels {
		entry := &summary.ExtraModels[i]
		if ls := logStatusByModel[entry.Model]; ls != nil && ls.TotalRequests >= monitorLogStatusMinSamples {
			entry.Status = logStatusToMonitorStatus(ls)
			entry.LatencyMs = ls.AvgLatencyMs
		}
	}
}

// logStatusToMonitorStatus 把日志聚合的成功率映射为监控状态字符串。
//   - 成功率 >= monitorLogStatusDegradedRate → operational
//   - 成功率 >= monitorLogStatusFailedRate → degraded
//   - 否则 → failed
func logStatusToMonitorStatus(ls *ChannelMonitorLogStatus) string {
	if ls == nil || ls.TotalRequests == 0 {
		return MonitorStatusFailed
	}
	rate := ls.SuccessRate()
	switch {
	case rate >= monitorLogStatusDegradedRate:
		return MonitorStatusOperational
	case rate >= monitorLogStatusFailedRate:
		return MonitorStatusDegraded
	default:
		return MonitorStatusFailed
	}
}

// ListUserView 用户只读视图：列出所有 enabled 监控的概览。
// 使用批量聚合接口避免 N+1：
//
//	1 次查 monitors；
//	1 次批量 latest（含 ping_latency_ms）；
//	1 次批量 7d availability；
//	1 次批量 timeline（主模型最近 N 条）。
func (s *ChannelMonitorService) ListUserView(ctx context.Context) ([]*UserMonitorView, error) {
	monitors, err := s.repo.ListEnabled(ctx)
	if err != nil {
		return nil, fmt.Errorf("list enabled monitors: %w", err)
	}
	if len(monitors) == 0 {
		return []*UserMonitorView{}, nil
	}

	ids, primaryByID, _ := collectMonitorIndexes(monitors)
	summaries := s.BatchMonitorStatusSummary(ctx, monitors)
	latestMap := s.batchLatest(ctx, ids)
	timelineMap := s.batchTimeline(ctx, ids, primaryByID)

	views := make([]*UserMonitorView, 0, len(monitors))
	for _, m := range monitors {
		primaryLatest := pickLatest(latestMap[m.ID], m.PrimaryModel)
		views = append(views, buildUserViewFromSummary(m, summaries[m.ID], primaryLatest, timelineMap[m.ID]))
	}
	return views, nil
}

// collectMonitorIndexes 把 monitors 列表按 ID 展开为聚合查询所需的三个索引结构。
func collectMonitorIndexes(monitors []*ChannelMonitor) ([]int64, map[int64]string, map[int64][]string) {
	ids := make([]int64, 0, len(monitors))
	primaryByID := make(map[int64]string, len(monitors))
	extrasByID := make(map[int64][]string, len(monitors))
	for _, m := range monitors {
		ids = append(ids, m.ID)
		primaryByID[m.ID] = m.PrimaryModel
		extrasByID[m.ID] = m.ExtraModels
	}
	return ids, primaryByID, extrasByID
}

// batchLatest 批量取 latest per model，失败仅日志（与现有 BatchMonitorStatusSummary 一致，不阻断列表渲染）。
func (s *ChannelMonitorService) batchLatest(ctx context.Context, ids []int64) map[int64][]*ChannelMonitorLatest {
	latestMap, err := s.repo.ListLatestForMonitorIDs(ctx, ids)
	if err != nil {
		slog.Warn("channel_monitor: user view batch latest failed", "error", err)
		return map[int64][]*ChannelMonitorLatest{}
	}
	return latestMap
}

// batchTimeline 批量取每个 monitor 主模型最近 monitorTimelineMaxPoints 条历史。
func (s *ChannelMonitorService) batchTimeline(
	ctx context.Context,
	ids []int64,
	primaryByID map[int64]string,
) map[int64][]*ChannelMonitorHistoryEntry {
	timelineMap, err := s.repo.ListRecentHistoryForMonitors(ctx, ids, primaryByID, monitorTimelineMaxPoints)
	if err != nil {
		slog.Warn("channel_monitor: user view batch timeline failed", "error", err)
		return map[int64][]*ChannelMonitorHistoryEntry{}
	}
	return timelineMap
}

// pickLatest 从 latest 切片中挑出指定 model 对应项，未命中返回 nil。
func pickLatest(rows []*ChannelMonitorLatest, model string) *ChannelMonitorLatest {
	if model == "" {
		return nil
	}
	for _, r := range rows {
		if r.Model == model {
			return r
		}
	}
	return nil
}

// GetUserDetail 用户只读视图：单个监控详情（每个模型 7d/15d/30d 可用率与平均延迟）。
// 不暴露 api_key。
func (s *ChannelMonitorService) GetUserDetail(ctx context.Context, id int64) (*UserMonitorDetail, error) {
	m, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !m.Enabled {
		return nil, ErrChannelMonitorNotFound
	}

	latest, err := s.repo.ListLatestPerModel(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list latest per model: %w", err)
	}
	availMap, err := s.collectAvailabilityWindows(ctx, id)
	if err != nil {
		return nil, err
	}

	models := mergeModelDetails(m, latest, availMap)
	// 日志驱动状态：覆盖 latest status（日志样本充足时）。
	s.applyLogStatusToModelDetails(ctx, m, models)
	return &UserMonitorDetail{
		ID:        m.ID,
		Name:      m.Name,
		Provider:  m.Provider,
		GroupName: m.GroupName,
		Models:    models,
	}, nil
}

// applyLogStatusToModelDetails 用单账号的请求日志聚合覆盖 ModelDetail.LatestStatus / LatestLatencyMs。
// 日志样本不足或未关联账号时保留探测值。
func (s *ChannelMonitorService) applyLogStatusToModelDetails(ctx context.Context, m *ChannelMonitor, models []ModelDetail) {
	if s == nil || s.usageLogReader == nil || m == nil || !m.UseLogsForStatus || m.AccountID == nil {
		return
	}
	allModels := make([]string, 0, len(models))
	for _, md := range models {
		allModels = append(allModels, md.Model)
	}
	if len(allModels) == 0 {
		return
	}
	statusByMonitor, err := s.usageLogReader.GetChannelMonitorLogStatusBatch(
		ctx,
		[]ChannelMonitorLogTarget{{
			MonitorID: m.ID,
			AccountID: *m.AccountID,
			ChannelID: cloneInt64Pointer(m.ChannelID),
			Models:    append([]string{}, allModels...),
		}},
		monitorLogStatusWindow,
	)
	if err != nil {
		slog.Warn("channel_monitor: detail log status failed", "error", err)
		return
	}
	statusByModel := statusByMonitor[m.ID]
	if len(statusByModel) == 0 {
		return
	}
	for i := range models {
		md := &models[i]
		if ls := statusByModel[md.Model]; ls != nil && ls.TotalRequests >= monitorLogStatusMinSamples {
			md.LatestStatus = logStatusToMonitorStatus(ls)
			md.LatestLatencyMs = ls.AvgLatencyMs
		}
	}
}

// collectAvailabilityWindows 一次性查询 7/15/30 天三个窗口，按模型组织。
func (s *ChannelMonitorService) collectAvailabilityWindows(ctx context.Context, monitorID int64) (map[int]map[string]*ChannelMonitorAvailability, error) {
	out := make(map[int]map[string]*ChannelMonitorAvailability, 3)
	windows := []int{monitorAvailability7Days, monitorAvailability15Days, monitorAvailability30Days}
	for _, w := range windows {
		rows, err := s.repo.ComputeAvailability(ctx, monitorID, w)
		if err != nil {
			return nil, fmt.Errorf("compute availability %dd: %w", w, err)
		}
		out[w] = indexAvailabilityByModel(rows)
	}
	return out, nil
}

// ---------- 纯函数 helper（无 IO，可在 batch / 单 monitor / detail 路径复用）----------

// indexLatestByModel 把 latest 切片按 model 索引（小工具，避免在 hot path 重复写）。
func indexLatestByModel(rows []*ChannelMonitorLatest) map[string]*ChannelMonitorLatest {
	m := make(map[string]*ChannelMonitorLatest, len(rows))
	for _, r := range rows {
		m[r.Model] = r
	}
	return m
}

// indexAvailabilityByModel 把 availability 切片按 model 索引。
func indexAvailabilityByModel(rows []*ChannelMonitorAvailability) map[string]*ChannelMonitorAvailability {
	m := make(map[string]*ChannelMonitorAvailability, len(rows))
	for _, r := range rows {
		m[r.Model] = r
	}
	return m
}

// buildStatusSummary 由 latest + availability 字典构造 MonitorStatusSummary。
// 不做任何 IO，纯组装，便于在 batch 与单 monitor 路径复用。
func buildStatusSummary(
	latestByModel map[string]*ChannelMonitorLatest,
	availByModel map[string]*ChannelMonitorAvailability,
	primary string,
	extras []string,
) MonitorStatusSummary {
	summary := MonitorStatusSummary{ExtraModels: make([]ExtraModelStatus, 0, len(extras))}
	if primary != "" {
		if l, ok := latestByModel[primary]; ok {
			summary.PrimaryStatus = l.Status
			summary.PrimaryLatencyMs = l.LatencyMs
		}
		if a, ok := availByModel[primary]; ok {
			summary.Availability7d = a.AvailabilityPct
		}
	}
	for _, model := range extras {
		entry := ExtraModelStatus{Model: model}
		if l, ok := latestByModel[model]; ok {
			entry.Status = l.Status
			entry.LatencyMs = l.LatencyMs
		}
		summary.ExtraModels = append(summary.ExtraModels, entry)
	}
	return summary
}

// buildUserViewFromSummary 用预聚合好的 MonitorStatusSummary + 主模型 latest + timeline 装填 UserMonitorView（无 IO）。
// primaryLatest 可能为 nil（该监控尚无历史）；timelineEntries 可能为空。
func buildUserViewFromSummary(
	m *ChannelMonitor,
	summary MonitorStatusSummary,
	primaryLatest *ChannelMonitorLatest,
	timelineEntries []*ChannelMonitorHistoryEntry,
) *UserMonitorView {
	view := &UserMonitorView{
		ID:               m.ID,
		Name:             m.Name,
		Provider:         m.Provider,
		GroupName:        m.GroupName,
		PrimaryModel:     m.PrimaryModel,
		PrimaryStatus:    summary.PrimaryStatus,
		PrimaryLatencyMs: summary.PrimaryLatencyMs,
		Availability7d:   summary.Availability7d,
		ExtraModels:      summary.ExtraModels,
		Timeline:         buildTimelinePoints(timelineEntries),
	}
	if primaryLatest != nil {
		view.PrimaryPingLatencyMs = primaryLatest.PingLatencyMs
	}
	return view
}

// buildTimelinePoints 把 history entry 裁剪为 timeline 点（去除 message/ID/Model，减小响应体）。
func buildTimelinePoints(entries []*ChannelMonitorHistoryEntry) []UserMonitorTimelinePoint {
	out := make([]UserMonitorTimelinePoint, 0, len(entries))
	for _, e := range entries {
		out = append(out, UserMonitorTimelinePoint{
			Status:        e.Status,
			LatencyMs:     e.LatencyMs,
			PingLatencyMs: e.PingLatencyMs,
			CheckedAt:     e.CheckedAt,
		})
	}
	return out
}

// mergeModelDetails 合并 latest + availability 三个窗口为 ModelDetail 列表。
// 复用 indexLatestByModel，避免在多处重复写 build map 逻辑。
func mergeModelDetails(
	m *ChannelMonitor,
	latest []*ChannelMonitorLatest,
	availMap map[int]map[string]*ChannelMonitorAvailability,
) []ModelDetail {
	all := append([]string{m.PrimaryModel}, m.ExtraModels...)
	latestByModel := indexLatestByModel(latest)
	out := make([]ModelDetail, 0, len(all))
	for _, model := range all {
		d := ModelDetail{Model: model}
		if l, ok := latestByModel[model]; ok {
			d.LatestStatus = l.Status
			d.LatestLatencyMs = l.LatencyMs
		}
		if a, ok := availMap[monitorAvailability7Days][model]; ok {
			d.Availability7d = a.AvailabilityPct
			d.AvgLatency7dMs = a.AvgLatencyMs
		}
		if a, ok := availMap[monitorAvailability15Days][model]; ok {
			d.Availability15d = a.AvailabilityPct
		}
		if a, ok := availMap[monitorAvailability30Days][model]; ok {
			d.Availability30d = a.AvailabilityPct
		}
		out = append(out, d)
	}
	return out
}
