package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type usageDisplayRoutesSettingRepo struct {
	values map[string]string
}

func (r *usageDisplayRoutesSettingRepo) Get(context.Context, string) (*service.Setting, error) {
	return nil, service.ErrSettingNotFound
}

func (r *usageDisplayRoutesSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	value, ok := r.values[key]
	if !ok {
		return "", service.ErrSettingNotFound
	}
	return value, nil
}

func (r *usageDisplayRoutesSettingRepo) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	return nil
}

func (r *usageDisplayRoutesSettingRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{}, nil
}

func (r *usageDisplayRoutesSettingRepo) SetMultiple(context.Context, map[string]string) error {
	return nil
}

func (r *usageDisplayRoutesSettingRepo) GetAll(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}

func (r *usageDisplayRoutesSettingRepo) Delete(_ context.Context, key string) error {
	delete(r.values, key)
	return nil
}

func newUsageDisplayRoutesTestRouter(repo *usageDisplayRoutesSettingRepo) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	settingService := service.NewSettingService(repo, &config.Config{})

	publicHandler := handler.NewSettingHandler(settingService, "test-version")
	adminSettingHandler := adminhandler.NewSettingHandler(settingService, nil, nil, nil, nil, nil, nil)

	noopJWT := servermiddleware.JWTAuthMiddleware(func(c *gin.Context) { c.Next() })
	noopAudit := servermiddleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() })
	noopStepUp := servermiddleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() })
	adminAuth := servermiddleware.AdminAuthMiddleware(func(c *gin.Context) {
		switch c.GetHeader("Authorization") {
		case "":
			servermiddleware.AbortWithError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authorization required")
		case "Bearer admin-token":
			c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 1})
			c.Set(string(servermiddleware.ContextKeyUserRole), service.RoleAdmin)
			c.Next()
		default:
			servermiddleware.AbortWithError(c, http.StatusForbidden, "FORBIDDEN", "Admin access required")
		}
	})

	handlers := &handler.Handlers{
		Auth:    &handler.AuthHandler{},
		Setting: publicHandler,
		Admin: &handler.AdminHandlers{
			Setting: adminSettingHandler,
		},
	}
	v1 := router.Group("/api/v1")
	RegisterAuthRoutes(v1, handlers, noopJWT, noopAudit, nil, nil, nil)
	RegisterAdminRoutes(v1, handlers, adminAuth, noopAudit, noopStepUp, nil, nil)
	return router
}

func decodeUsageDisplayResponse(t *testing.T, recorder *httptest.ResponseRecorder) (int, string, service.UsageDisplayConfig) {
	t.Helper()
	var response struct {
		Code    int                        `json:"code"`
		Message string                     `json:"message"`
		Data    service.UsageDisplayConfig `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	return response.Code, response.Message, response.Data
}

func TestUsageDisplayRoutesAreRegisteredWithExpectedMethods(t *testing.T) {
	router := newUsageDisplayRoutesTestRouter(&usageDisplayRoutesSettingRepo{values: map[string]string{}})
	registered := make(map[string]bool)
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = true
	}

	for _, route := range []string{
		"GET /api/v1/settings/usage-display",
		"GET /api/v1/admin/settings/usage-display",
		"PUT /api/v1/admin/settings/usage-display",
	} {
		require.True(t, registered[route], "%s should be registered", route)
	}
}

func TestUsageDisplayPublicRouteIsAnonymousAndUsesStandardEnvelope(t *testing.T) {
	router := newUsageDisplayRoutesTestRouter(&usageDisplayRoutesSettingRepo{values: map[string]string{}})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/settings/usage-display", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	code, message, data := decodeUsageDisplayResponse(t, recorder)
	require.Equal(t, 0, code)
	require.Equal(t, "success", message)
	require.Equal(t, service.DefaultUsageDisplayConfig(), data)
}

func TestUsageDisplayAdminRoutesRequireAdminAndReturnStandardEnvelope(t *testing.T) {
	router := newUsageDisplayRoutesTestRouter(&usageDisplayRoutesSettingRepo{values: map[string]string{}})

	for _, tc := range []struct {
		name       string
		method     string
		path       string
		authorize  string
		wantStatus int
	}{
		{name: "anonymous get", method: http.MethodGet, path: "/api/v1/admin/settings/usage-display", wantStatus: http.StatusUnauthorized},
		{name: "non-admin get", method: http.MethodGet, path: "/api/v1/admin/settings/usage-display", authorize: "Bearer user-token", wantStatus: http.StatusForbidden},
		{name: "anonymous put", method: http.MethodPut, path: "/api/v1/admin/settings/usage-display", wantStatus: http.StatusUnauthorized},
		{name: "non-admin put", method: http.MethodPut, path: "/api/v1/admin/settings/usage-display", authorize: "Bearer user-token", wantStatus: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
			request.Header.Set("Authorization", tc.authorize)
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, request)

			require.Equal(t, tc.wantStatus, recorder.Code)
		})
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings/usage-display", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	code, message, data := decodeUsageDisplayResponse(t, recorder)
	require.Equal(t, 0, code)
	require.Equal(t, "success", message)
	require.Equal(t, service.DefaultUsageDisplayConfig(), data)
}

func TestUsageDisplayAdminPutReturnsUpdatedConfigInStandardEnvelope(t *testing.T) {
	repo := &usageDisplayRoutesSettingRepo{values: map[string]string{}}
	router := newUsageDisplayRoutesTestRouter(repo)
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/settings/usage-display",
		strings.NewReader(`{"fields":{"speed":false},"token_decimals":3}`),
	)
	request.Header.Set("Authorization", "Bearer admin-token")
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	code, message, data := decodeUsageDisplayResponse(t, recorder)
	require.Equal(t, 0, code)
	require.Equal(t, "success", message)
	require.False(t, data.Fields.Speed)
	require.Equal(t, 3, data.TokenDecimals)
	require.NotEmpty(t, repo.values[service.SettingKeyUsageDisplayConfig])
}
