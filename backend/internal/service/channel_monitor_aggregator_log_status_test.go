//go:build unit

package service

import (
	"context"
	"testing"
	"time"
)

type channelMonitorLogReaderStub struct {
	calls   int
	targets []ChannelMonitorLogTarget
	result  map[int64]map[string]*ChannelMonitorLogStatus
	err     error
}

func (r *channelMonitorLogReaderStub) GetChannelMonitorLogStatusBatch(_ context.Context, targets []ChannelMonitorLogTarget, _ time.Duration) (map[int64]map[string]*ChannelMonitorLogStatus, error) {
	r.calls++
	r.targets = append([]ChannelMonitorLogTarget(nil), targets...)
	return r.result, r.err
}

func TestLogStatusToMonitorStatusMapsSuccessRates(t *testing.T) {
	tests := []struct {
		name       string
		success    int
		total      int
		wantStatus string
	}{
		{name: "operational", success: 5, total: 5, wantStatus: MonitorStatusOperational},
		{name: "degraded", success: 4, total: 5, wantStatus: MonitorStatusDegraded},
		{name: "failed", success: 3, total: 5, wantStatus: MonitorStatusFailed},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := logStatusToMonitorStatus(&ChannelMonitorLogStatus{
				TotalRequests:   tc.total,
				SuccessRequests: tc.success,
			})
			if got != tc.wantStatus {
				t.Fatalf("logStatusToMonitorStatus() = %q, want %q", got, tc.wantStatus)
			}
		})
	}
}

func TestApplyLogStatusToSummaryFallsBackWhenSamplesAreInsufficient(t *testing.T) {
	probeLatency := 12
	logLatency := 99
	summary := MonitorStatusSummary{
		PrimaryStatus:    MonitorStatusOperational,
		PrimaryLatencyMs: &probeLatency,
		ExtraModels: []ExtraModelStatus{{
			Model:     "extra-model",
			Status:    MonitorStatusDegraded,
			LatencyMs: &probeLatency,
		}},
	}

	applyLogStatusToSummary(&summary, map[string]*ChannelMonitorLogStatus{
		"primary-model": {
			TotalRequests:   monitorLogStatusMinSamples - 1,
			SuccessRequests: 0,
			AvgLatencyMs:    &logLatency,
		},
		"extra-model": {
			TotalRequests:   monitorLogStatusMinSamples - 1,
			SuccessRequests: 0,
			AvgLatencyMs:    &logLatency,
		},
	}, "primary-model", []string{"extra-model"})

	if summary.PrimaryStatus != MonitorStatusOperational || summary.PrimaryLatencyMs != &probeLatency {
		t.Fatalf("insufficient primary samples should preserve probe status, got %#v", summary)
	}
	if summary.ExtraModels[0].Status != MonitorStatusDegraded || summary.ExtraModels[0].LatencyMs != &probeLatency {
		t.Fatalf("insufficient extra samples should preserve probe status, got %#v", summary.ExtraModels[0])
	}
	if summary.StatusSource != "" {
		t.Fatalf("insufficient samples should not mark log source, got %q", summary.StatusSource)
	}
}

func TestBatchLogStatusSkipsDisabledLogsAndPreservesChannelIsolation(t *testing.T) {
	reader := &channelMonitorLogReaderStub{
		result: map[int64]map[string]*ChannelMonitorLogStatus{
			101: {"model-a": {TotalRequests: 5, SuccessRequests: 5}},
		},
	}
	svc := &ChannelMonitorService{usageLogReader: reader}
	accountID := int64(7)
	channelA := int64(11)
	channelB := int64(22)
	monitors := []*ChannelMonitor{
		{ID: 101, AccountID: &accountID, ChannelID: &channelA, PrimaryModel: "model-a", UseLogsForStatus: true},
		{ID: 102, AccountID: &accountID, ChannelID: &channelB, PrimaryModel: "model-b", UseLogsForStatus: true},
		{ID: 103, AccountID: &accountID, ChannelID: &channelA, PrimaryModel: "model-c", UseLogsForStatus: false},
	}

	got := svc.batchLogStatus(context.Background(), monitors)
	if reader.calls != 1 {
		t.Fatalf("expected one log status query, got %d", reader.calls)
	}
	if len(reader.targets) != 2 {
		t.Fatalf("expected disabled monitor to be excluded, got %d targets", len(reader.targets))
	}
	if reader.targets[0].MonitorID != 101 || reader.targets[1].MonitorID != 102 {
		t.Fatalf("unexpected target monitor IDs: %#v", reader.targets)
	}
	if reader.targets[0].ChannelID == nil || *reader.targets[0].ChannelID != channelA {
		t.Fatalf("monitor 101 channel target = %#v, want %d", reader.targets[0].ChannelID, channelA)
	}
	if reader.targets[1].ChannelID == nil || *reader.targets[1].ChannelID != channelB {
		t.Fatalf("monitor 102 channel target = %#v, want %d", reader.targets[1].ChannelID, channelB)
	}
	if _, ok := got[102]; ok {
		t.Fatalf("status for monitor 101 must not bleed into monitor 102: %#v", got)
	}
}

func TestApplyLogStatusToSummaryUsesLogsForPrimaryAndExtra(t *testing.T) {
	latency := 37
	summary := MonitorStatusSummary{
		PrimaryStatus: MonitorStatusFailed,
		ExtraModels:   []ExtraModelStatus{{Model: "extra-model", Status: MonitorStatusFailed}},
	}
	applyLogStatusToSummary(&summary, map[string]*ChannelMonitorLogStatus{
		"primary-model": {TotalRequests: 5, SuccessRequests: 4, AvgLatencyMs: &latency},
		"extra-model":   {TotalRequests: 5, SuccessRequests: 5, AvgLatencyMs: &latency},
	}, "primary-model", []string{"extra-model"})

	if summary.PrimaryStatus != MonitorStatusDegraded || summary.PrimaryLatencyMs != &latency {
		t.Fatalf("primary log status = %#v, want degraded with latency", summary)
	}
	if summary.ExtraModels[0].Status != MonitorStatusOperational || summary.ExtraModels[0].LatencyMs != &latency {
		t.Fatalf("extra log status = %#v, want operational with latency", summary.ExtraModels[0])
	}
	if summary.StatusSource != MonitorStatusSourceLogs {
		t.Fatalf("status source = %q, want %q", summary.StatusSource, MonitorStatusSourceLogs)
	}
}
