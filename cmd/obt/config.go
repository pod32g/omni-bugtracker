package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config is one host's connection details, resolved from (in decreasing precedence)
// flags, environment, and ~/.config/obt/config.toml.
type Config struct {
	Server  string
	Token   string
	Project string
	// Host is the config-file section this came from, for error messages.
	Host string
}

// configPath honours XDG_CONFIG_HOME, then falls back to ~/.config.
func configPath() string {
	if p := os.Getenv("OBT_CONFIG"); p != "" {
		return p
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "obt", "config.toml")
}

// LoadConfig resolves the effective configuration.
//
// Environment beats the file so a CI job can point at another instance without
// rewriting anybody's config, and the token can come from a secret rather than a file
// on disk — which is the only sensible place for it in CI.
func LoadConfig(host string) (Config, error) {
	cfg := Config{Host: host}

	if path := configPath(); path != "" {
		file, err := parseConfigFile(path)
		if err != nil {
			return cfg, err
		}
		if host == "" {
			host = file.defaultHost
			cfg.Host = host
		}
		if section, ok := file.hosts[host]; ok {
			cfg.Server, cfg.Token, cfg.Project = section["server"], section["token"], section["project"]
		} else if host != "" && len(file.hosts) > 0 {
			return cfg, fmt.Errorf("no host %q in %s (known: %s)", host, path, strings.Join(file.hostNames(), ", "))
		}
	}

	if v := os.Getenv("OBT_SERVER"); v != "" {
		cfg.Server = v
	}
	if v := os.Getenv("OBT_TOKEN"); v != "" {
		cfg.Token = v
	}
	if v := os.Getenv("OBT_PROJECT"); v != "" {
		cfg.Project = v
	}
	cfg.Server = strings.TrimRight(cfg.Server, "/")
	return cfg, nil
}

type configFile struct {
	defaultHost string
	// hosts maps a section name to its keys.
	hosts map[string]map[string]string
}

func (c configFile) hostNames() []string {
	names := make([]string, 0, len(c.hosts))
	for name := range c.hosts {
		names = append(names, name)
	}
	return names
}

// parseConfigFile reads the subset of TOML this config needs: top-level keys, and
// `[hosts.<name>]` tables of string values.
//
// Deliberately not a TOML library. The only offline-available version in this module's
// cache would have bumped an unrelated indirect dependency, and the file this reads is
// three keys and a table header — a dependency bump is a worse trade than forty lines.
// Anything outside that subset is reported rather than silently ignored, so a config
// that looks valid and does nothing is not a possible outcome.
func parseConfigFile(path string) (configFile, error) {
	out := configFile{hosts: map[string]map[string]string{}}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil // no config is fine; flags and env may be enough
		}
		return out, err
	}

	section := ""
	for n, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") {
				return out, fmt.Errorf("%s:%d: unterminated table header", path, n+1)
			}
			name := strings.TrimSpace(line[1 : len(line)-1])
			rest, ok := strings.CutPrefix(name, "hosts.")
			if !ok {
				return out, fmt.Errorf("%s:%d: only [hosts.<name>] tables are supported, got [%s]", path, n+1, name)
			}
			section = strings.Trim(strings.TrimSpace(rest), `"`)
			if _, exists := out.hosts[section]; !exists {
				out.hosts[section] = map[string]string{}
			}
			// The first host declared is the default, so a single-host config needs
			// no `default` key at all.
			if out.defaultHost == "" {
				out.defaultHost = section
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return out, fmt.Errorf("%s:%d: expected key = \"value\"", path, n+1)
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"`)
		if section == "" {
			if key == "default" {
				out.defaultHost = value
			}
			continue
		}
		out.hosts[section][key] = value
	}
	// An explicit `default` before any table still wins.
	return out, nil
}
