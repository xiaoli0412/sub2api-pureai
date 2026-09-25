//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// V3 is a scope/presentation layer over the V2 passive pipeline, not a third
// collector. These tests pin the two properties that make that true:
//
//  1. selecting v3 keeps passive aggregation (and therefore the aggregator,
//     rollups and /channel-monitor-v2 read endpoints) alive, and
//  2. selecting v3 still retires V1 active probes.

func TestNormalizeChannelMonitorModeAcceptsV3(t *testing.T) {
	require.Equal(t, ChannelMonitorModeV3, normalizeChannelMonitorMode(ChannelMonitorModeV3))
	require.Equal(t, ChannelMonitorModeV3, normalizeChannelMonitorMode(" V3 "))
	require.Equal(t, ChannelMonitorModeV3, normalizeChannelMonitorMode("v3"))
}

// An unknown mode must still fail safe to v1 rather than silently selecting a
// passive pipeline, including near-miss spellings of v3.
func TestNormalizeChannelMonitorModeRejectsUnknownNearV3(t *testing.T) {
	for _, raw := range []string{"v4", "v33", "3", "v 3", "vv3", "v3x", "three"} {
		require.Equal(t, ChannelMonitorModeV1, normalizeChannelMonitorMode(raw), "input %q", raw)
	}
}

// The whole V3 wiring hangs off this one predicate: the user routes, the admin
// routes and the passive aggregator all consult PassiveAggregationAllowed.
func TestChannelMonitorRuntimePassiveAggregationAllowedForV3(t *testing.T) {
	require.True(t, (ChannelMonitorRuntime{Enabled: true, Mode: ChannelMonitorModeV3}).PassiveAggregationAllowed())
	// Regression guard: V2's behaviour must be untouched by the v3 addition.
	require.True(t, (ChannelMonitorRuntime{Enabled: true, Mode: ChannelMonitorModeV2}).PassiveAggregationAllowed())
	// V1 is still an active-probe mode and must never run passive aggregation.
	require.False(t, (ChannelMonitorRuntime{Enabled: true, Mode: ChannelMonitorModeV1}).PassiveAggregationAllowed())
	// The feature switch still dominates the mode.
	require.False(t, (ChannelMonitorRuntime{Enabled: false, Mode: ChannelMonitorModeV3}).PassiveAggregationAllowed())
}

func TestChannelMonitorRuntimeActiveProbesRetiredForV3(t *testing.T) {
	require.False(t, (ChannelMonitorRuntime{Enabled: true, Mode: ChannelMonitorModeV3}).ActiveProbesAllowed())
	require.False(t, (ChannelMonitorRuntime{Enabled: false, Mode: ChannelMonitorModeV3}).ActiveProbesAllowed())
}

// Mode predicates must stay mutually exclusive so a v3 deployment cannot
// accidentally resume V1 probing or be reported as v2 by a strict equality check.
func TestChannelMonitorModeV3IsDistinctFromV1AndV2(t *testing.T) {
	rt := ChannelMonitorRuntime{Enabled: true, Mode: ChannelMonitorModeV3}
	require.NotEqual(t, ChannelMonitorModeV1, rt.Mode)
	require.NotEqual(t, ChannelMonitorModeV2, rt.Mode)
	require.False(t, rt.ActiveProbesAllowed())
	require.True(t, rt.PassiveAggregationAllowed())
}

func TestRunCheck_ModeV3NeverProbes(t *testing.T) {
	svc := NewChannelMonitorService(nil, nil)
	svc.SetRuntimeReader(channelMonitorRuntimeStub{rt: ChannelMonitorRuntime{
		Enabled: true,
		Mode:    ChannelMonitorModeV3,
	}})

	results, err := svc.RunCheck(context.Background(), 1)
	require.ErrorIs(t, err, ErrChannelMonitorActiveProbesRetired)
	require.Nil(t, results)
}
