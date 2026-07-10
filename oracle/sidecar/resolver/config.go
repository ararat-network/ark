package resolver

import (
	"fmt"
	"strings"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/oracle/sidecar/types"
)

// Config defines optional routes for resolving provider pair medians into
// vote-target pair prices.
type Config struct {
	// Routes maps vote-target denoms to alternate resolution paths. Missing or
	// empty entries use the canonical direct NOAH/QUOTE route.
	Routes map[string][]Route `json:"routes"`

	// BootstrapPrices supplies temporary route-leg prices when no provider has
	// a fresh direct or inverse observation. Provider observations always take
	// precedence, and each bootstrap price expires at its absolute deadline.
	BootstrapPrices []BootstrapPrice `json:"bootstrapPrices"`
}

// Route is one named path from NOAH to a vote-target quote denom.
type Route struct {
	// Name identifies this path in per-route metrics.
	Name string `json:"name"`

	// Pairs are multiplied in order. A step may be satisfied by provider data
	// for either the configured pair or its inverse.
	Pairs []types.Pair `json:"pairs"`
}

// BootstrapPrice is an expiring, last-resort price for one canonical route
// pair. Price is a decimal string so operator configuration does not lose
// precision through float decoding.
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

// MarketPairs returns provider market pairs required to resolve active denoms,
// including inverse pairs that can satisfy the same steps. Denoms without
// configured routes use the default direct NOAH/QUOTE path.
func (c Config) MarketPairs(denoms []string) map[types.Pair]struct{} {
	pairs := make(map[types.Pair]struct{})
	for _, denom := range denoms {
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
// or empty configured routes fall back to the direct NOAH/QUOTE path.
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
// nil or empty Routes map uses default direct routes for active denoms. A denom
// present in Routes with an empty route list also uses the default direct route.
// Non-empty routes must define valid paths to their canonical NOAH/QUOTE outputs.
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
		if err := sdk.ValidateDenom(denom); err != nil {
			return fmt.Errorf("invalid denom %q: %w", denom, err)
		}
		if _, err := types.FromDenom(denom); err != nil {
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
// denom's canonical NOAH/QUOTE pair.
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
