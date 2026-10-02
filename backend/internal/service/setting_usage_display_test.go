//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type usageDisplaySettingRepoStub struct {
	value    string
	getErr   error
	setErr   error
	setKey   string
	setValue string
}

func (r *usageDisplaySettingRepoStub) Get(context.Context, string) (*Setting, error) {
	return nil, r.getErr
}
func (r *usageDisplaySettingRepoStub) GetValue(context.Context, string) (string, error) {
	if r.getErr != nil {
		return "", r.getErr
	}
	return r.value, nil
}
func (r *usageDisplaySettingRepoStub) Set(_ context.Context, key, value string) error {
	r.setKey, r.setValue = key, value
	return r.setErr
}
func (r *usageDisplaySettingRepoStub) GetMultiple(context.Context, []string) (map[string]string, error) {
	return nil, errors.New("unexpected GetMultiple")
}
func (r *usageDisplaySettingRepoStub) SetMultiple(context.Context, map[string]string) error {
	return errors.New("unexpected SetMultiple")
}
func (r *usageDisplaySettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	return nil, errors.New("unexpected GetAll")
}
func (r *usageDisplaySettingRepoStub) Delete(context.Context, string) error {
	return errors.New("unexpected Delete")
}

func TestUsageDisplayConfigMissingAndCorruptUseSafeDefaults(t *testing.T) {
	for name, raw := range map[string]string{"missing": "", "corrupt": "{"} {
		t.Run(name, func(t *testing.T) {
			repo := &usageDisplaySettingRepoStub{value: raw, getErr: nil}
			if name == "missing" {
				repo.getErr = ErrSettingNotFound
			}
			got, err := NewSettingService(repo, &config.Config{}).GetUsageDisplayConfig(context.Background())
			require.NoError(t, err)
			require.Equal(t, DefaultUsageDisplayConfig(), got)
		})
	}
}

func TestUsageDisplayConfigUpdatePreservesExplicitFalseAndPersists(t *testing.T) {
	repo := &usageDisplaySettingRepoStub{getErr: ErrSettingNotFound}
	var callbacks int
	svc := NewSettingService(repo, &config.Config{})
	svc.SetOnUpdateCallback(func() { callbacks++ })

	falseValue := false
	err := svc.UpdateUsageDisplayConfig(context.Background(), UsageDisplayConfigPatch{
		Fields: &UsageDisplayFieldsPatch{Speed: &falseValue},
	})
	require.NoError(t, err)
	require.Equal(t, SettingKeyUsageDisplayConfig, repo.setKey)
	require.Contains(t, repo.setValue, `"speed":false`)
	require.Equal(t, 1, callbacks)
}

func TestUsageDisplayConfigRejectsInvalidFieldsBeforePersistence(t *testing.T) {
	repo := &usageDisplaySettingRepoStub{getErr: ErrSettingNotFound}
	bad := "invalid"
	err := NewSettingService(repo, &config.Config{}).UpdateUsageDisplayConfig(context.Background(), UsageDisplayConfigPatch{TokenUnit: &bad})
	require.Error(t, err)
	require.Equal(t, "", repo.setKey)
}

func TestUsageDisplayConfigReturnsPersistenceError(t *testing.T) {
	repo := &usageDisplaySettingRepoStub{getErr: ErrSettingNotFound, setErr: errors.New("db down")}
	err := NewSettingService(repo, &config.Config{}).UpdateUsageDisplayConfig(context.Background(), UsageDisplayConfigPatch{})
	require.ErrorIs(t, err, repo.setErr)
}
