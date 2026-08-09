package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type astrBotSystemLogRepoStub struct {
	service.OpsRepository
	filter *service.OpsSystemLogFilter
	result *service.OpsSystemLogList
}

func (r *astrBotSystemLogRepoStub) ListSystemLogs(_ context.Context, filter *service.OpsSystemLogFilter) (*service.OpsSystemLogList, error) {
	r.filter = filter
	if r.result != nil {
		return r.result, nil
	}
	return &service.OpsSystemLogList{Page: filter.Page, PageSize: filter.PageSize}, nil
}

func newAstrBotSystemLogContext(target string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", target, nil)
	return c, recorder
}

func TestParseAstrBotSystemLogFilterBoundsAndFilters(t *testing.T) {
	c, _ := newAstrBotSystemLogContext("/api/v1/bot/logs?start=2026-08-07T00:00:00Z&end=2026-08-08T00:00:00Z&page=2&page_size=100&level=WARN&component=ops&account_id=7&q=timeout")
	filter, err := parseAstrBotSystemLogFilter(c)

	require.NoError(t, err)
	require.Equal(t, 2, filter.Page)
	require.Equal(t, 100, filter.PageSize)
	require.Equal(t, "warn", filter.Level)
	require.Equal(t, "ops", filter.Component)
	require.Equal(t, int64(7), *filter.AccountID)
	require.Equal(t, "timeout", filter.Query)

	for _, target := range []string{
		"/api/v1/bot/logs?page=0",
		"/api/v1/bot/logs?page_size=101",
		"/api/v1/bot/logs?account_id=0",
		"/api/v1/bot/logs?component=" + strings.Repeat("x", astrBotSystemLogMaxFilterBytes+1),
	} {
		c, _ = newAstrBotSystemLogContext(target)
		_, err = parseAstrBotSystemLogFilter(c)
		require.Error(t, err, target)
	}
}

func TestAstrBotSystemLogResourceAllowlistAndSQLScope(t *testing.T) {
	repo := &astrBotSystemLogRepoStub{}
	h := &AstrBotHandler{ops: service.NewOpsService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)}
	token := &service.AstrBotToken{ResourceAllowlist: service.AstrBotResourceAllowlist{
		LogSources: []string{"ops"}, AccountIDs: []int64{7}, UserIDs: []int64{9},
	}}
	c, recorder := newAstrBotSystemLogContext("/api/v1/bot/logs?component=ops")
	c.Set(middleware.AstrBotTokenKey, token)
	h.Logs(c)

	require.Equal(t, 200, recorder.Code)
	require.NotNil(t, repo.filter)
	require.Equal(t, []string{"ops"}, repo.filter.Components)
	require.Equal(t, []int64{7}, repo.filter.AccountIDs)
	require.Equal(t, []int64{9}, repo.filter.UserIDs)

	c, recorder = newAstrBotSystemLogContext("/api/v1/bot/logs?component=admin")
	c.Set(middleware.AstrBotTokenKey, token)
	h.Logs(c)
	require.Equal(t, 404, recorder.Code)

	c, recorder = newAstrBotSystemLogContext("/api/v1/bot/logs")
	c.Set(middleware.AstrBotTokenKey, &service.AstrBotToken{ResourceAllowlist: service.AstrBotResourceAllowlist{GroupIDs: []int64{1}}})
	h.Logs(c)
	require.Equal(t, 404, recorder.Code)
}

func TestToAstrBotSystemLogDTORedactsAndOmitsSecrets(t *testing.T) {
	userID := int64(9)
	accountID := int64(7)
	value := &service.OpsSystemLog{
		ID: 1, CreatedAt: time.Unix(1, 0).UTC(), Host: "node-1", Level: "warn", Component: "ops",
		Message: `authorization=Bearer secret-value password=letmein`, RequestID: "req-1", UserID: &userID, AccountID: &accountID,
		APIKeyID: func() *int64 { id := int64(99); return &id }(),
		Extra: map[string]any{
			"authorization": "Bearer secret-value",
			"nested":        map[string]any{"refresh_token": "refresh-secret", "safe": "ok"},
		},
	}

	out := toAstrBotSystemLogDTO(value)
	require.NotContains(t, out.Message, "secret-value")
	require.NotContains(t, out.Message, "letmein")
	require.NotContains(t, out.Message, "api_key_id")
	require.Equal(t, "***", out.Extra["authorization"])
	nested, ok := out.Extra["nested"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "***", nested["refresh_token"])
	require.Equal(t, "ok", nested["safe"])

	encoded, err := json.Marshal(out)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "api_key_id")
	require.NotContains(t, string(encoded), "secret-value")
}

func TestAstrBotSystemLogRequiresOpsServiceAndToken(t *testing.T) {
	c, recorder := newAstrBotSystemLogContext("/api/v1/bot/logs")
	(&AstrBotHandler{}).Logs(c)
	require.Equal(t, 503, recorder.Code)

	repo := &astrBotSystemLogRepoStub{}
	h := &AstrBotHandler{ops: service.NewOpsService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)}
	c, recorder = newAstrBotSystemLogContext("/api/v1/bot/logs")
	h.Logs(c)
	require.Equal(t, 404, recorder.Code)
}
