package providers

import (
	"errors"

	"noah/oracle/providers/base"
	"noah/oracle/providers/base/api"
	"noah/oracle/providers/base/websocket"
	"noah/oracle/providers/types"
)

// Config describes a provider and the transport config used to build it.
//
// Config values are treated as immutable after being passed to the oracle.
// Runtime config changes should build a replacement provider.
type Config struct {
	// Name is the provider name used in logs and metrics.
	Name string `json:"name"`
	// Type identifies the transport backing the provider.
	Type base.TransportType `json:"type"`
	// Markets maps chain denoms to provider-specific symbols.
	Markets types.Markets `json:"markets"`

	// API configures an HTTP API provider when Type is base.API.
	API api.Config `json:"api"`
	// WebSocket configures a websocket provider when Type is base.WebSocket.
	WebSocket websocket.Config `json:"websocket"`
}

// Validate checks the provider identity, market mapping, and selected transport
// config are usable.
func (c *Config) Validate() error {
	if len(c.Name) == 0 {
		return errors.New("provider name cannot be empty")
	}

	if len(c.Type) == 0 {
		return errors.New("provider type cannot be empty")
	}

	if err := c.Markets.Validate(); err != nil {
		return err
	}

	switch c.Type {
	case base.API:
		if c.API.Name != c.Name {
			return errors.New("mismatched provider and API config name")
		}
		if err := c.API.Validate(); err != nil {
			return err
		}
	case base.WebSocket:
		if c.WebSocket.Name != c.Name {
			return errors.New("mismatched provider and websocket config name")
		}
		if err := c.WebSocket.Validate(); err != nil {
			return err
		}
	default:
		return errors.New("invalid provider type")
	}

	return nil
}
