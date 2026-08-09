package service

import (
	"context"
	"encoding/json"
	"math"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type pricingOverrideSettingRepo struct {
	mu     sync.Mutex
	values map[string]string
	setErr error
}

func (r *pricingOverrideSettingRepo) Get(_ context.Context, key string) (*Setting, error) {
	value, err := r.GetValue(context.Background(), key)
	if err != nil {
		return nil, err
	}
	return &Setting{Key: key, Value: value}, nil
}

func (r *pricingOverrideSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (r *pricingOverrideSettingRepo) Set(_ context.Context, key, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.setErr != nil {
		return r.setErr
	}
	if r.values == nil {
		r.values = make(map[string]string)
	}
	r.values[key] = value
	return nil
}

func (r *pricingOverrideSettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}

func (r *pricingOverrideSettingRepo) SetMultiple(_ context.Context, values map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.setErr != nil {
		return r.setErr
	}
	if r.values == nil {
		r.values = make(map[string]string)
	}
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}

func (r *pricingOverrideSettingRepo) GetAll(_ context.Context) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	values := make(map[string]string, len(r.values))
	for key, value := range r.values {
		values[key] = value
	}
	return values, nil
}

func (r *pricingOverrideSettingRepo) Delete(_ context.Context, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.values, key)
	return nil
}

func pricingOverrideFloat(value float64) *float64 { return &value }

func pricingOverrideInt(value int) *int { return &value }

func TestPricingService_ModelPricingOverridePersistsAndReloads(t *testing.T) {
	repo := &pricingOverrideSettingRepo{values: make(map[string]string)}
	svc := &PricingService{pricingData: make(map[string]*LiteLLMModelPricing)}
	override := ModelPricingOverride{
		InputCostPerToken:              pricingOverrideFloat(0.000003),
		LongContextInputTokenThreshold: pricingOverrideInt(128000),
	}

	require.NoError(t, svc.SetModelPricingOverride(context.Background(), repo, "  GPT-4O  ", override))

	var persisted map[string]ModelPricingOverride
	require.NoError(t, json.Unmarshal([]byte(repo.values[SettingKeyModelPricingOverrides]), &persisted))
	gotPersisted, ok := persisted["gpt-4o"]
	require.True(t, ok)
	require.Equal(t, *override.InputCostPerToken, *gotPersisted.InputCostPerToken)
	require.Equal(t, *override.LongContextInputTokenThreshold, *gotPersisted.LongContextInputTokenThreshold)

	reloaded := NewPricingService(nil, nil)
	require.NoError(t, reloaded.LoadModelPricingOverrides(context.Background(), repo))
	got, ok := reloaded.GetModelPricingOverride("models/GPT-4O")
	require.True(t, ok)
	require.Equal(t, *override.InputCostPerToken, *got.InputCostPerToken)
	require.Equal(t, *override.LongContextInputTokenThreshold, *got.LongContextInputTokenThreshold)

	require.NoError(t, reloaded.SetModelPricingOverride(context.Background(), repo, "gpt-4o", ModelPricingOverride{}))
	_, ok = reloaded.GetModelPricingOverride("gpt-4o")
	require.False(t, ok)
	require.JSONEq(t, `{}`, repo.values[SettingKeyModelPricingOverrides])
}

func TestPricingService_SetModelPricingOverrideRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name     string
		override ModelPricingOverride
	}{
		{name: "negative price", override: ModelPricingOverride{InputCostPerToken: pricingOverrideFloat(-1)}},
		{name: "nan price", override: ModelPricingOverride{OutputCostPerToken: pricingOverrideFloat(math.NaN())}},
		{name: "infinite price", override: ModelPricingOverride{OutputCostPerToken: pricingOverrideFloat(math.Inf(1))}},
		{name: "negative threshold", override: ModelPricingOverride{LongContextInputTokenThreshold: pricingOverrideInt(-1)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &pricingOverrideSettingRepo{values: make(map[string]string)}
			svc := NewPricingService(nil, nil)

			err := svc.SetModelPricingOverride(context.Background(), repo, "gpt-4o", tt.override)
			require.Error(t, err)
			require.Empty(t, repo.values)
			_, ok := svc.GetModelPricingOverride("gpt-4o")
			require.False(t, ok)
		})
	}
}

func TestPricingService_LoadModelPricingOverridesRejectsInvalidData(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "malformed json", raw: `{"gpt-4o":`},
		{name: "negative price", raw: `{"gpt-4o":{"input_cost_per_token":-1}}`},
		{name: "negative threshold", raw: `{"gpt-4o":{"long_context_input_token_threshold":-1}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &pricingOverrideSettingRepo{values: map[string]string{
				SettingKeyModelPricingOverrides: tt.raw,
			}}
			svc := NewPricingService(nil, nil)

			err := svc.LoadModelPricingOverrides(context.Background(), repo)
			require.Error(t, err)
			_, ok := svc.GetModelPricingOverride("gpt-4o")
			require.False(t, ok)
		})
	}
}

func TestPricingService_ModelPricingOverrideInheritsCurrentCatalogValues(t *testing.T) {
	base := &LiteLLMModelPricing{
		InputCostPerToken:              1,
		OutputCostPerToken:             2,
		LongContextInputTokenThreshold: 100,
	}
	repo := &pricingOverrideSettingRepo{values: make(map[string]string)}
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{"gpt-4o": base},
		overrides:   make(map[string]ModelPricingOverride),
	}

	require.NoError(t, svc.SetModelPricingOverride(context.Background(), repo, "gpt-4o", ModelPricingOverride{
		InputCostPerToken: pricingOverrideFloat(3),
	}))

	got := svc.GetModelPricing("models/GPT-4O")
	require.NotNil(t, got)
	require.NotSame(t, base, got)
	require.Equal(t, float64(3), got.InputCostPerToken)
	require.Equal(t, float64(2), got.OutputCostPerToken)
	require.Equal(t, 100, got.LongContextInputTokenThreshold)

	base.OutputCostPerToken = 4
	base.LongContextInputTokenThreshold = 200
	got = svc.GetModelPricing("gpt-4o")
	require.NotNil(t, got)
	require.Equal(t, float64(3), got.InputCostPerToken)
	require.Equal(t, float64(4), got.OutputCostPerToken)
	require.Equal(t, 200, got.LongContextInputTokenThreshold)
}
