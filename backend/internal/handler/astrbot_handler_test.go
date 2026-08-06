package handler

import (
	"math"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAstrBotPricingRequestToServicePricing_UsesPublicFields(t *testing.T) {
	input := 0.25
	maxTokens := 1000
	req := astrBotPricingRequest{Pricing: []astrBotPricingEntryRequest{{
		Platform:    " openai ",
		Models:      []string{" gpt-4o "},
		BillingMode: service.BillingModeToken,
		InputPrice:  &input,
		Intervals:   []astrBotPricingIntervalRequest{{MinTokens: 0, MaxTokens: &maxTokens, TierLabel: "standard"}},
	}}}

	pricing, err := req.toServicePricing()

	require.NoError(t, err)
	require.Len(t, pricing, 1)
	require.Equal(t, "openai", pricing[0].Platform)
	require.Equal(t, []string{"gpt-4o"}, pricing[0].Models)
	require.Equal(t, service.BillingModeToken, pricing[0].BillingMode)
	require.Same(t, &input, pricing[0].InputPrice)
	require.Equal(t, maxTokens, *pricing[0].Intervals[0].MaxTokens)
}

func TestAstrBotPricingRequestToServicePricing_RejectsNonFinitePrices(t *testing.T) {
	for name, value := range map[string]float64{
		"nan":               math.NaN(),
		"positive infinity": math.Inf(1),
		"negative infinity": math.Inf(-1),
	} {
		t.Run(name, func(t *testing.T) {
			req := astrBotPricingRequest{Pricing: []astrBotPricingEntryRequest{{
				Platform:    "openai",
				Models:      []string{"gpt-4o"},
				OutputPrice: &value,
			}}}

			_, err := req.toServicePricing()

			require.Error(t, err)
			require.True(t, errors.IsBadRequest(err))
		})
	}
}

func TestAstrBotPricingRequestToServicePricing_RejectsInvalidInterval(t *testing.T) {
	maxTokens := 10
	req := astrBotPricingRequest{Pricing: []astrBotPricingEntryRequest{{
		Platform:  "openai",
		Models:    []string{"gpt-4o"},
		Intervals: []astrBotPricingIntervalRequest{{MinTokens: 10, MaxTokens: &maxTokens}},
	}}}

	_, err := req.toServicePricing()

	require.Error(t, err)
	require.True(t, errors.IsBadRequest(err))
}
