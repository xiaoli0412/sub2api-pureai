package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type astrBotModelAuditRepoStub struct {
	UsageLogRepository
	result *AstrBotModelAuditResult
	err    error
	got    AstrBotModelAuditFilter
}

func (r *astrBotModelAuditRepoStub) GetAstrBotModelAudit(_ context.Context, filter AstrBotModelAuditFilter) (*AstrBotModelAuditResult, error) {
	r.got = filter
	return r.result, r.err
}

func TestUsageServiceGetAstrBotModelAudit_UsesReader(t *testing.T) {
	repo := &astrBotModelAuditRepoStub{result: &AstrBotModelAuditResult{Total: 2}}
	svc := NewUsageService(repo, nil, nil, nil)
	filter := AstrBotModelAuditFilter{Page: 2, PageSize: 10}

	result, err := svc.GetAstrBotModelAudit(context.Background(), filter)

	require.NoError(t, err)
	require.Equal(t, int64(2), result.Total)
	require.Equal(t, filter, repo.got)
}

func TestUsageServiceGetAstrBotModelAudit_ReturnsUnavailableWithoutReader(t *testing.T) {
	repo := &astrBotModelAuditRepoStub{}
	svc := NewUsageService(repo.UsageLogRepository, nil, nil, nil)

	_, err := svc.GetAstrBotModelAudit(context.Background(), AstrBotModelAuditFilter{})

	require.Error(t, err)
	require.ErrorIs(t, err, ErrUsageModelAuditUnavailable)
}

func TestUsageServiceGetAstrBotModelAudit_ReturnsReaderError(t *testing.T) {
	repo := &astrBotModelAuditRepoStub{err: context.Canceled}
	svc := NewUsageService(repo, nil, nil, nil)

	_, err := svc.GetAstrBotModelAudit(context.Background(), AstrBotModelAuditFilter{})

	require.ErrorIs(t, err, context.Canceled)
}
func TestUsageServiceGetAstrBotModelAudit_NormalizesNilResult(t *testing.T) {
	repo := &astrBotModelAuditRepoStub{}
	svc := NewUsageService(repo, nil, nil, nil)
	filter := AstrBotModelAuditFilter{Page: 3, PageSize: 7}

	result, err := svc.GetAstrBotModelAudit(context.Background(), filter)

	require.NoError(t, err)
	require.Empty(t, result.Aggregates)
	require.Empty(t, result.Samples)
	require.Equal(t, 3, result.Page)
	require.Equal(t, 7, result.PageSize)
}
