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

// ── inbound integrations ──

// SettingInboundIntegrations is the app_settings key holding runtime overrides for
// the HMAC-authenticated inbound endpoints (git / logging / metrics).
const SettingInboundIntegrations = "integrations.inbound"

// InboundSources are the inbound integrations, in the order the UI lists them.
var InboundSources = []string{"git", "logging", "metrics"}

// InboundOverride is a runtime override of one inbound integration. Both fields are
// pointers because "not set here" and "set to false/empty" are different answers: a
// nil field defers to config.yaml, a non-nil one wins over it. That distinction is
// what lets an operator rotate a secret from the UI without also pinning `enabled`.
type InboundOverride struct {
	Enabled       *bool   `json:"enabled,omitempty"`
	WebhookSecret *string `json:"webhook_secret,omitempty"`
}

// InboundSettings is the stored override set, keyed by source name.
type InboundSettings map[string]InboundOverride

// GetInboundSettings reads the stored overrides. An unset key is an empty set, not
// an error — every install starts with config.yaml alone.
func GetInboundSettings(ctx context.Context, repo Repository) (InboundSettings, error) {
	raw, err := repo.GetSetting(ctx, SettingInboundIntegrations)
	if err != nil {
		return nil, err
	}
	out := InboundSettings{}
	if raw == nil {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetInboundSettings replaces the stored override set wholesale.
func SetInboundSettings(ctx context.Context, repo Repository, s InboundSettings) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return repo.SetSetting(ctx, SettingInboundIntegrations, raw)
}

// ConfiguredInbound returns one source's bootstrap config, so callers don't have to
// switch over the config struct's field names themselves.
func ConfiguredInbound(cfg config.Integrations, source string) config.Inbound {
	switch source {
	case "git":
		return config.Inbound{Enabled: cfg.Git.Enabled, WebhookSecret: cfg.Git.WebhookSecret}
	case "logging":
		return cfg.Logging
	case "metrics":
		return cfg.Metrics
	default:
		return config.Inbound{}
	}
}

// EffectiveInbound resolves what one inbound integration is actually doing right
// now: the stored override where it has an opinion, config.yaml otherwise. The
// inbound handlers read through this on each delivery, which is what makes a secret
// rotation from Settings take effect without a restart.
func EffectiveInbound(ctx context.Context, repo Repository, cfg config.Integrations, source string) (config.Inbound, error) {
	eff := ConfiguredInbound(cfg, source)
	stored, err := GetInboundSettings(ctx, repo)
	if err != nil {
		return eff, err
	}
	ov, ok := stored[source]
	if !ok {
		return eff, nil
	}
	return ApplyInboundOverride(eff, ov), nil
}

// ApplyInboundOverride layers one stored override over the bootstrap config.
func ApplyInboundOverride(base config.Inbound, ov InboundOverride) config.Inbound {
	if ov.Enabled != nil {
		base.Enabled = *ov.Enabled
	}
	if ov.WebhookSecret != nil {
		base.WebhookSecret = *ov.WebhookSecret
	}
	return base
}
