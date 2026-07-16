package service

import (
	"context"
	"encoding/json"

	"github.com/omni/bugtracker/internal/config"
)

// SettingArchiveAutoAfterDays is the app_settings key for the auto-archive window.
const SettingArchiveAutoAfterDays = "archive.auto_after_days"

type archiveSetting struct {
	AutoAfterDays int `json:"auto_after_days"`
}

// EffectiveArchiveDays returns the active auto-archive window: the DB setting when present,
// otherwise the bootstrap config default (so existing env/config setups keep working and
// Settings overrides them). 0 or negative means auto-archive is off.
func EffectiveArchiveDays(ctx context.Context, repo Repository, cfg *config.Config) (int, error) {
	raw, err := repo.GetSetting(ctx, SettingArchiveAutoAfterDays)
	if err != nil {
		return 0, err
	}
	if raw == nil {
		if cfg != nil {
			return cfg.Archive.AutoAfterDays, nil
		}
		return 0, nil
	}
	var s archiveSetting
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, err
	}
	return s.AutoAfterDays, nil
}

// SetArchiveDays persists the auto-archive window (0 disables it).
func SetArchiveDays(ctx context.Context, repo Repository, days int) error {
	raw, _ := json.Marshal(archiveSetting{AutoAfterDays: days})
	return repo.SetSetting(ctx, SettingArchiveAutoAfterDays, raw)
}
