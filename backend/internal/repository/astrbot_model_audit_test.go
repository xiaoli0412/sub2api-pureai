package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBuildAstrBotModelAuditWhere_UsesTriStateAndAllowlistFilters(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	mismatch := false
	where, args := buildAstrBotModelAuditWhere(service.AstrBotModelAuditFilter{
		StartTime: start, EndTime: end, RequestedModel: "gpt-5", UpstreamModel: "gpt-5-mini",
		AccountID: 7, ChannelID: 3, GroupID: 8, UserID: 9, Mismatch: &mismatch,
		ResourceAllowlist: service.AstrBotResourceAllowlist{AccountIDs: []int64{7}, ChannelIDs: []int64{3}},
	})

	require.Contains(t, where, "ul.upstream_model_mismatch IS FALSE")
	require.Contains(t, where, "ul.account_id = ANY")
	require.Contains(t, where, "ul.channel_id = ANY")
	require.Contains(t, where, "ul.channel_id = 0")
	require.Len(t, args, 10)
}

func TestUsageLogRepositoryGetAstrBotModelAudit_ReturnsMismatchContract(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := newUsageLogRepositoryWithSQL(nil, db)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	filter := service.AstrBotModelAuditFilter{StartTime: start, EndTime: end, Page: 1, PageSize: 20}

	mismatch := true
	mock.ExpectQuery(regexp.QuoteMeta("SELECT")).WithArgs(start, end).WillReturnRows(sqlmock.NewRows([]string{
		"requested_model", "upstream_model", "upstream_response_model", "upstream_model_mismatch",
		"requests", "observed_requests", "mismatch_count", "match_count", "no_response_model_count",
	}).AddRow("gpt-5", "gpt-5-mini", "gpt-5-mini", mismatch, int64(2), int64(2), int64(1), int64(1), int64(0)).
		AddRow("legacy", nil, nil, nil, int64(1), int64(0), int64(0), int64(0), int64(1)))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*)")).WithArgs(start, end).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(3)))
	created := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT ul.id")).WithArgs(start, end, 20, 0).WillReturnRows(sqlmock.NewRows([]string{
		"id", "created_at", "request_id", "requested_model", "upstream_model", "upstream_response_model", "upstream_model_mismatch",
		"account_id", "channel_id", "group_id", "user_id", "provider", "upstream_endpoint",
	}).AddRow(int64(4), created, "req-4", "gpt-5", "gpt-5-mini", "gpt-5-mini", mismatch, int64(7), int64(3), int64(8), int64(9), "openai", "https://upstream.example").
		AddRow(int64(5), created, "req-5", "legacy", nil, nil, nil, int64(7), nil, nil, int64(9), "", ""))

	result, err := repo.GetAstrBotModelAudit(context.Background(), filter)

	require.NoError(t, err)
	require.Equal(t, int64(3), result.TotalRequests)
	require.Equal(t, int64(2), result.ObservedRequests)
	require.Equal(t, int64(1), result.MismatchCount)
	require.Equal(t, int64(1), result.MatchCount)
	require.Equal(t, int64(1), result.NoResponseModelCount)
	require.InDelta(t, 0.5, result.MismatchRate, 0.0001)
	require.Len(t, result.Samples, 2)
	require.Equal(t, &mismatch, result.Samples[0].Mismatch)
	require.Nil(t, result.Samples[1].Mismatch)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetAstrBotModelAudit_QueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := newUsageLogRepositoryWithSQL(nil, db)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT")).WillReturnError(context.Canceled)

	_, err = repo.GetAstrBotModelAudit(context.Background(), auditTestFilter())

	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, mock.ExpectationsWereMet())
}

func auditTestFilter() service.AstrBotModelAuditFilter {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return service.AstrBotModelAuditFilter{StartTime: start, EndTime: start.Add(24 * time.Hour)}
}
