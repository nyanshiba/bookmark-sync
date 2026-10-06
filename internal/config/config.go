// Package config loads and validates the TOML configuration for bookmark-sync.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"bookmark-sync/internal/expand"
)

// Config is the root configuration structure.
type Config struct {
	Linkding LinkdingConfig `toml:"linkding"`
	Clef     ClefConfig     `toml:"clef"`
	Sync     SyncConfig     `toml:"sync"`
	Filter   FilterConfig   `toml:"filter"`
	Expand   ExpandConfig   `toml:"expand"`
}

// LinkdingConfig holds the connection settings for the linkding instance.
type LinkdingConfig struct {
	BaseURL  string `toml:"base_url"`
	APIToken string `toml:"api_token"`
}

// ClefConfig holds the tag classifier assignment.
// Enabled is opt-in: default is false. When disabled, classification is
// skipped entirely (title + URL + inbox tag only). The classifier never
// creates new tags: it picks the nearest one from the existing linkding tags.
type ClefConfig struct {
	Enabled      bool   `toml:"enabled"` // default: false (opt-in)
	AccountID    string `toml:"account_id"`
	APIToken     string `toml:"api_token"`
	Model        string `toml:"model"` // "clef-flash" or "clef"
	Instructions string `toml:"instructions"`
	// Daily caps shared by sync and the backfill command. A zero value
	// disables that dimension. Neuron usage is estimated before each call
	// and debited with reported input tokens after, resetting 00:00 UTC.
	DailyNeuronBudget  int `toml:"daily_neuron_budget"`
	DailyRequestBudget int `toml:"daily_request_budget"`
}

// SyncConfig holds settings for the Sync pipeline.
type SyncConfig struct {
	FirefoxSyncCLI string `toml:"firefox_sync_cli"` // path to firefox-sync-cli binary
}

// FilterConfig holds filtering rules.
type FilterConfig struct {
	BlockedDomains []string `toml:"blocked_domains"`
}

// ExpandConfig controls short-URL expansion inside bookmark titles.
// Enabled by default; only titles containing a listed shortener host
// trigger an outbound request.
type ExpandConfig struct {
	Enabled     bool     `toml:"enabled"`
	TimeoutSecs int      `toml:"timeout_secs"`
	Hosts       []string `toml:"hosts"`
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Linkding: LinkdingConfig{
			BaseURL: "http://localhost:9090",
		},
		Clef: ClefConfig{
			Enabled:            false,
			Model:              "clef-flash",
			Instructions:       `Pick the single tag whose meaning is closest to this bookmark. Answer with the nearest existing tag only.`,
			DailyNeuronBudget:  9000,
			DailyRequestBudget: 0,
		},
		Sync: SyncConfig{
			FirefoxSyncCLI: "/home/bookmark-sync/bin/ffsclient",
		},
		Filter: FilterConfig{
			BlockedDomains: []string{},
		},
		Expand: ExpandConfig{
			Enabled:     true,
			TimeoutSecs: 10,
			Hosts:       expand.DefaultHosts,
		},
	}
}

// Load reads and parses a TOML configuration file, merging with defaults.
func Load(path string) (Config, error) {
	cfg := DefaultConfig()

	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("reading config: %w", err)
	}

	if err := toml.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return cfg, fmt.Errorf("validating config: %w", err)
	}

	return cfg, nil
}

// DefaultPath returns the default config file path (~/.config/bookmark-sync/config.toml).
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "bookmark-sync", "config.toml")
}

func (c *Config) validate() error {
	if c.Linkding.BaseURL == "" {
		return fmt.Errorf("linkding.base_url is required")
	}
	if c.Linkding.APIToken == "" {
		return fmt.Errorf("linkding.api_token is required")
	}
	if c.Sync.FirefoxSyncCLI == "" {
		return fmt.Errorf("sync.firefox_sync_cli is required")
	}
	return nil
}