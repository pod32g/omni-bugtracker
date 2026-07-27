package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OBT_CONFIG", path)
	return path
}

func TestLoadConfigHosts(t *testing.T) {
	writeConfig(t, `
# a comment
default = "prod"

[hosts.prod]
server = "http://tracker:8092"
token = "obt_prod"
project = "BUG"

[hosts.local]
server = "http://localhost:8099"
token = "obt_local"
`)
	// Clear inherited environment so the file is what is under test.
	for _, k := range []string{"OBT_SERVER", "OBT_TOKEN", "OBT_PROJECT"} {
		t.Setenv(k, "")
	}

	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Server != "http://tracker:8092" || cfg.Token != "obt_prod" || cfg.Project != "BUG" {
		t.Errorf("default host resolved to %+v", cfg)
	}

	cfg, err = LoadConfig("local")
	if err != nil {
		t.Fatalf("load local: %v", err)
	}
	if cfg.Server != "http://localhost:8099" {
		t.Errorf("named host resolved to %+v", cfg)
	}

	// A host that is not there must say so and name what is, rather than silently
	// falling back to a different server than the one that was asked for.
	if _, err := LoadConfig("staging"); err == nil {
		t.Error("an unknown host should be an error")
	}
}

func TestLoadConfigEnvironmentWins(t *testing.T) {
	writeConfig(t, "[hosts.prod]\nserver = \"http://tracker:8092\"\ntoken = \"obt_file\"\n")
	t.Setenv("OBT_TOKEN", "obt_env")
	t.Setenv("OBT_SERVER", "http://other:9000/")
	t.Setenv("OBT_PROJECT", "")

	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Token != "obt_env" {
		t.Errorf("token = %q; the environment must win so CI can use a secret", cfg.Token)
	}
	// The trailing slash is stripped, or every request path would double up.
	if cfg.Server != "http://other:9000" {
		t.Errorf("server = %q, want the trailing slash trimmed", cfg.Server)
	}
}

func TestLoadConfigMissingFileIsNotAnError(t *testing.T) {
	t.Setenv("OBT_CONFIG", filepath.Join(t.TempDir(), "absent.toml"))
	for _, k := range []string{"OBT_SERVER", "OBT_TOKEN", "OBT_PROJECT"} {
		t.Setenv(k, "")
	}
	cfg, err := LoadConfig("")
	if err != nil {
		t.Errorf("a missing config should not be fatal — flags and env may be enough: %v", err)
	}
	if cfg.Server != "" {
		t.Errorf("nothing configured should mean nothing set, got %+v", cfg)
	}
}

// A config that looks fine and quietly does nothing is the worst outcome, so anything
// outside the supported subset is reported.
func TestLoadConfigRejectsUnsupportedShapes(t *testing.T) {
	writeConfig(t, "[server]\nurl = \"http://x\"\n")
	if _, err := LoadConfig(""); err == nil {
		t.Error("a non-[hosts.*] table should be reported, not ignored")
	}
	writeConfig(t, "[hosts.prod\nserver = \"x\"\n")
	if _, err := LoadConfig(""); err == nil {
		t.Error("an unterminated table header should be reported")
	}
}
