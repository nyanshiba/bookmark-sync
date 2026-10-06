package config

import (
	"testing"

	"github.com/BurntSushi/toml"
)

// TestDefaultClefDisabled verifies the safe default: classification is
// disabled unless explicitly enabled in the config file.
func TestDefaultClefDisabled(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Clef.Enabled {
		t.Error("Clef must be disabled by default (opt-in)")
	}
}

// TestClefEnabledOptIn verifies that a config file that omits the key
// keeps the default (false), and that an explicit enabled = true wins.
func TestClefEnabledOptIn(t *testing.T) {
	// Omitted -> default false preserved.
	cfg := DefaultConfig()
	if err := toml.Unmarshal([]byte(""), &cfg); err != nil {
		t.Fatalf("unmarshal empty: %v", err)
	}
	if cfg.Clef.Enabled {
		t.Error("omitted enabled must stay false")
	}

	// Explicit true -> enabled.
	cfg = DefaultConfig()
	if err := toml.Unmarshal([]byte("[clef]\nenabled = true\n"), &cfg); err != nil {
		t.Fatalf("unmarshal enabled=true: %v", err)
	}
	if !cfg.Clef.Enabled {
		t.Error("explicit enabled = true must enable Clef")
	}
}
