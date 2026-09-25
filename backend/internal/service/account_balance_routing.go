package service

import "time"

// filterAccountsByBalanceAvailability applies the fail-open balance policy to
// scheduler candidates. Probes are deliberately not performed here.
func filterAccountsByBalanceAvailability(accounts []Account, now time.Time) []Account {
	if len(accounts) == 0 {
		return accounts
	}
	filtered := make([]Account, 0, len(accounts))
	for i := range accounts {
		if accountBalanceBlocksScheduling(&accounts[i], now) {
			continue
		}
		filtered = append(filtered, accounts[i])
	}
	return filtered
}

func accountBalanceBlocksScheduling(account *Account, now time.Time) bool {
	if account == nil || len(account.Extra) == 0 {
		return false
	}
	if raw, ok := account.Extra["upstream_balance"].(map[string]any); ok {
		status, _ := raw["status"].(string)
		if status != "low" && status != "zero" {
			return false
		}
		freshUntil := balanceExtraUnix(raw["fresh_until"])
		return freshUntil > 0 && now.Unix() <= freshUntil && balanceSnapshotHasNumericValue(raw)
	}
	// Legacy snapshots predate upstream_balance. They are eligible only when
	// their update timestamp is present and still within the normal freshness TTL.
	balance, ok := cnParseF64(account.Extra[cnExtraKey(account.Platform, cnBalanceExtraSuffixBalance)])
	if !ok {
		return false
	}
	updatedAt := balanceExtraUnix(account.Extra[cnExtraKey(account.Platform, cnBalanceExtraSuffixUpdated)])
	if updatedAt <= 0 || now.Unix() > updatedAt+int64(cnBalanceFreshTTL/time.Second) {
		return false
	}
	if available, ok := account.Extra[cnExtraKey(account.Platform, cnBalanceExtraSuffixAvailable)].(bool); ok && !available {
		return true
	}
	if low, ok := account.Extra[cnExtraKey(account.Platform, cnBalanceExtraSuffixLow)].(bool); ok && low {
		return true
	}
	return balance == 0
}

func balanceSnapshotHasNumericValue(raw map[string]any) bool {
	balance, ok := cnParseF64(raw["balance"])
	return ok && balance >= 0
}
