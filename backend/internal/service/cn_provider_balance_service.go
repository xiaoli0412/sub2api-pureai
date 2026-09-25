package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/tidwall/gjson"
	"golang.org/x/sync/singleflight"
)

// 国产供应商 payg（按量付费）账号余额探测服务。
//
// 仅覆盖有公开余额端点的供应商：
//   - Kimi/Moonshot：GET https://api.moonshot.cn/v1/users/me/balance (Bearer) → data.available_balance
//   - DeepSeek：     GET https://api.deepseek.com/user/balance (Bearer) → balance_infos[].total_balance + is_available
//
// 智谱（zhipu）无公开余额端点（OpenAPI 规格验证），仅靠响应式 429/402（见
// ratelimit_cn_providers.go）。解析逻辑对齐 cc-switch services/balance.rs::query_deepseek。
const (
	cnBalanceUpstreamTimeout = 15 * time.Second
	cnBalanceMaxBodyBytes    = 256 * 1024
	cnBalanceFreshTTL        = 15 * time.Minute

	// Extra 余额快照键后缀（加 provider 前缀）。
	cnBalanceExtraSuffixBalance   = "balance"
	cnBalanceExtraSuffixCurrency  = "balance_currency"
	cnBalanceExtraSuffixAvailable = "balance_available" // deepseek is_available 健康标记
	cnBalanceExtraSuffixUpdated   = "balance_updated_at"
	cnBalanceExtraSuffixBalances  = "balances" // 多币种明细（deepseek USD+CNY）
)

// CNProviderBalanceEntry 是单一币种的余额明细。
type CNProviderBalanceEntry struct {
	Currency string  `json:"currency"`
	Balance  float64 `json:"balance"`
}

// CNProviderBalanceResult 是余额探测的返回结构（管理端 + UI 消费）。
type CNProviderBalanceResult struct {
	Provider string `json:"provider"`
	Success  bool   `json:"success"`
	// Status distinguishes a valid zero/low balance from a failed probe.
	// It is intentionally explicit because Success=false must never be treated as zero.
	Status string `json:"status"`
	// Balance/Currency 为主币种（balance_infos 首条，兼容单币种消费方）；
	// 完整明细见 Balances（deepseek 双币种账号含 CNY + USD 两条）。
	Balance        float64                  `json:"balance"`
	Currency       string                   `json:"currency,omitempty"`
	Balances       []CNProviderBalanceEntry `json:"balances,omitempty"`
	Available      bool                     `json:"available"` // 健康标记（deepseek is_available；kimi 无此概念恒 true）
	StatusCode     int                      `json:"status_code,omitempty"`
	FetchedAt      int64                    `json:"fetched_at"`
	FreshUntil     int64                    `json:"fresh_until,omitempty"`
	Stale          bool                     `json:"stale"`
	RateMultiplier float64                  `json:"rate_multiplier,omitempty"`
	Persisted      bool                     `json:"persisted"`
	Error          string                   `json:"error,omitempty"`
}

// AccountBalanceResult 是账号管理使用的统一余额结果。
// CNProviderBalanceResult 保持原有 CN 专用接口契约；该类型通过别名扩展字段，
// 让旧客户端继续兼容，同时为非 CN 账号返回 unsupported + 本地倍率。
type AccountBalanceResult = CNProviderBalanceResult

// GetAccountBalance 返回最近一次成功的余额快照，不访问上游。
func (s *CNProviderBalanceService) GetAccountBalance(ctx context.Context, accountID int64) (*AccountBalanceResult, error) {
	if s == nil || s.accountRepo == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "BALANCE_NOT_CONFIGURED", "account balance service is not configured")
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil || account == nil {
		return nil, infraerrors.New(http.StatusNotFound, "BALANCE_ACCOUNT_NOT_FOUND", "account not found")
	}
	return accountBalanceSnapshot(account), nil
}

// QueryAccountBalance probes providers with a known balance adapter and returns
// an explicit unsupported result for providers without one.
func (s *CNProviderBalanceService) QueryAccountBalance(ctx context.Context, accountID int64) (*AccountBalanceResult, error) {
	if s == nil || s.accountRepo == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "BALANCE_NOT_CONFIGURED", "account balance service is not configured")
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil || account == nil {
		return nil, infraerrors.New(http.StatusNotFound, "BALANCE_ACCOUNT_NOT_FOUND", "account not found")
	}
	if account.Platform == PlatformKimi || account.Platform == PlatformDeepseek {
		if err := validatePayGAccount(account); err != nil {
			return &CNProviderBalanceResult{Provider: account.Platform, Status: "unsupported", RateMultiplier: account.BillingRateMultiplier(), Error: "This account uses a quota plan; balance probing is not supported"}, nil
		}
		return s.QueryBalanceForAccount(ctx, account)
	}
	return unsupportedAccountBalance(account), nil
}

func accountBalanceSnapshot(account *Account) *AccountBalanceResult {
	if account == nil {
		return unsupportedAccountBalance(account)
	}
	if raw, ok := account.Extra["upstream_balance"].(map[string]any); ok {
		if result := accountBalanceFromMap(account, raw); result != nil {
			return result
		}
	}
	if legacy := legacyAccountBalanceSnapshot(account); legacy != nil {
		return legacy
	}
	if account.Platform == PlatformKimi || account.Platform == PlatformDeepseek {
		return &AccountBalanceResult{
			Provider:       account.Platform,
			Status:         "unknown",
			RateMultiplier: account.BillingRateMultiplier(),
			Available:      true,
			Error:          "No successful balance snapshot is available",
		}
	}
	return unsupportedAccountBalance(account)
}

func legacyAccountBalanceSnapshot(account *Account) *AccountBalanceResult {
	if account == nil || account.Extra == nil {
		return nil
	}
	provider := account.Platform
	rawBalance, exists := account.Extra[cnExtraKey(provider, cnBalanceExtraSuffixBalance)]
	if !exists {
		return nil
	}
	balance, ok := cnParseF64(rawBalance)
	if !ok {
		return nil
	}
	result := &AccountBalanceResult{
		Provider:       provider,
		Status:         "success",
		Success:        true,
		Balance:        balance,
		Available:      true,
		RateMultiplier: account.BillingRateMultiplier(),
	}
	result.Currency, _ = account.Extra[cnExtraKey(provider, cnBalanceExtraSuffixCurrency)].(string)
	if available, ok := account.Extra[cnExtraKey(provider, cnBalanceExtraSuffixAvailable)].(bool); ok {
		result.Available = available
	}
	result.FetchedAt = balanceExtraUnix(account.Extra[cnExtraKey(provider, cnBalanceExtraSuffixUpdated)])
	if result.FetchedAt > 0 {
		result.FreshUntil = result.FetchedAt + int64(cnBalanceFreshTTL/time.Second)
	}
	result.Balances = balanceEntriesFromRaw(account.Extra[cnExtraKey(provider, cnBalanceExtraSuffixBalances)])
	if len(result.Balances) == 0 {
		result.Balances = []CNProviderBalanceEntry{{Currency: result.Currency, Balance: result.Balance}}
	}
	if low, ok := account.Extra[cnExtraKey(provider, cnBalanceExtraSuffixLow)].(bool); ok && low {
		result.Status = "low"
	} else if !result.Available {
		result.Status = "low"
	} else if result.Balance == 0 && len(result.Balances) == 1 {
		result.Status = "zero"
	}
	if result.FreshUntil > 0 && time.Now().UTC().Unix() > result.FreshUntil {
		result.Stale = true
		result.Status = "stale"
	}
	return result
}

func balanceEntriesFromRaw(raw any) []CNProviderBalanceEntry {
	var entries []CNProviderBalanceEntry
	switch values := raw.(type) {
	case []any:
		for _, value := range values {
			if entry, ok := balanceEntryFromRaw(value); ok {
				entries = append(entries, entry)
			}
		}
	case []map[string]any:
		for _, value := range values {
			if entry, ok := balanceEntryFromRaw(value); ok {
				entries = append(entries, entry)
			}
		}
	}
	return entries
}

func balanceEntryFromRaw(raw any) (CNProviderBalanceEntry, bool) {
	value, ok := raw.(map[string]any)
	if !ok {
		return CNProviderBalanceEntry{}, false
	}
	balance, ok := cnParseF64(value["balance"])
	if !ok {
		return CNProviderBalanceEntry{}, false
	}
	currency, _ := value["currency"].(string)
	return CNProviderBalanceEntry{Currency: currency, Balance: balance}, true
}

func unsupportedAccountBalance(account *Account) *AccountBalanceResult {
	result := &AccountBalanceResult{Status: "unsupported", Error: "This provider does not expose a supported balance endpoint"}
	if account != nil {
		result.Provider = account.Platform
		result.RateMultiplier = account.BillingRateMultiplier()
	}
	return result
}

func accountBalanceFromMap(account *Account, raw map[string]any) *AccountBalanceResult {
	status, _ := raw["status"].(string)
	balance, ok := cnParseF64(raw["balance"])
	if status == "" || !ok {
		return nil
	}
	result := &AccountBalanceResult{
		Provider:       account.Platform,
		Status:         status,
		Success:        status == "success" || status == "zero" || status == "low",
		Balance:        balance,
		Available:      true,
		FetchedAt:      balanceExtraUnix(raw["fetched_at"]),
		FreshUntil:     balanceExtraUnix(raw["fresh_until"]),
		RateMultiplier: account.BillingRateMultiplier(),
	}
	result.Currency, _ = raw["currency"].(string)
	if available, ok := raw["available"].(bool); ok {
		result.Available = available
	}
	if stale, ok := raw["stale"].(bool); ok {
		result.Stale = stale
	}
	if result.FreshUntil > 0 && time.Now().UTC().Unix() > result.FreshUntil {
		result.Stale = true
		if result.Success {
			result.Status = "stale"
		}
	}
	return result
}

func balanceExtraUnix(raw any) int64 {
	switch value := raw.(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case float64:
		return int64(value)
	case string:
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			return parsed.Unix()
		}
	}
	return 0
}

type CNProviderBalanceService struct {
	accountRepo  AccountRepository
	proxyRepo    ProxyRepository
	httpUpstream HTTPUpstream
	cfg          *config.Config
	flight       singleflight.Group
}

// NewCNProviderBalanceService 构造余额探测服务。
func NewCNProviderBalanceService(
	accountRepo AccountRepository,
	proxyRepo ProxyRepository,
	httpUpstream HTTPUpstream,
	cfg *config.Config,
) *CNProviderBalanceService {
	return &CNProviderBalanceService{
		accountRepo:  accountRepo,
		proxyRepo:    proxyRepo,
		httpUpstream: httpUpstream,
		cfg:          cfg,
	}
}

// QueryBalance 探测指定 payg 账号的余额并落 Extra 快照。
func (s *CNProviderBalanceService) QueryBalance(ctx context.Context, accountID int64) (*CNProviderBalanceResult, error) {
	account, err := s.loadPayGAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	return s.QueryBalanceForAccount(ctx, account)
}

// QueryBalanceForAccount 探测已加载账号（配额监控 fetcher / 周期余额检测复用，
// 避免二次 GetByID）。singleflight key 与 QueryBalance 相同，按账号 ID 合并。
func (s *CNProviderBalanceService) QueryBalanceForAccount(ctx context.Context, account *Account) (*CNProviderBalanceResult, error) {
	if s == nil || s.accountRepo == nil || s.httpUpstream == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "CN_BALANCE_NOT_CONFIGURED", "cn provider balance service is not configured")
	}
	if err := validatePayGAccount(account); err != nil {
		return nil, err
	}
	key := "cn_balance:" + strconv.FormatInt(account.ID, 10)
	resultCh := s.flight.DoChan(key, func() (any, error) {
		probeCtx, cancel := context.WithTimeout(context.Background(), cnBalanceUpstreamTimeout+5*time.Second)
		defer cancel()
		return s.queryBalanceForAccount(probeCtx, account)
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case flightResult := <-resultCh:
		if flightResult.Err != nil {
			return nil, flightResult.Err
		}
		result, ok := flightResult.Val.(*CNProviderBalanceResult)
		if !ok || result == nil {
			return nil, infraerrors.New(http.StatusInternalServerError, "CN_BALANCE_PROBE_RESULT_INVALID", "invalid cn provider balance probe result")
		}
		cloned := *result
		return &cloned, nil
	}
}

func (s *CNProviderBalanceService) queryBalanceForAccount(ctx context.Context, account *Account) (*CNProviderBalanceResult, error) {
	provider := account.Platform
	if provider != PlatformKimi && provider != PlatformDeepseek {
		return nil, infraerrors.New(http.StatusBadRequest, "CN_BALANCE_NO_ENDPOINT", "account provider has no balance endpoint")
	}

	apiKey := strings.TrimSpace(account.GetCNAPIKey())
	if apiKey == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "CN_BALANCE_NO_APIKEY", "account api_key is empty")
	}

	targetURL := cnBalanceURL(account)
	// 探测发起前过出站 URL 安全策略（与网关转发/Grok 探测同一套校验）：
	// DeepSeek 端点由账号 base_url 衍生，不得把 API key 发往策略外主机。
	validatedURL, err := cnValidateProbeURL(s.cfg, targetURL)
	if err != nil {
		return nil, infraerrors.New(http.StatusForbidden, "CN_BALANCE_URL_REJECTED", err.Error())
	}
	targetURL = validatedURL
	proxyURL := s.resolveProxyURL(ctx, account)
	callCtx, cancel := context.WithTimeout(ctx, cnBalanceUpstreamTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "CN_BALANCE_REQUEST_BUILD_FAILED", "build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	account.ApplyHeaderOverrides(req.Header)

	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, maxInt(account.Concurrency, 1))
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "CN_BALANCE_REQUEST_FAILED", "upstream request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, cnBalanceMaxBodyBytes))

	now := time.Now().UTC()
	result := &CNProviderBalanceResult{
		Provider:   provider,
		Status:     "unknown",
		FetchedAt:  now.Unix(),
		FreshUntil: now.Add(cnBalanceFreshTTL).Unix(),
		StatusCode: resp.StatusCode,
		Available:  true,
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		result.Status = "auth_failed"
		result.Error = fmt.Sprintf("Authentication failed (HTTP %d)", resp.StatusCode)
		return result, nil
	}
	if resp.StatusCode == http.StatusNotFound {
		result.Status = "endpoint_not_found"
		result.Error = "Balance endpoint was not found"
		return result, nil
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		result.Status = "rate_limited"
		result.Error = "Balance endpoint rate limited"
		return result, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Status = "unknown"
		result.Error = fmt.Sprintf("API error (HTTP %d): %s", resp.StatusCode, truncate(strings.TrimSpace(string(bodyBytes)), 240))
		return result, nil
	}

	var entries []CNProviderBalanceEntry
	available := true
	switch provider {
	case PlatformKimi:
		// Moonshot：code==0 成功；data.available_balance（number），单币种 CNY。
		balanceValue := gjson.GetBytes(bodyBytes, "data.available_balance")
		if !balanceValue.Exists() || balanceValue.Type == gjson.Null {
			result.Status = "parse_error"
			result.Error = "Invalid balance response: missing data.available_balance"
			return result, nil
		}
		balance, ok := cnParseF64(balanceValue.Value())
		if !ok {
			result.Status = "parse_error"
			result.Error = "Invalid balance response: data.available_balance is not numeric"
			return result, nil
		}
		entries = append(entries, CNProviderBalanceEntry{Currency: "CNY", Balance: balance})

	case PlatformDeepseek:
		// is_available 缺省视为 true（健康）；显式存在时取其值。
		if v := gjson.GetBytes(bodyBytes, "is_available"); v.Exists() {
			available = v.Bool()
		}
		// balance_infos 逐条解析：双币种账号同时返回 CNY + USD（数组顺序即
		// 主次序，首条为主币种）。
		balanceInfos := gjson.GetBytes(bodyBytes, "balance_infos")
		if !balanceInfos.Exists() || !balanceInfos.IsArray() {
			result.Error = "Invalid balance response: missing balance_infos"
			return result, nil
		}
		balanceInfos.ForEach(func(_, info gjson.Result) bool {
			currency := strings.ToUpper(strings.TrimSpace(info.Get("currency").String()))
			totalBalance := info.Get("total_balance")
			balance, ok := cnParseF64(totalBalance.Value())
			if !totalBalance.Exists() || !ok {
				return true
			}
			if currency == "" {
				currency = "CNY"
			}
			entries = append(entries, CNProviderBalanceEntry{Currency: currency, Balance: balance})
			return true
		})
		if len(entries) == 0 {
			result.Error = "Invalid balance response: no valid balance entries"
			return result, nil
		}
	}
	result.Balances = entries
	result.Balance = entries[0].Balance
	result.Currency = entries[0].Currency
	result.Available = available
	result.Success = true
	result.RateMultiplier = account.BillingRateMultiplier()
	if !available {
		result.Status = "low"
	} else if result.Balance == 0 && len(entries) == 1 {
		result.Status = "zero"
	} else if threshold := balanceThresholdForStatus(s.cfg); threshold >= 0 && allCNBalancesBelowThreshold(result, threshold) {
		result.Status = "low"
	} else {
		result.Status = "success"
	}

	balanceUpdates := make([]any, 0, len(entries))
	for _, entry := range entries {
		balanceUpdates = append(balanceUpdates, map[string]any{
			"currency": entry.Currency,
			"balance":  entry.Balance,
		})
	}
	updates := map[string]any{
		cnExtraKey(provider, cnBalanceExtraSuffixBalance):   result.Balance,
		cnExtraKey(provider, cnBalanceExtraSuffixCurrency):  result.Currency,
		cnExtraKey(provider, cnBalanceExtraSuffixAvailable): available,
		cnExtraKey(provider, cnBalanceExtraSuffixUpdated):   now.Format(time.RFC3339),
		cnExtraKey(provider, cnBalanceExtraSuffixBalances):  balanceUpdates,
		"upstream_balance": map[string]any{
			"provider":    result.Provider,
			"status":      result.Status,
			"balance":     result.Balance,
			"currency":    result.Currency,
			"balances":    balanceUpdates,
			"available":   result.Available,
			"fetched_at":  result.FetchedAt,
			"fresh_until": result.FreshUntil,
			"stale":       false,
		},
		// 余额探测成功即清除响应式 402/429 写下的 balance_low 标记。
		cnExtraKey(provider, cnBalanceExtraSuffixLow): false,
	}
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, updates); err != nil {
		slog.Warn("cn_balance_persist_failed", "account_id", account.ID, "provider", provider, "error", err)
	} else {
		result.Persisted = true
	}
	return result, nil
}

func balanceThresholdForStatus(cfg *config.Config) float64 {
	if cfg == nil || cfg.Gateway.CNProviders.BalanceThreshold <= 0 {
		return -1
	}
	return cfg.Gateway.CNProviders.BalanceThreshold
}

func (s *CNProviderBalanceService) loadPayGAccount(ctx context.Context, accountID int64) (*Account, error) {
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusNotFound, "CN_BALANCE_ACCOUNT_NOT_FOUND", "account not found: %v", err)
	}
	if err := validatePayGAccount(account); err != nil {
		return nil, err
	}
	return account, nil
}

// validatePayGAccount 加载后的非 DB 校验（ForAccount 入口同样复用，
// 保证直传 account 也不绕过平台/模式检查）。
func validatePayGAccount(account *Account) error {
	if account == nil {
		return infraerrors.New(http.StatusNotFound, "CN_BALANCE_ACCOUNT_NOT_FOUND", "account not found")
	}
	if !account.IsCNProvider() {
		return infraerrors.New(http.StatusBadRequest, "CN_BALANCE_INVALID_PLATFORM", "account is not a CN provider account")
	}
	// coding 账号走额度探测，余额端点不适用。
	if account.IsCodingPlan() {
		return infraerrors.New(http.StatusBadRequest, "CN_BALANCE_CODING_PLAN", "coding plan account has no balance endpoint; use quota probe")
	}
	return nil
}

func (s *CNProviderBalanceService) resolveProxyURL(ctx context.Context, account *Account) string {
	if account == nil || account.ProxyID == nil {
		return ""
	}
	if account.Proxy != nil {
		return account.Proxy.URL()
	}
	if s != nil && s.proxyRepo != nil {
		if proxy, err := s.proxyRepo.GetByID(ctx, *account.ProxyID); err == nil && proxy != nil {
			account.Proxy = proxy
			return proxy.URL()
		}
	}
	return ""
}

// cnBalanceURL 解析账号的余额端点。
//
//   - Kimi：固定 https://api.moonshot.cn/v1/users/me/balance（与 base_url 无关，Moonshot 仅此一处）
//   - DeepSeek：基于 base_url 拼接 /user/balance（支持自定义域名）
func cnBalanceURL(account *Account) string {
	switch account.Platform {
	case PlatformKimi:
		return "https://api.moonshot.cn/v1/users/me/balance"
	case PlatformDeepseek:
		// Anthropic 协议账号的凭证 base_url 指向 /anthropic 端点，余额探测需回退
		// 到 OpenAI 格式 base（协议感知）再拼接 /user/balance。
		return strings.TrimRight(account.GetOpenAIFormatBaseURL(), "/") + "/user/balance"
	default:
		return ""
	}
}
