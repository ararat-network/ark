// Package config loads, writes, and validates oracle runtime configuration
// files.
package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"

	"github.com/ararat-network/ark/pricefeed/sidecar/runtime"
)

// Load reads, decodes, and validates the runtime config at path. The file is
// TOML whatever its extension.
func Load(path string) (runtime.Config, error) {
	var cfg runtime.Config
	if strings.TrimSpace(path) == "" {
		return cfg, errors.New("oracle config path cannot be empty")
	}

	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("toml")
	if err := v.ReadInConfig(); err != nil {
		return cfg, fmt.Errorf("reading oracle config: %w", err)
	}
	// A key the struct does not know is a misspelled knob in a hand-edited
	// file, not a default to fall back on.
	if err := v.Unmarshal(&cfg, func(dc *mapstructure.DecoderConfig) { dc.ErrorUnused = true }); err != nil {
		return cfg, fmt.Errorf("decoding oracle config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}

	return cfg, nil
}
