package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// UsageDisplayFields controls which usage table fields are shown by default.
type UsageDisplayFields struct {
	InputTokens         bool `json:"input_tokens"`
	OutputTokens        bool `json:"output_tokens"`
	CacheReadTokens     bool `json:"cache_read_tokens"`
	CacheCreationTokens bool `json:"cache_creation_tokens"`
	CacheTTLBreakdown   bool `json:"cache_ttl_breakdown"`
	FirstToken          bool `json:"first_token"`
	Duration            bool `json:"duration"`
	Speed               bool `json:"speed"`
}

// UsageDisplayConfig is the persisted usage presentation configuration.
type UsageDisplayConfig struct {
	Fields               UsageDisplayFields     `json:"fields"`
	TokenUnit            string                 `json:"token_unit"`
	TokenDecimals        int                    `json:"token_decimals"`
	DurationUnit         string                 `json:"duration_unit"`
	DurationDecimals     int                    `json:"duration_decimals"`
	SpeedFormula         string                 `json:"speed_formula"`
	SpeedDecimals        int                    `json:"speed_decimals"`
	ColorEnabled         bool                   `json:"color_enabled"`
	FirstTokenThresholds UsageDisplayThresholds `json:"first_token_thresholds"`
	DurationThresholds   UsageDisplayThresholds `json:"duration_thresholds"`
	SpeedLowThreshold    float64                `json:"speed_low_threshold"`
}

type UsageDisplayThresholds struct {
	Warn     int `json:"warn"`
	Slow     int `json:"slow"`
	Critical int `json:"critical"`
}

// UsageDisplayFieldsPatch and UsageDisplayConfigPatch preserve omitted values on PUT.
type UsageDisplayFieldsPatch struct {
	InputTokens         *bool `json:"input_tokens"`
	OutputTokens        *bool `json:"output_tokens"`
	CacheReadTokens     *bool `json:"cache_read_tokens"`
	CacheCreationTokens *bool `json:"cache_creation_tokens"`
	CacheTTLBreakdown   *bool `json:"cache_ttl_breakdown"`
	FirstToken          *bool `json:"first_token"`
	Duration            *bool `json:"duration"`
	Speed               *bool `json:"speed"`
}

type UsageDisplayThresholdsPatch struct {
	Warn     *int `json:"warn"`
	Slow     *int `json:"slow"`
	Critical *int `json:"critical"`
}

type UsageDisplayConfigPatch struct {
	Fields               *UsageDisplayFieldsPatch     `json:"fields"`
	TokenUnit            *string                      `json:"token_unit"`
	TokenDecimals        *int                         `json:"token_decimals"`
	DurationUnit         *string                      `json:"duration_unit"`
	DurationDecimals     *int                         `json:"duration_decimals"`
	SpeedFormula         *string                      `json:"speed_formula"`
	SpeedDecimals        *int                         `json:"speed_decimals"`
	ColorEnabled         *bool                        `json:"color_enabled"`
	FirstTokenThresholds *UsageDisplayThresholdsPatch `json:"first_token_thresholds"`
	DurationThresholds   *UsageDisplayThresholdsPatch `json:"duration_thresholds"`
	SpeedLowThreshold    *float64                     `json:"speed_low_threshold"`
}

func DefaultUsageDisplayConfig() UsageDisplayConfig {
	return UsageDisplayConfig{
		Fields: UsageDisplayFields{
			InputTokens: true, OutputTokens: true, CacheReadTokens: true,
			CacheCreationTokens: true, CacheTTLBreakdown: true, FirstToken: true,
			Duration: true, Speed: true,
		},
		TokenUnit: "raw", TokenDecimals: 2,
		DurationUnit: "auto", DurationDecimals: 2,
		SpeedFormula: "end_to_end", SpeedDecimals: 2,
		ColorEnabled:         true,
		FirstTokenThresholds: UsageDisplayThresholds{Warn: 10000, Slow: 30000, Critical: 60000},
		DurationThresholds:   UsageDisplayThresholds{Warn: 60000, Slow: 180000, Critical: 300000},
		SpeedLowThreshold:    10,
	}
}

var ErrInvalidUsageDisplayConfig = infraerrors.BadRequest("INVALID_USAGE_DISPLAY_CONFIG", "invalid usage display configuration")

func validateUsageDisplayConfig(cfg UsageDisplayConfig) error {
	if cfg.TokenUnit != "raw" && cfg.TokenUnit != "compact" && cfg.TokenUnit != "k" && cfg.TokenUnit != "m" {
		return ErrInvalidUsageDisplayConfig.WithCause(errors.New("token_unit must be raw, compact, k, or m"))
	}
	if cfg.DurationUnit != "auto" && cfg.DurationUnit != "ms" && cfg.DurationUnit != "s" {
		return ErrInvalidUsageDisplayConfig.WithCause(errors.New("duration_unit must be auto, ms, or s"))
	}
	if cfg.SpeedFormula != "end_to_end" && cfg.SpeedFormula != "after_first_token" {
		return ErrInvalidUsageDisplayConfig.WithCause(errors.New("speed_formula must be end_to_end or after_first_token"))
	}
	if cfg.TokenDecimals < 0 || cfg.TokenDecimals > 4 || cfg.DurationDecimals < 0 || cfg.DurationDecimals > 4 || cfg.SpeedDecimals < 0 || cfg.SpeedDecimals > 4 {
		return ErrInvalidUsageDisplayConfig.WithCause(errors.New("decimal places must be between 0 and 4"))
	}
	if err := validateUsageDisplayThresholds(cfg.FirstTokenThresholds); err != nil {
		return ErrInvalidUsageDisplayConfig.WithCause(errors.New("invalid first_token_thresholds: " + err.Error()))
	}
	if err := validateUsageDisplayThresholds(cfg.DurationThresholds); err != nil {
		return ErrInvalidUsageDisplayConfig.WithCause(errors.New("invalid duration_thresholds: " + err.Error()))
	}
	if math.IsNaN(cfg.SpeedLowThreshold) || math.IsInf(cfg.SpeedLowThreshold, 0) || cfg.SpeedLowThreshold < 0 || cfg.SpeedLowThreshold > 1000000 {
		return ErrInvalidUsageDisplayConfig.WithCause(errors.New("speed_low_threshold must be finite and between 0 and 1000000"))
	}
	return nil
}

func validateUsageDisplayThresholds(t UsageDisplayThresholds) error {
	if t.Warn < 0 || t.Slow < 0 || t.Critical < 0 {
		return errors.New("thresholds must be non-negative")
	}
	if !(t.Warn < t.Slow && t.Slow < t.Critical) {
		return errors.New("thresholds must be strictly ascending")
	}
	return nil
}

func (s *SettingService) GetUsageDisplayConfig(ctx context.Context) (UsageDisplayConfig, error) {
	defaults := DefaultUsageDisplayConfig()
	if s == nil || s.settingRepo == nil {
		return defaults, nil
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyUsageDisplayConfig)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return defaults, nil
		}
		return UsageDisplayConfig{}, err
	}
	var cfg UsageDisplayConfig
	if json.Unmarshal([]byte(raw), &cfg) != nil || validateUsageDisplayConfig(cfg) != nil {
		return defaults, nil
	}
	return cfg, nil
}

func mergeUsageDisplayConfig(base UsageDisplayConfig, patch UsageDisplayConfigPatch) UsageDisplayConfig {
	if patch.Fields != nil {
		p := patch.Fields
		if p.InputTokens != nil {
			base.Fields.InputTokens = *p.InputTokens
		}
		if p.OutputTokens != nil {
			base.Fields.OutputTokens = *p.OutputTokens
		}
		if p.CacheReadTokens != nil {
			base.Fields.CacheReadTokens = *p.CacheReadTokens
		}
		if p.CacheCreationTokens != nil {
			base.Fields.CacheCreationTokens = *p.CacheCreationTokens
		}
		if p.CacheTTLBreakdown != nil {
			base.Fields.CacheTTLBreakdown = *p.CacheTTLBreakdown
		}
		if p.FirstToken != nil {
			base.Fields.FirstToken = *p.FirstToken
		}
		if p.Duration != nil {
			base.Fields.Duration = *p.Duration
		}
		if p.Speed != nil {
			base.Fields.Speed = *p.Speed
		}
	}
	if patch.TokenUnit != nil {
		base.TokenUnit = *patch.TokenUnit
	}
	if patch.TokenDecimals != nil {
		base.TokenDecimals = *patch.TokenDecimals
	}
	if patch.DurationUnit != nil {
		base.DurationUnit = *patch.DurationUnit
	}
	if patch.DurationDecimals != nil {
		base.DurationDecimals = *patch.DurationDecimals
	}
	if patch.SpeedFormula != nil {
		base.SpeedFormula = *patch.SpeedFormula
	}
	if patch.SpeedDecimals != nil {
		base.SpeedDecimals = *patch.SpeedDecimals
	}
	if patch.ColorEnabled != nil {
		base.ColorEnabled = *patch.ColorEnabled
	}
	if patch.FirstTokenThresholds != nil {
		p := patch.FirstTokenThresholds
		if p.Warn != nil {
			base.FirstTokenThresholds.Warn = *p.Warn
		}
		if p.Slow != nil {
			base.FirstTokenThresholds.Slow = *p.Slow
		}
		if p.Critical != nil {
			base.FirstTokenThresholds.Critical = *p.Critical
		}
	}
	if patch.DurationThresholds != nil {
		p := patch.DurationThresholds
		if p.Warn != nil {
			base.DurationThresholds.Warn = *p.Warn
		}
		if p.Slow != nil {
			base.DurationThresholds.Slow = *p.Slow
		}
		if p.Critical != nil {
			base.DurationThresholds.Critical = *p.Critical
		}
	}
	if patch.SpeedLowThreshold != nil {
		base.SpeedLowThreshold = *patch.SpeedLowThreshold
	}
	return base
}

func (s *SettingService) UpdateUsageDisplayConfig(ctx context.Context, patch UsageDisplayConfigPatch) error {
	base, err := s.GetUsageDisplayConfig(ctx)
	if err != nil {
		return err
	}
	cfg := mergeUsageDisplayConfig(base, patch)
	if err := validateUsageDisplayConfig(cfg); err != nil {
		return err
	}
	value, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := s.settingRepo.Set(ctx, SettingKeyUsageDisplayConfig, string(value)); err != nil {
		return err
	}
	if s.onUpdate != nil {
		s.onUpdate()
	}
	return nil
}
