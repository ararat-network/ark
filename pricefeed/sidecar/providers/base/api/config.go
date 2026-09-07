package api

import (
	"errors"
	"slices"
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

// Config defines a config for an API based data provider.
//
// Config values are treated as immutable after being passed to NewFetcher.
// Build a replacement config instead of mutating Endpoints in place.
type Config struct {
	// Name is the name of the Fetcher that corresponds to this config.
	Name string `mapstructure:"name"`

	// Timeout is the request timeout.
	Timeout time.Duration `mapstructure:"timeout"`

	// Interval is the delay between polling cycles.
	Interval time.Duration `mapstructure:"interval"`

	// RequestsPerSecond limits request rate. If zero, requests are not rate limited.
	RequestsPerSecond int `mapstructure:"requests_per_second"`

	// Endpoints is a list of endpoints that the provider can query.
	Endpoints []types.Endpoint `mapstructure:"endpoints"`

	// BatchSize is the maximum number of tickers in each provider-defined request
	// group. If zero, each complete provider-defined group is queried in one request.
	BatchSize int `mapstructure:"batch_size"`

	// MaxBlockHeightAge is the maximum time an on-chain data source may report the
	// same block height before its data is considered stale. If zero, block height
	// freshness is not checked by the provider-specific fetcher.
	MaxBlockHeightAge time.Duration `mapstructure:"max_block_height_age"`
}

// Validate performs validation of the API config.
func (c *Config) Validate() error {
	if len(c.Name) == 0 {
		return errors.New("fetcher name cannot be empty")
	}

	if len(c.Endpoints) == 0 {
		return errors.New("endpoints cannot be empty")
	}

	for _, e := range c.Endpoints {
		if err := e.Validate(); err != nil {
			return err
		}
	}

	if c.Timeout <= 0 {
		return errors.New("timeout must be greater than 0")
	}
	if c.Interval <= 0 {
		return errors.New("interval must be greater than 0")
	}
	if c.RequestsPerSecond < 0 {
		return errors.New("requests per second cannot be negative")
	}
	if c.BatchSize < 0 {
		return errors.New("batch size cannot be negative")
	}
	if c.MaxBlockHeightAge < 0 {
		return errors.New("max block height age cannot be negative")
	}

	return nil
}

// Equal reports whether two API configs are equivalent.
func (c Config) Equal(other Config) bool {
	return c.Name == other.Name &&
		c.Timeout == other.Timeout &&
		c.Interval == other.Interval &&
		c.RequestsPerSecond == other.RequestsPerSecond &&
		slices.Equal(c.Endpoints, other.Endpoints) &&
		c.BatchSize == other.BatchSize &&
		c.MaxBlockHeightAge == other.MaxBlockHeightAge
}
