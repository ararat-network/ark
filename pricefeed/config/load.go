// Package config loads and validates oracle runtime configuration files.
package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/viper"

	"ark/pricefeed/sidecar/runtime"
)

// Load reads, decodes, and validates the runtime config at path.
func Load(path string) (runtime.Config, error) {
	var cfg runtime.Config
	if strings.TrimSpace(path) == "" {
		return cfg, errors.New("oracle config path cannot be empty")
	}

	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return cfg, fmt.Errorf("reading oracle config: %w", err)
	}
	if err := v.Unmarshal(&cfg); err != nil {
		return cfg, fmt.Errorf("decoding oracle config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}

	return cfg, nil
}
