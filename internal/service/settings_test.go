package service

import (
	"testing"

	"github.com/omni/bugtracker/internal/config"
)

func ptr[T any](v T) *T { return &v }

// The whole point of the pointer fields is that "no opinion" and "explicitly false /
// explicitly empty" are different answers. A plain struct merge would collapse them
// and quietly disable an integration the operator only meant to rotate a secret for.
func TestApplyInboundOverride(t *testing.T) {
	base := config.Inbound{Enabled: true, WebhookSecret: "from-config"}

	tests := []struct {
		name string
		ov   InboundOverride
		want config.Inbound
	}{
		{
			name: "empty override defers to config entirely",
			ov:   InboundOverride{},
			want: base,
		},
		{
			name: "secret rotation leaves enabled alone",
			ov:   InboundOverride{WebhookSecret: ptr("rotated")},
			want: config.Inbound{Enabled: true, WebhookSecret: "rotated"},
		},
		{
			name: "switching off keeps the configured secret",
			ov:   InboundOverride{Enabled: ptr(false)},
			want: config.Inbound{Enabled: false, WebhookSecret: "from-config"},
		},
		{
			name: "an explicitly empty secret wins over the configured one",
			ov:   InboundOverride{WebhookSecret: ptr("")},
			want: config.Inbound{Enabled: true, WebhookSecret: ""},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ApplyInboundOverride(base, tc.ov); got != tc.want {
				t.Errorf("ApplyInboundOverride() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestConfiguredInbound(t *testing.T) {
	cfg := config.Integrations{
		Git:     config.Git{Enabled: true, WebhookSecret: "git-secret"},
		Logging: config.Inbound{Enabled: true, WebhookSecret: "log-secret"},
		Metrics: config.Inbound{Enabled: false},
	}
	for _, tc := range []struct {
		source string
		want   config.Inbound
	}{
		{"git", config.Inbound{Enabled: true, WebhookSecret: "git-secret"}},
		{"logging", config.Inbound{Enabled: true, WebhookSecret: "log-secret"}},
		{"metrics", config.Inbound{Enabled: false}},
		{"nonsense", config.Inbound{}},
	} {
		if got := ConfiguredInbound(cfg, tc.source); got != tc.want {
			t.Errorf("ConfiguredInbound(%q) = %+v, want %+v", tc.source, got, tc.want)
		}
	}
}

func TestSecretAction(t *testing.T) {
	if got := secretAction(nil); got != "unchanged" {
		t.Errorf("omitted secret = %q, want unchanged", got)
	}
	if got := secretAction(ptr("  ")); got != "cleared" {
		t.Errorf("blank secret = %q, want cleared", got)
	}
	if got := secretAction(ptr("s3cret")); got != "set" {
		t.Errorf("supplied secret = %q, want set", got)
	}
}
