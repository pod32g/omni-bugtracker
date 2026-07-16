// Package config loads layered configuration: built-in defaults → config.yaml →
// environment (OMNI_BT_<SECTION>__<KEY>). Only bootstrap config lives here; behavioral
// config (automation rules, webhooks, integration credentials) lives in the database.
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

const envPrefix = "OMNI_BT_"

type Config struct {
	Server        Server        `koanf:"server"`
	Database      Database      `koanf:"database"`
	Redis         Redis         `koanf:"redis"`
	Worker        Worker        `koanf:"worker"`
	Identity      Identity      `koanf:"identity"`
	Integrations  Integrations  `koanf:"integrations"`
	Storage       Storage       `koanf:"storage"`
	Archive       Archive       `koanf:"archive"`
	Log           Log           `koanf:"log"`
	Observability Observability `koanf:"observability"`
}

// Archive configures automatic issue archival.
type Archive struct {
	// AutoAfterDays archives issues closed more than this many days ago, via a daily
	// job. 0 (default) disables auto-archive; manual archival is always available.
	AutoAfterDays int `koanf:"auto_after_days"`
}

// Storage configures native file storage for attachments (local disk).
type Storage struct {
	AttachmentsDir string `koanf:"attachments_dir"` // default ./data/attachments
	MaxUploadMB    int64  `koanf:"max_upload_mb"`   // default 25
}

type Server struct {
	Addr         string        `koanf:"addr"`
	BaseURL      string        `koanf:"base_url"`
	ReadTimeout  time.Duration `koanf:"read_timeout"`
	WriteTimeout time.Duration `koanf:"write_timeout"`
	CORSOrigins  []string      `koanf:"cors_origins"`
}

type Database struct {
	DSN      string `koanf:"dsn"`
	MaxConns int32  `koanf:"max_conns"`
	MinConns int32  `koanf:"min_conns"`
}

type Redis struct {
	Addr string `koanf:"addr"`
	DB   int    `koanf:"db"`
}

type Worker struct {
	Queues       map[string]int `koanf:"queues"`
	PollInterval time.Duration  `koanf:"poll_interval"`
}

type Identity struct {
	Issuer       string        `koanf:"issuer"`
	JWKSURL      string        `koanf:"jwks_url"`
	Audience     string        `koanf:"audience"`
	JWKSCacheTTL time.Duration `koanf:"jwks_cache_ttl"`

	// OIDC login (BFF) settings. The API is a confidential-less public PKCE client
	// that performs the code exchange server-side (Omni-Identity sends no CORS headers).
	ClientID      string `koanf:"client_id"`
	AuthorizeURL  string `koanf:"authorize_url"`
	TokenURL      string `koanf:"token_url"`
	UserinfoURL   string `koanf:"userinfo_url"`
	EndSessionURL string `koanf:"end_session_url"`
	RedirectURI   string `koanf:"redirect_uri"`
	Scopes        string `koanf:"scopes"`
	PostLogoutURL string `koanf:"post_logout_url"`
}

// LoginEnabled reports whether the browser OIDC login flow is fully configured.
func (i Identity) LoginEnabled() bool {
	return i.ClientID != "" && i.AuthorizeURL != "" && i.TokenURL != "" && i.RedirectURI != ""
}

type Integrations struct {
	Notify  ServiceAdapter `koanf:"notify"`
	Search  ServiceAdapter `koanf:"search"`
	Upload  ServiceAdapter `koanf:"upload"`
	Logging Inbound        `koanf:"logging"`
	Metrics Inbound        `koanf:"metrics"`
	Git     Git            `koanf:"git"`
}

type ServiceAdapter struct {
	Enabled  bool          `koanf:"enabled"`
	BaseURL  string        `koanf:"base_url"`
	Timeout  time.Duration `koanf:"timeout"`
	APIToken string        `koanf:"api_token"` // bearer token, if the service requires auth
}

type Inbound struct {
	Enabled       bool   `koanf:"enabled"`
	WebhookSecret string `koanf:"webhook_secret"`
}

type Git struct {
	Enabled       bool     `koanf:"enabled"`
	WebhookSecret string   `koanf:"webhook_secret"`
	CloseVerbs    []string `koanf:"close_verbs"`
	LinkVerbs     []string `koanf:"link_verbs"`
	CloseOnMerge  bool     `koanf:"close_on_merge"`
}

type Log struct {
	Level  string `koanf:"level"`
	Format string `koanf:"format"`
}

type Observability struct {
	MetricsEnabled bool   `koanf:"metrics_enabled"`
	TracingEnabled bool   `koanf:"tracing_enabled"`
	OTLPEndpoint   string `koanf:"otlp_endpoint"`
}

// Load reads config.yaml (if present) then overlays environment variables.
func Load(path string) (*Config, error) {
	k := koanf.New(".")

	if path != "" {
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("load %s: %w", path, err)
		}
	}

	// OMNI_BT_SERVER__ADDR -> server.addr
	err := k.Load(env.Provider(envPrefix, ".", func(s string) string {
		s = strings.TrimPrefix(s, envPrefix)
		s = strings.ReplaceAll(s, "__", ".")
		return strings.ToLower(s)
	}), nil)
	if err != nil {
		return nil, fmt.Errorf("load env: %w", err)
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// validate fails fast on missing required bootstrap values.
func (c *Config) validate() error {
	var missing []string
	if c.Database.DSN == "" {
		missing = append(missing, "database.dsn")
	}
	if c.Identity.Issuer == "" {
		missing = append(missing, "identity.issuer")
	}
	if c.Identity.JWKSURL == "" {
		missing = append(missing, "identity.jwks_url")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required config: %s", strings.Join(missing, ", "))
	}
	return nil
}
