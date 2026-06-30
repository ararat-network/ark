package oracle

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/viper"

	"noah/oracle/providers"
)

// Config is the top-level runtime config for the oracle sidecar. The
// oracle is configured via a set of data providers (i.e. coinbase, binance, etc.) and a set
// of denoms (i.e. uusd, ukrw, etc.). The oracle will fetch prices from the
// data providers for the currency pairs at the specified update interval.
//
// Config values are treated as immutable after being passed to the oracle. Build
// a replacement config instead of mutating nested maps or slices in place.
type Config struct {
	// UpdateInterval is the interval at which the oracle will fetch prices from providers.
	UpdateInterval time.Duration `json:"updateInterval"`

	// MaxPriceAge is the maximum age of a price that the oracle will consider valid. If a
	// price is older than this, the oracle will not consider it valid and will not return it in /prices
	// requests.
	MaxPriceAge time.Duration `json:"maxPriceAge"`

	// Providers is the set of providers that the oracle will fetch prices from, keyed by provider name.
	Providers map[string]providers.Config `json:"providers"`

	// Host is the host that the oracle will listen on.
	Host string `json:"host"`

	// Port is the port that the oracle will listen on.
	Port string `json:"port"`

	// Denoms is the list of denoms to fetch prices for.
	Denoms []string `json:"denoms"`
}

// Validate performs basic validation on the oracle config.
func (c *Config) Validate() error {
	if c.UpdateInterval <= 0 {
		return errors.New("oracle update interval must be greater than 0")
	}
	if c.MaxPriceAge <= 0 {
		return errors.New("oracle max price age must be greater than 0")
	}
	for name, p := range c.Providers {
		if name != p.Name {
			return fmt.Errorf("provider map key %q must match provider name %q", name, p.Name)
		}
		if err := p.Validate(); err != nil {
			return fmt.Errorf("provider is not formatted correctly: %w", err)
		}
	}
	if len(c.Host) == 0 {
		return errors.New("oracle host cannot be empty")
	}
	if len(c.Port) == 0 {
		return errors.New("oracle port cannot be empty")
	}
	if len(c.Denoms) == 0 {
		return errors.New("oracle denoms cannot be empty")
	}

	return nil
}

// ReadOracleConfigFromFile reads a config from a file and returns the config.
func ReadOracleConfigFromFile(path string) (Config, error) {
	viper.SetConfigFile(path)
	viper.SetConfigType("json")

	if err := viper.ReadInConfig(); err != nil {
		return Config{}, err
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return Config{}, err
	}

	if err := config.Validate(); err != nil {
		return Config{}, err
	}

	return config, nil
}
