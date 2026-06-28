package base

import (
	"fmt"

	"noah/oracle/providers/types"
)

// Config describes provider-level identity and market mapping.
//
// Transport-specific settings belong to the fetcher config. This config only
// contains fields the base Provider consumes directly: labels for logs and
// metrics, the active provider type, and denom-to-market resolution.
type Config struct {
	// Name is the provider name used in logs and metrics.
	Name string `json:"name"`
	// Type identifies the transport backing the provider.
	Type TransportType `json:"type"`
	// Markets maps chain denoms to provider-specific symbols.
	Markets types.Markets `json:"markets"`
}

// TransportType identifies the transport used by a provider fetcher.
type TransportType string

const (
	// WebSocket identifies a websocket provider fetcher.
	WebSocket TransportType = "websocket"
	// API identifies an HTTP API provider fetcher.
	API TransportType = "api"
)

// Clone returns a copy of the provider config that does not share mutable slice state.
func (c Config) Clone() Config {
	c.Markets = append(types.Markets(nil), c.Markets...)
	return c
}

// Validate checks the provider identity and market mapping are usable.
func (c *Config) Validate() error {
	if len(c.Name) == 0 {
		return fmt.Errorf("provider name cannot be empty")
	}

	if len(c.Type) == 0 {
		return fmt.Errorf("provider type cannot be empty")
	}

	if err := c.Markets.Validate(); err != nil {
		return err
	}

	return nil
}
