//go:build unit

package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestGetChannelMonitorLogStatusBatchUsesConfiguredExecutorAndKeepsChannelTargetsIsolated(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	now := time.Now().UTC()

	mock.ExpectQuery(regexp.QuoteMeta("t.channel_id = 0 OR ul.channel_id = t.channel_id")).
		WithArgs(
			pq.Array([]int64{101, 102}),
			pq.Array([]int64{7, 7}),
			pq.Array([]int64{11, 22}),
			pq.Array([]string{"model-a", "model-a"}),
			sqlmock.AnyArg(),
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"monitor_id", "account_id", "model", "total_requests", "success_requests", "avg_latency_ms", "last_request_at",
		}).
			AddRow(int64(101), int64(7), "model-a", int64(5), int64(5), 10.0, now).
			AddRow(int64(102), int64(7), "model-a", int64(5), int64(0), 20.0, now))

	channelA := int64(11)
	channelB := int64(22)
	got, err := repo.GetChannelMonitorLogStatusBatch(context.Background(), []service.ChannelMonitorLogTarget{
		{MonitorID: 101, AccountID: 7, ChannelID: &channelA, Models: []string{"model-a"}},
		{MonitorID: 102, AccountID: 7, ChannelID: &channelB, Models: []string{"model-a"}},
	}, 10*time.Minute)

	require.NoError(t, err)
	require.Equal(t, 5, got[101]["model-a"].TotalRequests)
	require.Equal(t, 5, got[101]["model-a"].SuccessRequests)
	require.Equal(t, 5, got[102]["model-a"].TotalRequests)
	require.Equal(t, 0, got[102]["model-a"].SuccessRequests)
	require.NoError(t, mock.ExpectationsWereMet())
}
