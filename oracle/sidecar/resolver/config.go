package resolver

import (
	"fmt"
	"slices"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"noah/oracle/sidecar/types"
)

// Config defines optional routes for resolving provider pair medians into
// vote-target pair prices.
type Config struct {
	Routes map[string][]Route `json:"routes"`
}

// Route is one named path from ARK to a vote-target quote denom.
type Route struct {
	// Name identifies this path in per-route metrics.
	Name string `json:"name"`

	// Pairs are multiplied in order. A step may be satisfied by provider data
	// for either the configured pair or its inverse.
	Pairs []types.Pair `json:"pairs"`
}

// MarketPairs returns provider market pairs required to resolve active denoms,
// including inverse pairs that can satisfy the same steps. Denoms without
// configured routes use the default direct ARK/QUOTE path.
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
// or empty configured routes fall back to the direct ARK/QUOTE path.
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

// Validate checks configured resolver routes at config-update time. A nil or
// empty Routes map uses default direct routes for active denoms. A denom present
// in Routes with an empty route list also uses the default direct route. Non-empty
// routes must define valid paths to their canonical ARK/QUOTE outputs.
func (c Config) Validate() error {
	for denom, routes := range c.Routes {
		if err := sdk.ValidateDenom(denom); err != nil {
			return fmt.Errorf("invalid denom %q: %w", denom, err)
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

// Equal reports whether two resolver configs define the same routes.
func (c Config) Equal(other Config) bool {
	if len(c.Routes) != len(other.Routes) {
		return false
	}
	for denom, routes := range c.Routes {
		otherRoutes, ok := other.Routes[denom]
		if !ok {
			return false
		}
		if len(routes) != len(otherRoutes) {
			return false
		}
		for i, route := range routes {
			otherRoute := otherRoutes[i]
			if route.Name != otherRoute.Name || !slices.Equal(route.Pairs, otherRoute.Pairs) {
				return false
			}
		}
	}

	return true
}

// validateRouteOutput ensures a route's ordered path resolves to the requested
// denom's canonical ARK/QUOTE pair.
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
