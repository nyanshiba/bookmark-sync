package config

import (
	"testing"

	"github.com/BurntSushi/toml"
)

// TestDefaultLLMDisabled verifies the safe default: LLM is disabled
// unless explicitly enabled in the config file.
func TestDefaultLLMDisabled(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.LLM.Enabled {
		t.Error("LLM must be disabled by default (opt-in)")
	}
}

// TestLLMEnabledOptIn verifies that a config file that omits the key
// keeps the default (false), and that an explicit enabled = true wins.
func TestLLMEnabledOptIn(t *testing.T) {
	// Omitted -> default false preserved.
	cfg := DefaultConfig()
	if err := toml.Unmarshal([]byte(""), &cfg); err != nil {
		t.Fatalf("unmarshal empty: %v", err)
	}
	if cfg.LLM.Enabled {
		t.Error("omitted enabled must stay false")
	}

	// Explicit true -> enabled.
	cfg = DefaultConfig()
	if err := toml.Unmarshal([]byte("[llm]\nenabled = true\n"), &cfg); err != nil {
		t.Fatalf("unmarshal enabled=true: %v", err)
	}
	if !cfg.LLM.Enabled {
		t.Error("explicit enabled = true must enable LLM")
	}
}
