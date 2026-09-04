// Package config loads and validates the TOML configuration for bookmark-sync.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config is the root configuration structure.
type Config struct {
	Linkding LinkdingConfig `toml:"linkding"`
	LLM      LLMConfig      `toml:"llm"`
	Sync     SyncConfig     `toml:"sync"`
	Filter   FilterConfig   `toml:"filter"`
}

// LinkdingConfig holds the connection settings for the linkding instance.
type LinkdingConfig struct {
	BaseURL  string `toml:"base_url"`
	APIToken string `toml:"api_token"`
}

// ProviderConfig represents a single LLM provider (OpenAI-compatible).
type ProviderConfig struct {
	BaseURL string `toml:"base_url"` // e.g. http://127.0.0.1:8080/v1
	APIKey  string `toml:"api_key"`  // may be empty for local llama.cpp
	Model   string `toml:"model"`    // e.g. llama-3.1-8b, gpt-4o-mini
}

// LLMConfig holds per-function provider assignments and prompts.
// Enabled is opt-in: default is false. When disabled, LLM calls are
// skipped entirely (title + URL + inbox tag only).
type LLMConfig struct {
	Enabled         bool           `toml:"enabled"` // default: false (opt-in)
	Summarize       ProviderConfig `toml:"summarize"`
	Tag             ProviderConfig `toml:"tag"`
	SummarizePrompt string         `toml:"summarize_prompt"`
	TagPrompt       string         `toml:"tag_prompt"`
}

// SyncConfig holds settings for the Sync pipeline.
type SyncConfig struct {
	FirefoxSyncCLI string `toml:"firefox_sync_cli"` // path to firefox-sync-cli binary
}

// FilterConfig holds filtering rules.
type FilterConfig struct {
	BlockedDomains []string `toml:"blocked_domains"`
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Linkding: LinkdingConfig{
			BaseURL: "http://localhost:9090",
		},
		LLM: LLMConfig{
			Enabled: false,
			Summarize: ProviderConfig{
				BaseURL: "http://127.0.0.1:8080/v1",
				Model:   "llama-3.1-8b",
			},
			Tag: ProviderConfig{
				BaseURL: "http://127.0.0.1:8080/v1",
				Model:   "llama-3.1-8b",
			},
			SummarizePrompt: `You are a helpful assistant. Summarize the following web page in one or two sentences in Japanese. Focus on what makes it worth bookmarking.

Title: {{.Title}}
URL: {{.URL}}

Summary:`,
			TagPrompt: `You are a helpful assistant. Generate 2-5 relevant tags for the following web page. Tags should be single words or short phrases in Japanese. Return them as a comma-separated list.

Title: {{.Title}}
URL: {{.URL}}
Summary: {{.Summary}}

Tags:`,
		},
		Sync: SyncConfig{
			FirefoxSyncCLI: "/home/bookmark-sync/bin/ffsclient",
		},
		Filter: FilterConfig{
			BlockedDomains: []string{},
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