package api

import (
	"errors"
	"time"

	"noah/oracle/providers/types"
)

// Config defines a config for an API based data provider.
//
// Config values are treated as immutable after being passed to NewFetcher.
// Build a replacement config instead of mutating Endpoints in place.
type Config struct {
	// Name is the name of the Fetcher that corresponds to this config.
	Name string `json:"name"`

	// Timeout is the request timeout. If zero, requests use only the parent context deadline.
	Timeout time.Duration `json:"timeout"`

	// Interval is the delay between polling cycles. If zero, cycles run without an added delay.
	Interval time.Duration `json:"interval"`

	// RequestsPerSecond limits request rate. If zero, requests are not rate limited.
	RequestsPerSecond int `json:"requestsPerSecond"`

	// Endpoints is a list of endpoints that the provider can query.
	Endpoints []types.Endpoint `json:"endpoints"`

	// BatchSize is the maximum number of tickers to query in a single request.
	// If zero, all tickers are queried in one request.
	BatchSize int `json:"batchSize"`

	// MaxBlockHeightAge is the maximum time an on-chain data source may report the
	// same block height before its data is considered stale. If zero, block height
	// freshness is not checked by the provider-specific fetcher.
	MaxBlockHeightAge time.Duration `json:"maxBlockHeightAge"`
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

	if c.Interval < 0 {
		return errors.New("interval cannot be negative")
	}
	if c.Timeout < 0 {
		return errors.New("timeout cannot be negative")
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
