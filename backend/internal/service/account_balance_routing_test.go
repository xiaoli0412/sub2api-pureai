package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAccountBalanceBlocksScheduling(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	freshUntil := now.Add(5 * time.Minute).Unix()
	cases := []struct {
		name  string
		extra map[string]any
		want  bool
	}{
		{name: "fresh low blocks", extra: map[string]any{"upstream_balance": map[string]any{"status": "low", "balance": 0.1, "fresh_until": freshUntil}}, want: true},
		{name: "fresh zero blocks", extra: map[string]any{"upstream_balance": map[string]any{"status": "zero", "balance": 0, "fresh_until": freshUntil}}, want: true},
		{name: "stale low fails open", extra: map[string]any{"upstream_balance": map[string]any{"status": "low", "balance": 0, "fresh_until": now.Add(-time.Minute).Unix()}}, want: false},
		{name: "unknown fails open", extra: map[string]any{"upstream_balance": map[string]any{"status": "network_error", "balance": 0, "fresh_until": freshUntil}}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{Platform: PlatformDeepseek, Extra: tc.extra}
			require.Equal(t, tc.want, accountBalanceBlocksScheduling(account, now))
		})
	}
}
