package runtime

import (
	"errors"
	"fmt"
	"time"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pricefeed/sidecar/chainstate"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers"
	providertypes "github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	"github.com/ararat-network/ark/pricefeed/sidecar/resolver"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// Config defines the price runtime configuration. The runtime is configured
// with price providers and a fallback feed set used until the on-chain feed
// registry is available.
//
// Config values are treated as immutable after being passed to the runtime. Build
// a replacement config instead of mutating nested maps or slices in place.
type Config struct {
	// UpdateInterval is the interval at which cached provider prices are resolved
	// into a new public price snapshot.
	UpdateInterval time.Duration `json:"updateInterval"`

	// Providers is the set of providers that the oracle will fetch prices from, keyed by provider name.
	Providers map[string]providers.Config `json:"providers"`

	// Resolver configures how provider pair prices are resolved into final feed prices.
	Resolver resolver.Config `json:"resolver"`

	// Client configures the chainstate feed query client.
	Client chainstate.Config `json:"client"`

	// FallbackFeeds is used until feed polling produces its first on-chain
	// snapshot. Later polling failures preserve the last on-chain snapshot instead
	// of returning to these defaults.
	FallbackFeeds []string `json:"fallbackFeeds"`
}

// Clone returns a runtime-owned copy of c, including nested maps and slices.
func (c Config) Clone() Config {
	cloned := c
	if c.Providers != nil {
		cloned.Providers = make(map[string]providers.Config, len(c.Providers))
		for name, providerCfg := range c.Providers {
			providerCfg.Markets = append(providertypes.Markets(nil), providerCfg.Markets...)
			providerCfg.API.Endpoints = append([]providertypes.Endpoint(nil), providerCfg.API.Endpoints...)
			providerCfg.WebSocket.Endpoints = append([]providertypes.Endpoint(nil), providerCfg.WebSocket.Endpoints...)
			cloned.Providers[name] = providerCfg
		}
	}
	cloned.Resolver = c.Resolver.Clone()
	cloned.FallbackFeeds = append([]string(nil), c.FallbackFeeds...)

	return cloned
}

// Validate performs basic validation on the runtime config.
func (c *Config) Validate() error {
	if c.UpdateInterval <= 0 {
		return errors.New("oracle update interval must be greater than 0")
	}
	if len(c.Providers) == 0 {
		return errors.New("oracle needs at least one provider")
	}
	for name, p := range c.Providers {
		if name != p.Name {
			return fmt.Errorf("provider map key %q must match provider name %q", name, p.Name)
		}
		if err := p.Validate(); err != nil {
			return fmt.Errorf("provider config is invalid: %w", err)
		}
	}
	if err := c.Resolver.Validate(); err != nil {
		return fmt.Errorf("resolver config is invalid: %w", err)
	}
	if err := c.Client.Validate(); err != nil {
		return fmt.Errorf("client config is invalid: %w", err)
	}
	if len(c.FallbackFeeds) == 0 {
		return errors.New("oracle feeds fallback cannot be empty")
	}
	if len(c.FallbackFeeds) > oracletypes.MaxFeeds {
		return fmt.Errorf(
			"oracle fallback feed count %d exceeds maximum feeds %d",
			len(c.FallbackFeeds),
			oracletypes.MaxFeeds,
		)
	}
	fallbackFeeds := make(map[string]struct{}, len(c.FallbackFeeds))
	for _, denom := range c.FallbackFeeds {
		if err := chain.ValidatePricedDenom(denom); err != nil {
			return fmt.Errorf("invalid fallback feed %q: %w", denom, err)
		}
		if _, ok := fallbackFeeds[denom]; ok {
			return fmt.Errorf("duplicate fallback feed %q", denom)
		}
		fallbackFeeds[denom] = struct{}{}
	}

	return nil
}
