package repository

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// These tests pin the "substantial streaming text traffic" sample definition
// that the passive monitor rollups are built from. The rule is a product
// decision: channel health, cache rate and latency are only comparable when
// every counted sample has the same shape. If a future change reverts any of
// these predicates, V2 and V3 numbers silently start mixing traffic classes
// again, so each clause is asserted explicitly rather than as one blob.

func TestChannelMonitorV2UsageSampleFilterRequiresSubstantialStreamingText(t *testing.T) {
	f := channelMonitorV2UsageSampleFilterUL

	// Still bounded by the shared success predicate (ul.actual_cost > 0).
	require.Contains(t, f, usageLogSuccessFilterUL)
	// Streaming only.
	require.Contains(t, f, "ul.stream IS TRUE")
	// Exclude the non-chat request types the previous code already excluded.
	require.Contains(t, f, "COALESCE(ul.request_type, 0) NOT IN (4, 6)")
	// Exclude image billing by every signal the schema exposes: the billing
	// mode plus all three image token counters.
	require.Contains(t, f, "COALESCE(ul.billing_mode, 'token') <> 'image'")
	require.Contains(t, f, "COALESCE(ul.image_count, 0) = 0")
	require.Contains(t, f, "COALESCE(ul.image_input_tokens, 0) = 0")
	require.Contains(t, f, "COALESCE(ul.image_output_tokens, 0) = 0")
	// Substantial prompt: image tokens are already excluded above, so total
	// input is input + both cache counters.
	require.Contains(t, f, "COALESCE(ul.input_tokens, 0)")
	require.Contains(t, f, "COALESCE(ul.cache_creation_tokens, 0)")
	require.Contains(t, f, "COALESCE(ul.cache_read_tokens, 0)")
	require.Contains(t, f, "> 10000")
}

func TestChannelMonitorV2ErrorSampleFilterStaysDeliberatelyLooser(t *testing.T) {
	f := channelMonitorV2ErrorSampleFilter

	require.Contains(t, f, "current_error.stream IS TRUE")
	require.Contains(t, f, "COALESCE(current_error.request_type, 0) NOT IN (4, 6)")
	require.Contains(t, f, "current_error.inbound_endpoint")
	require.Contains(t, f, "current_error.request_path")
	require.Contains(t, f, "NOT LIKE '/v1/images%'")

	// Error rows carry no token counts, so the usage-side volume rule must NOT
	// be applied here: doing so would hide real upstream failures from the
	// success rate.
	require.NotContains(t, f, "10000")
	require.NotContains(t, f, "input_tokens")
	require.NotContains(t, f, "actual_cost")
}

// Every metric column in the 1m rollups must be gated by the sample filter.
// The legacy shape (a bare `NOT IN (4, 6) AND <success>` on success_requests
// plus an ungated token/latency sum) must not survive anywhere.
func TestChannelMonitorV2RollupsGateEveryMetricOnTheSampleFilter(t *testing.T) {
	legacyBare := "NOT IN (4, 6) AND " + usageLogSuccessFilterUL

	for name, query := range map[string]string{
		"metrics":     channelMonitorV2UsageMetricsSQL,
		"userMetrics": channelMonitorV2UserMetricsSQL,
		"histogram":   channelMonitorV2HistogramSQL,
	} {
		require.Contains(t, query, channelMonitorV2UsageSampleFilterUL, "%s rollup must use the sample filter", name)
		require.Contains(t, query, "ul.stream IS TRUE", "%s rollup must require streaming", name)
		require.NotContains(t, query, legacyBare, "%s rollup still uses the legacy bare filter", name)
	}

	// The metrics rollups gate nine distinct metric expressions
	// (success_requests, 4 token sums, 2 ttft, 2 duration).
	require.GreaterOrEqual(t, strings.Count(channelMonitorV2UsageMetricsSQL, channelMonitorV2UsageSampleFilterUL), 9)
	require.GreaterOrEqual(t, strings.Count(channelMonitorV2UserMetricsSQL, channelMonitorV2UsageSampleFilterUL), 9)
	require.Contains(t, channelMonitorV2HistogramSQL, channelMonitorV2UsageSampleFilterUL)
}

func TestChannelMonitorV2ErrorAggregationAppliesErrorSampleFilter(t *testing.T) {
	require.Contains(t, channelMonitorV2ErrorAggregationSQL, channelMonitorV2ErrorSampleFilter)
	require.Contains(t, channelMonitorV2ErrorAggregationSQL, "current_error.stream IS TRUE")
	// The pre-existing terminal-error guards must survive the addition.
	require.Contains(t, channelMonitorV2ErrorAggregationSQL, "AND NOT current_error.is_count_tokens")
	require.Contains(t, channelMonitorV2ErrorAggregationSQL, "current_error.error_type = 'cyber_policy'")
}

// Guard against the two filters drifting into each other: the usage filter is
// usage_logs-scoped (ul.) and the error filter is ops_error_logs-scoped
// (current_error.). Cross-referencing aliases would compile but never match a row.
func TestChannelMonitorV2SampleFiltersUseTheirOwnAliases(t *testing.T) {
	require.NotContains(t, channelMonitorV2UsageSampleFilterUL, "current_error")
	require.NotContains(t, channelMonitorV2ErrorSampleFilter, "ul.")
}
