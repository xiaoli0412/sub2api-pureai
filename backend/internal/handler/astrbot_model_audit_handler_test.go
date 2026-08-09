package handler

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type astrBotAuditHandlerRepoStub struct {
	service.UsageLogRepository
	result *service.AstrBotModelAuditResult
	err    error
	filter service.AstrBotModelAuditFilter
}

func (r *astrBotAuditHandlerRepoStub) GetAstrBotModelAudit(_ context.Context, filter service.AstrBotModelAuditFilter) (*service.AstrBotModelAuditResult, error) {
	r.filter = filter
	return r.result, r.err
}

func newAstrBotAuditContext(target string) *gin.Context {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("astrbot-test-recorder", recorder)
	c.Request = httptest.NewRequest("GET", target, nil)
	return c
}

func astrBotAuditRecorder(c *gin.Context) *httptest.ResponseRecorder {
	value, _ := c.Get("astrbot-test-recorder")
	return value.(*httptest.ResponseRecorder)
}

func TestParseAstrBotModelAuditFilter_DefaultsAndFilters(t *testing.T) {
	c := newAstrBotAuditContext("/api/v1/bot/model-audit?requested_model=gpt-5&upstream_model=gpt-5-mini&account_id=7&channel_id=3&mismatch=true&page=2&page_size=30")
	filter, err := parseAstrBotModelAuditFilter(c)

	require.NoError(t, err)
	require.Equal(t, "gpt-5", filter.RequestedModel)
	require.Equal(t, "gpt-5-mini", filter.UpstreamModel)
	require.Equal(t, int64(7), filter.AccountID)
	require.Equal(t, int64(3), filter.ChannelID)
	require.NotNil(t, filter.Mismatch)
	require.True(t, *filter.Mismatch)
	require.Equal(t, 2, filter.Page)
	require.Equal(t, 30, filter.PageSize)
	require.WithinDuration(t, filter.EndTime.Add(-24*time.Hour), filter.StartTime, time.Second)
}

func TestParseAstrBotModelAuditFilter_RejectsInvalidInputs(t *testing.T) {
	for _, target := range []string{
		"/api/v1/bot/model-audit?page=0",
		"/api/v1/bot/model-audit?page_size=101",
		"/api/v1/bot/model-audit?mismatch=unknown",
		"/api/v1/bot/model-audit?account_id=nope",
		"/api/v1/bot/model-audit?start=not-time",
		"/api/v1/bot/model-audit?start=2026-01-02T00:00:00Z&end=2026-01-01T00:00:00Z",
	} {
		t.Run(target, func(t *testing.T) {
			_, err := parseAstrBotModelAuditFilter(newAstrBotAuditContext(target))
			require.Error(t, err)
			require.True(t, infraerrors.IsBadRequest(err))
		})
	}
}

func TestModelAuditAllowlistIsPassedToReader(t *testing.T) {
	repo := &astrBotAuditHandlerRepoStub{result: &service.AstrBotModelAuditResult{}}
	h := &AstrBotHandler{usage: service.NewUsageService(repo, nil, nil, nil)}
	c := newAstrBotAuditContext("/api/v1/bot/model-audit?account_id=7")
	c.Set(middleware.AstrBotTokenKey, &service.AstrBotToken{ResourceAllowlist: service.AstrBotResourceAllowlist{AccountIDs: []int64{7}}})

	h.ModelAudit(c)

	require.Equal(t, 200, c.Writer.Status())
	require.Equal(t, []int64{7}, repo.filter.ResourceAllowlist.AccountIDs)
}

func TestModelAuditRejectsUnauthorizedSpecificResource(t *testing.T) {
	h := &AstrBotHandler{}
	c := newAstrBotAuditContext("/api/v1/bot/model-audit?account_id=8")
	c.Set(middleware.AstrBotTokenKey, &service.AstrBotToken{ResourceAllowlist: service.AstrBotResourceAllowlist{AccountIDs: []int64{7}}})

	h.ModelAudit(c)

	require.Equal(t, 404, c.Writer.Status())
}

func TestToAstrBotModelAuditDTOsExposeOnlyAuditFields(t *testing.T) {
	mismatch := true
	aggregate := toAstrBotModelAuditAggregateDTO(service.AstrBotModelAuditAggregate{RequestedModel: "gpt-5", MismatchCount: 1, ObservedRequests: 2, MismatchRate: 0.5})
	sample := toAstrBotModelAuditSampleDTO(service.AstrBotModelAuditSample{ID: 9, RequestedModel: "gpt-5", Mismatch: &mismatch, AccountID: 7})

	require.Equal(t, "gpt-5", aggregate.RequestedModel)
	require.Equal(t, int64(1), aggregate.MismatchCount)
	require.Equal(t, int64(9), sample.ID)
	require.Equal(t, &mismatch, sample.Mismatch)
}

func TestModelAuditHandlerSuccess(t *testing.T) {
	repo := &astrBotAuditHandlerRepoStub{result: &service.AstrBotModelAuditResult{TotalRequests: 1, ObservedRequests: 1, MismatchCount: 1, MismatchRate: 1, Page: 1, PageSize: 20}}
	h := &AstrBotHandler{usage: service.NewUsageService(repo, nil, nil, nil)}
	c := newAstrBotAuditContext("/api/v1/bot/model-audit")
	c.Set(middleware.AstrBotTokenKey, &service.AstrBotToken{})

	h.ModelAudit(c)

	require.Equal(t, 200, c.Writer.Status())
	require.Contains(t, astrBotAuditRecorder(c).Body.String(), "mismatch_rate")
}

func TestModelAuditHandlerStorageError(t *testing.T) {
	repo := &astrBotAuditHandlerRepoStub{err: errors.New("storage down")}
	h := &AstrBotHandler{usage: service.NewUsageService(repo, nil, nil, nil)}
	c := newAstrBotAuditContext("/api/v1/bot/model-audit")
	c.Set(middleware.AstrBotTokenKey, &service.AstrBotToken{})

	h.ModelAudit(c)

	require.Equal(t, 500, c.Writer.Status())
}

func TestModelAuditHandlerValidationError(t *testing.T) {
	h := &AstrBotHandler{}
	c := newAstrBotAuditContext("/api/v1/bot/model-audit?page_size=101")
	c.Set(middleware.AstrBotTokenKey, &service.AstrBotToken{})

	h.ModelAudit(c)

	require.Equal(t, 400, c.Writer.Status())
}
