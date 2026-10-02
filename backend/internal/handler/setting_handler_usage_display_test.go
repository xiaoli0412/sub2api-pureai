//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type usageDisplayPublicRepo struct{}

func (usageDisplayPublicRepo) Get(context.Context, string) (*service.Setting, error) {
	panic("unexpected Get")
}
func (usageDisplayPublicRepo) GetValue(context.Context, string) (string, error) {
	return "", service.ErrSettingNotFound
}
func (usageDisplayPublicRepo) Set(context.Context, string, string) error { panic("unexpected Set") }
func (usageDisplayPublicRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	panic("unexpected GetMultiple")
}
func (usageDisplayPublicRepo) SetMultiple(context.Context, map[string]string) error {
	panic("unexpected SetMultiple")
}
func (usageDisplayPublicRepo) GetAll(context.Context) (map[string]string, error) {
	panic("unexpected GetAll")
}
func (usageDisplayPublicRepo) Delete(context.Context, string) error { panic("unexpected Delete") }

func TestSettingHandlerGetUsageDisplayConfigIsIndependentPublicEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewSettingHandler(service.NewSettingService(usageDisplayPublicRepo{}, &config.Config{}), "test")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/settings/usage-display", nil)
	h.GetUsageDisplayConfig(c)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Code int                        `json:"code"`
		Data service.UsageDisplayConfig `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, "raw", resp.Data.TokenUnit)
	require.True(t, resp.Data.Fields.OutputTokens)
}
