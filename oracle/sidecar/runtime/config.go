package runtime

import (
	"errors"
	"fmt"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"noah/oracle/sidecar/providers"
	"noah/oracle/sidecar/resolver"
)

// Config defines the price runtime configuration. The runtime is configured
// with price providers and a fallback denom set used until on-chain vote
// targets are available.
//
// Config values are treated as immutable after being passed to the runtime. Build
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

	// Resolver configures how provider pair prices are resolved into final vote-target denom prices.
	Resolver resolver.Config `json:"resolver"`

	// FallbackDenoms is used when vote-target polling has not produced an on-chain snapshot.
	FallbackDenoms []string `json:"fallbackDenoms"`

	// AbstainDenoms are active vote-target denoms to submit as explicit zero-rate abstains.
	AbstainDenoms []string `json:"abstainDenoms"`
}

// Validate performs basic validation on the runtime config.
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
			return fmt.Errorf("provider config is invalid: %w", err)
		}
	}
	if err := c.Resolver.Validate(); err != nil {
		return fmt.Errorf("resolver config is invalid: %w", err)
	}
	if len(c.FallbackDenoms) == 0 {
		return errors.New("oracle denoms fallback cannot be empty")
	}
	fallbackDenoms := make(map[string]struct{}, len(c.FallbackDenoms))
	for _, denom := range c.FallbackDenoms {
		if err := sdk.ValidateDenom(denom); err != nil {
			return fmt.Errorf("invalid fallback denom %q: %w", denom, err)
		}
		if _, ok := fallbackDenoms[denom]; ok {
			return fmt.Errorf("duplicate fallback denom %q", denom)
		}
		fallbackDenoms[denom] = struct{}{}
	}

	abstainDenoms := make(map[string]struct{}, len(c.AbstainDenoms))
	for _, denom := range c.AbstainDenoms {
		if err := sdk.ValidateDenom(denom); err != nil {
			return fmt.Errorf("invalid abstain denom %q: %w", denom, err)
		}
		if _, ok := abstainDenoms[denom]; ok {
			return fmt.Errorf("duplicate abstain denom %q", denom)
		}
		abstainDenoms[denom] = struct{}{}
	}

	return nil
}
