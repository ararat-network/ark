package providers

import (
	"errors"
	"time"

	"ark/pricefeed/sidecar/providers/base"
	"ark/pricefeed/sidecar/providers/base/api"
	"ark/pricefeed/sidecar/providers/base/websocket"
	"ark/pricefeed/sidecar/providers/types"
)

// Config describes a provider and the transport config used to build it.
//
// Config values are treated as immutable after being passed to the oracle.
// Runtime config changes should build a replacement provider.
type Config struct {
	// Name is the provider name used in logs and metrics.
	Name string `json:"name"`
	// TransportType identifies the transport backing the provider.
	TransportType base.TransportType `json:"transportType"`
	// Markets maps canonical oracle pairs to provider-specific symbols.
	Markets types.Markets `json:"markets"`
	// MaxPriceAge is the maximum age of a cached price accepted from this provider.
	MaxPriceAge time.Duration `json:"maxPriceAge"`

	// API configures an HTTP API provider when TransportType is base.API.
	API api.Config `json:"api"`
	// WebSocket configures a websocket provider when TransportType is base.WebSocket.
	WebSocket websocket.Config `json:"websocket"`
}

// Validate checks the provider identity, market mapping, and selected transport
// config are usable.
func (c *Config) Validate() error {
	if len(c.Name) == 0 {
		return errors.New("provider name cannot be empty")
	}
	if len(c.TransportType) == 0 {
		return errors.New("provider transport type cannot be empty")
	}
	if c.MaxPriceAge <= 0 {
		return errors.New("provider max price age must be greater than 0")
	}

	if err := c.Markets.Validate(); err != nil {
		return err
	}

	switch c.TransportType {
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
		return errors.New("invalid provider transport type")
	}

	return nil
}

// Equal reports whether two configs can use the same runtime provider.
//
// Markets and MaxPriceAge are intentionally excluded because both are runtime
// policy changes that do not require rebuilding the provider transport.
func (c Config) Equal(other Config) bool {
	if c.Name != other.Name || c.TransportType != other.TransportType {
		return false
	}

	switch c.TransportType {
	case base.API:
		return c.API.Equal(other.API)
	case base.WebSocket:
		return c.WebSocket.Equal(other.WebSocket)
	default:
		return false
	}
}
