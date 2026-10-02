//go:build unit

package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

func TestUsageLogRepositoryGetStatsWithFiltersUsesWeightedSpeedAggregates(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	mock.ExpectQuery("(?s)FROM usage_logs.*GROUP BY GROUPING SETS").WillReturnRows(
		newSpeedStatsRows().
			AddRow(1, 1, nil, nil, int64(3), int64(30), int64(300), int64(0), int64(0), 1.0, 1.0, 1.0, 100.0,
				int64(300), int64(3000), int64(2), int64(150), int64(1500), int64(1)),
	)

	stats, err := repo.GetStatsWithFilters(context.Background(), usagestats.UsageLogFilters{})
	require.NoError(t, err)
	require.Equal(t, int64(2), stats.SpeedSampleCount)
	require.NotNil(t, stats.OutputTokensPerSecond)
	require.InDelta(t, 100.0, *stats.OutputTokensPerSecond, 1e-9)
	require.NotNil(t, stats.GenerationTokensPerSecond)
	require.InDelta(t, 100.0, *stats.GenerationTokensPerSecond, 1e-9)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetStatsWithFiltersLeavesSpeedNullableWithoutPairs(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	mock.ExpectQuery("(?s)FROM usage_logs.*GROUP BY GROUPING SETS").WillReturnRows(
		newSpeedStatsRows().
			AddRow(1, 1, nil, nil, int64(1), int64(0), int64(0), int64(0), int64(0), 0.0, 0.0, 0.0, 0.0,
				nil, nil, int64(0), nil, nil, int64(0)),
	)

	stats, err := repo.GetStatsWithFilters(context.Background(), usagestats.UsageLogFilters{})
	require.NoError(t, err)
	require.Nil(t, stats.OutputTokensPerSecond)
	require.Nil(t, stats.GenerationTokensPerSecond)
	require.Zero(t, stats.SpeedSampleCount)
	require.NoError(t, mock.ExpectationsWereMet())
}

func newSpeedStatsRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"inbound_grouped", "upstream_grouped", "inbound_endpoint", "upstream_endpoint",
		"requests", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens",
		"cost", "actual_cost", "account_cost", "avg_duration_ms",
		"speed_output_tokens", "speed_duration_ms", "speed_sample_count",
		"generation_output_tokens", "generation_duration_ms", "generation_speed_sample_count",
	})
}
