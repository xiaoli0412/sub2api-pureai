package handler

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
	"github.com/gin-gonic/gin"
)

const (
	astrBotSystemLogDefaultPageSize = 50
	astrBotSystemLogMaxPageSize     = 100
	astrBotSystemLogMaxFilterBytes  = 256
	astrBotSystemLogMaxMessageBytes = 4096
	astrBotSystemLogMaxExtraBytes   = 16 * 1024
)

var astrBotSystemLogSensitiveKeys = []string{
	"api_key", "apikey", "authorization", "bearer", "cookie", "credentials",
	"credential", "installation_secret", "private_key", "proxy", "raw_token", "secret",
	"set-cookie", "signing_secret", "request_body", "response_body",
}

type astrBotSystemLogFilter struct {
	service.OpsSystemLogFilter
	component string
	userID    *int64
	accountID *int64
}

type astrBotSystemLogDTO struct {
	ID              int64          `json:"id"`
	CreatedAt       time.Time      `json:"created_at"`
	Host            string         `json:"host,omitempty"`
	Level           string         `json:"level"`
	Component       string         `json:"component"`
	Message         string         `json:"message"`
	RequestID       string         `json:"request_id,omitempty"`
	ClientRequestID string         `json:"client_request_id,omitempty"`
	UserID          *int64         `json:"user_id,omitempty"`
	AccountID       *int64         `json:"account_id,omitempty"`
	Platform        string         `json:"platform,omitempty"`
	Model           string         `json:"model,omitempty"`
	Extra           map[string]any `json:"extra,omitempty"`
}

func parseAstrBotSystemLogQuery(c *gin.Context, name string) (string, error) {
	value := strings.TrimSpace(c.Query(name))
	if len(value) > astrBotSystemLogMaxFilterBytes {
		return "", infraerrors.BadRequest("INVALID_LOG_FILTER", name+" is too long")
	}
	return value, nil
}

func parseAstrBotSystemLogID(c *gin.Context, name string) (*int64, error) {
	value, err := parseAstrBotSystemLogQuery(c, name)
	if err != nil {
		return nil, err
	}
	if value == "" {
		return nil, nil
	}
	id, parseErr := strconv.ParseInt(value, 10, 64)
	if parseErr != nil || id <= 0 {
		return nil, infraerrors.BadRequest("INVALID_LOG_FILTER", name+" must be a positive integer")
	}
	return &id, nil
}

func parseAstrBotSystemLogFilter(c *gin.Context) (astrBotSystemLogFilter, error) {
	rangeValue, err := parseAstrBotTimeRange(c)
	if err != nil {
		return astrBotSystemLogFilter{}, err
	}
	page, err := parseAstrBotStrictPositiveInt(c, "page", 1, 1000000)
	if err != nil {
		return astrBotSystemLogFilter{}, err
	}
	pageSize, err := parseAstrBotStrictPositiveInt(c, "page_size", astrBotSystemLogDefaultPageSize, astrBotSystemLogMaxPageSize)
	if err != nil {
		return astrBotSystemLogFilter{}, err
	}

	filter := astrBotSystemLogFilter{}
	filter.StartTime = &rangeValue.start
	filter.EndTime = &rangeValue.end
	filter.Page = page
	filter.PageSize = pageSize
	for _, name := range []string{"host", "level", "component", "request_id", "client_request_id", "platform", "model", "q"} {
		value, queryErr := parseAstrBotSystemLogQuery(c, name)
		if queryErr != nil {
			return astrBotSystemLogFilter{}, queryErr
		}
		switch name {
		case "host":
			filter.Host = value
		case "level":
			filter.Level = strings.ToLower(value)
		case "component":
			filter.Component = value
			filter.component = value
		case "request_id":
			filter.RequestID = value
		case "client_request_id":
			filter.ClientRequestID = value
		case "platform":
			filter.Platform = value
		case "model":
			filter.Model = value
		case "q":
			filter.Query = value
		}
	}
	filter.UserID, err = parseAstrBotSystemLogID(c, "user_id")
	if err != nil {
		return astrBotSystemLogFilter{}, err
	}
	filter.userID = filter.UserID
	filter.APIKeyID, err = parseAstrBotSystemLogID(c, "api_key_id")
	if err != nil {
		return astrBotSystemLogFilter{}, err
	}
	filter.AccountID, err = parseAstrBotSystemLogID(c, "account_id")
	if err != nil {
		return astrBotSystemLogFilter{}, err
	}
	filter.accountID = filter.AccountID
	return filter, nil
}

func astrBotSystemLogHasApplicableAllowlist(token *service.AstrBotToken) bool {
	if token == nil {
		return false
	}
	allowlist := token.ResourceAllowlist
	return allowlist.IsUnrestricted() || len(allowlist.LogSources) > 0 || len(allowlist.AccountIDs) > 0 || len(allowlist.UserIDs) > 0
}

func astrBotSystemLogResourceAllowed(c *gin.Context, token *service.AstrBotToken, filter astrBotSystemLogFilter) bool {
	if token == nil || !astrBotSystemLogHasApplicableAllowlist(token) {
		response.NotFound(c, "resource not found")
		return false
	}
	allowlist := token.ResourceAllowlist
	if filter.component != "" && len(allowlist.LogSources) > 0 && !allowlist.AllowsLogSource(filter.component) {
		response.NotFound(c, "resource not found")
		return false
	}
	if filter.userID != nil && len(allowlist.UserIDs) > 0 && !allowlist.AllowsUser(*filter.userID) {
		response.NotFound(c, "resource not found")
		return false
	}
	if filter.accountID != nil && len(allowlist.AccountIDs) > 0 && !allowlist.AllowsAccount(*filter.accountID) {
		response.NotFound(c, "resource not found")
		return false
	}
	if len(allowlist.ChannelIDs) > 0 || len(allowlist.GroupIDs) > 0 {
		response.NotFound(c, "resource not found")
		return false
	}
	return true
}

func astrBotSystemLogFilterForToken(filter astrBotSystemLogFilter, token *service.AstrBotToken) service.OpsSystemLogFilter {
	result := filter.OpsSystemLogFilter
	if token == nil || token.ResourceAllowlist.IsUnrestricted() {
		return result
	}
	allowlist := token.ResourceAllowlist
	if len(allowlist.LogSources) > 0 {
		result.Components = append([]string(nil), allowlist.LogSources...)
	}
	if len(allowlist.AccountIDs) > 0 {
		result.AccountIDs = append([]int64(nil), allowlist.AccountIDs...)
	}
	if len(allowlist.UserIDs) > 0 {
		result.UserIDs = append([]int64(nil), allowlist.UserIDs...)
	}
	return result
}

func truncateAstrBotSystemLog(value string, maxBytes int) string {
	value = strings.TrimSpace(value)
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}
	const suffix = "..."
	if maxBytes <= len(suffix) {
		return suffix[:maxBytes]
	}
	limit := maxBytes - len(suffix)
	cut := 0
	for _, r := range value {
		runeBytes := len(string(r))
		if cut+runeBytes > limit {
			break
		}
		cut += runeBytes
	}
	return value[:cut] + suffix
}

func toAstrBotSystemLogDTO(value *service.OpsSystemLog) astrBotSystemLogDTO {
	if value == nil {
		return astrBotSystemLogDTO{}
	}
	extra := logredact.RedactMap(value.Extra, astrBotSystemLogSensitiveKeys...)
	if len(extra) > 0 {
		if encoded, err := jsonMarshal(extra); err != nil || len(encoded) > astrBotSystemLogMaxExtraBytes {
			extra = map[string]any{"_redacted": "extra payload too large"}
		}
	}
	return astrBotSystemLogDTO{
		ID: value.ID, CreatedAt: value.CreatedAt, Host: truncateAstrBotSystemLog(value.Host, astrBotSystemLogMaxFilterBytes),
		Level: truncateAstrBotSystemLog(value.Level, 32), Component: truncateAstrBotSystemLog(value.Component, astrBotSystemLogMaxFilterBytes),
		Message:   truncateAstrBotSystemLog(logredact.RedactText(value.Message, astrBotSystemLogSensitiveKeys...), astrBotSystemLogMaxMessageBytes),
		RequestID: truncateAstrBotSystemLog(value.RequestID, astrBotSystemLogMaxFilterBytes), ClientRequestID: truncateAstrBotSystemLog(value.ClientRequestID, astrBotSystemLogMaxFilterBytes),
		UserID: value.UserID, AccountID: value.AccountID, Platform: truncateAstrBotSystemLog(value.Platform, astrBotSystemLogMaxFilterBytes),
		Model: truncateAstrBotSystemLog(value.Model, astrBotSystemLogMaxFilterBytes), Extra: extra,
	}
}

func jsonMarshal(value any) ([]byte, error) {
	return json.Marshal(value)
}

// Logs returns redacted, read-only Ops system logs for AstrBot.
// GET /api/v1/bot/logs
func (h *AstrBotHandler) Logs(c *gin.Context) {
	if h == nil || h.ops == nil {
		response.Error(c, 503, "Ops service not available")
		return
	}
	token, ok := astrBotTokenFromContext(c)
	if !ok {
		response.NotFound(c, "resource not found")
		return
	}
	filter, err := parseAstrBotSystemLogFilter(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if !astrBotSystemLogResourceAllowed(c, token, filter) {
		return
	}
	result, err := h.ops.ListSystemLogs(c.Request.Context(), func() *service.OpsSystemLogFilter {
		value := astrBotSystemLogFilterForToken(filter, token)
		return &value
	}())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	items := make([]astrBotSystemLogDTO, 0, len(result.Logs))
	for _, value := range result.Logs {
		items = append(items, toAstrBotSystemLogDTO(value))
	}
	response.Paginated(c, items, int64(result.Total), result.Page, result.PageSize)
}
