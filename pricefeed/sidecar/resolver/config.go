package resolver

import (
	"fmt"
	"strings"
	"time"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pricefeed/sidecar/types"
)

// Config defines optional routes for resolving provider pair medians into
// feed pair prices.
type Config struct {
	// Routes maps feed denoms to alternate resolution paths. Missing or
	// empty entries use the direct UNIT/NOAH route.
	Routes map[string][]Route `json:"routes"`

	// BootstrapPrices supplies temporary route-leg prices when no provider has
	// a fresh direct or inverse observation. Provider observations always take
	// precedence, and each bootstrap price expires at its absolute deadline.
	BootstrapPrices []BootstrapPrice `json:"bootstrapPrices"`
}

// Route is one named path from a feed's unit to NOAH. Its legs multiply to
// NOAH per one unit, the orientation the chain stores, so the resolved price
// is the published price. Venues quote whichever way they quote: a leg is
// satisfied by a provider observation in either orientation, normalised per
// sample by the resolver.
type Route struct {
	// Name identifies this path in per-route metrics.
	Name string `json:"name"`

	// Pairs are multiplied in order. A step may be satisfied by provider data
	// for either the configured pair or its inverse.
	Pairs []types.Pair `json:"pairs"`
}

// BootstrapPrice is an expiring, last-resort price for one route leg, stated
// in the leg's orientation. Price is a decimal string so operator
// configuration does not lose precision through float decoding.
type BootstrapPrice struct {
	Pair       types.Pair `json:"pair"`
	Price      string     `json:"price"`
	ValidUntil string     `json:"validUntil"`
}

// Clone returns a deep copy of c, including nested route slices.
func (c Config) Clone() Config {
	cloned := c
	if c.Routes != nil {
		cloned.Routes = make(map[string][]Route, len(c.Routes))
		for denom, routes := range c.Routes {
			if routes == nil {
				cloned.Routes[denom] = nil
				continue
			}
			copiedRoutes := make([]Route, len(routes))
			for i, route := range routes {
				route.Pairs = append([]types.Pair(nil), route.Pairs...)
				copiedRoutes[i] = route
			}
			cloned.Routes[denom] = copiedRoutes
		}
	}
	cloned.BootstrapPrices = append([]BootstrapPrice(nil), c.BootstrapPrices...)

	return cloned
}

// MarketPairs returns provider market pairs required to resolve the active feed set,
// including inverse pairs that can satisfy the same steps. Feeds without
// configured routes use the default direct UNIT/NOAH path.
func (c Config) MarketPairs(feeds []string) map[types.Pair]struct{} {
	pairs := make(map[types.Pair]struct{})
	for _, denom := range feeds {
		_, routes, ok := c.RoutesForDenom(denom)
		if !ok {
			continue
		}
		for _, route := range routes {
			for _, pair := range route.Pairs {
				pairs[pair] = struct{}{}
				pairs[pair.Inverse()] = struct{}{}
			}
		}
	}

	return pairs
}

// RoutesForDenom returns the output pair and effective routes for denom. Missing
// or empty configured routes fall back to the direct UNIT/NOAH path.
func (c Config) RoutesForDenom(denom string) (types.Pair, []Route, bool) {
	output, err := types.FromDenom(denom)
	if err != nil {
		return "", nil, false
	}
	if routes, ok := c.Routes[denom]; ok && len(routes) > 0 {
		return output, routes, true
	}

	return output, []Route{
		{
			Name:  "direct",
			Pairs: []types.Pair{output},
		},
	}, true
}

// Validate checks bootstrap prices and resolver routes at config-update time. A
// nil or empty Routes map uses default direct routes for active feeds. A feed
// present in Routes with an empty route list also uses the default direct route.
// Non-empty routes must define valid paths to their UNIT/NOAH outputs.
func (c Config) Validate() error {
	bootstrapPairs := make(map[types.Pair]struct{}, len(c.BootstrapPrices))
	for _, bootstrap := range c.BootstrapPrices {
		if err := bootstrap.Pair.Validate(); err != nil {
			return fmt.Errorf("bootstrap price pair %q is invalid: %w", bootstrap.Pair, err)
		}
		if _, ok := bootstrapPairs[bootstrap.Pair]; ok {
			return fmt.Errorf("duplicate bootstrap price pair %q", bootstrap.Pair)
		}
		bootstrapPairs[bootstrap.Pair] = struct{}{}

		price, err := types.ParsePrice(bootstrap.Price)
		if err != nil {
			return fmt.Errorf("bootstrap price for %s is invalid: %w", bootstrap.Pair, err)
		}
		if price.Sign() != 1 {
			return fmt.Errorf("bootstrap price for %s must be positive", bootstrap.Pair)
		}
		if _, err := time.Parse(time.RFC3339, bootstrap.ValidUntil); err != nil {
			return fmt.Errorf(
				"bootstrap price for %s validUntil %q must be RFC3339: %w",
				bootstrap.Pair,
				bootstrap.ValidUntil,
				err,
			)
		}
	}

	for denom, routes := range c.Routes {
		if err := chain.ValidatePricedDenom(denom); err != nil {
			return fmt.Errorf("invalid resolver denom %q: %w", denom, err)
		}
		if len(routes) == 0 {
			continue
		}

		names := make(map[string]struct{}, len(routes))
		for routeIndex, route := range routes {
			if strings.TrimSpace(route.Name) == "" {
				return fmt.Errorf("resolver denom %q route %d route name cannot be empty", denom, routeIndex)
			}
			if strings.TrimSpace(route.Name) != route.Name {
				return fmt.Errorf("resolver denom %q route name %q contains whitespace", denom, route.Name)
			}
			if _, ok := names[route.Name]; ok {
				return fmt.Errorf("resolver denom %q has duplicate route name %q", denom, route.Name)
			}
			names[route.Name] = struct{}{}

			if len(route.Pairs) == 0 {
				return fmt.Errorf("resolver denom %q route %q must have at least one pair", denom, route.Name)
			}
			for pairIndex, pair := range route.Pairs {
				if err := pair.Validate(); err != nil {
					return fmt.Errorf("resolver denom %q route %q pair %d is invalid: %w", denom, route.Name, pairIndex, err)
				}
			}
			if err := validateRouteOutput(denom, route); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateRouteOutput ensures a route's ordered path resolves to the requested
// denom's UNIT/NOAH output pair.
func validateRouteOutput(denom string, route Route) error {
	expected, err := types.FromDenom(denom)
	if err != nil {
		return err
	}
	resolved, err := routeOutput(route.Pairs)
	if err != nil {
		return fmt.Errorf("resolver denom %q route %q is invalid: %w", denom, route.Name, err)
	}
	if resolved != expected {
		return fmt.Errorf(
			"resolver denom %q route %q resolves to %q, want %q",
			denom,
			route.Name,
			resolved,
			expected,
		)
	}

	return nil
}

// routeOutput returns the base/quote pair implied by an ordered connected path.
func routeOutput(pairs []types.Pair) (types.Pair, error) {
	if len(pairs) == 0 {
		return "", fmt.Errorf("route must have at least one pair")
	}

	base := pairs[0].Base()
	quote := pairs[0].Quote()
	for _, pair := range pairs[1:] {
		if pair.Base() != quote {
			return "", fmt.Errorf("pair %q does not connect after %q", pair, quote)
		}
		quote = pair.Quote()
	}

	return types.NewPair(base, quote)
}
