//go:build unit

package admin

import (
	"bytes"
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

type usageDisplayHandlerRepo struct {
	values map[string]string
}

func (r *usageDisplayHandlerRepo) Get(context.Context, string) (*service.Setting, error) {
	panic("unexpected Get")
}
func (r *usageDisplayHandlerRepo) GetValue(_ context.Context, key string) (string, error) {
	if value, ok := r.values[key]; ok {
		return value, nil
	}
	return "", service.ErrSettingNotFound
}
func (r *usageDisplayHandlerRepo) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	return nil
}
func (r *usageDisplayHandlerRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	panic("unexpected GetMultiple")
}
func (r *usageDisplayHandlerRepo) SetMultiple(context.Context, map[string]string) error {
	panic("unexpected SetMultiple")
}
func (r *usageDisplayHandlerRepo) GetAll(context.Context) (map[string]string, error) {
	panic("unexpected GetAll")
}
func (r *usageDisplayHandlerRepo) Delete(context.Context, string) error { panic("unexpected Delete") }

func TestUsageDisplayHandlersGetAndPut(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &usageDisplayHandlerRepo{values: map[string]string{}}
	h := NewSettingHandler(service.NewSettingService(repo, &config.Config{}), nil, nil, nil, nil, nil, nil)

	get := httptest.NewRecorder()
	gc, _ := gin.CreateTestContext(get)
	gc.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings/usage-display", nil)
	h.GetUsageDisplayConfig(gc)
	require.Equal(t, http.StatusOK, get.Code)
	var initial struct {
		Data service.UsageDisplayConfig `json:"data"`
	}
	require.NoError(t, json.Unmarshal(get.Body.Bytes(), &initial))
	require.True(t, initial.Data.Fields.Speed)

	body := bytes.NewBufferString(`{"fields":{"speed":false},"color_enabled":false}`)
	put := httptest.NewRecorder()
	pc, _ := gin.CreateTestContext(put)
	pc.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/usage-display", body)
	pc.Request.Header.Set("Content-Type", "application/json")
	h.UpdateUsageDisplayConfig(pc)
	require.Equal(t, http.StatusOK, put.Code)
	var updated struct {
		Data service.UsageDisplayConfig `json:"data"`
	}
	require.NoError(t, json.Unmarshal(put.Body.Bytes(), &updated))
	require.False(t, updated.Data.Fields.Speed)
	require.False(t, updated.Data.ColorEnabled)
}

func TestUsageDisplayHandlerValidationUsesApplicationError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &usageDisplayHandlerRepo{values: map[string]string{}}
	h := NewSettingHandler(service.NewSettingService(repo, &config.Config{}), nil, nil, nil, nil, nil, nil)
	body := bytes.NewBufferString(`{"token_unit":"bad"}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/usage-display", body)
	c.Request.Header.Set("Content-Type", "application/json")
	h.UpdateUsageDisplayConfig(c)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "INVALID_USAGE_DISPLAY_CONFIG")
}

var _ service.SettingRepository = (*usageDisplayHandlerRepo)(nil)
