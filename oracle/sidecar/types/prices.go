package types

import (
	"fmt"
	"math/big"
)

// Prices is the oracle-internal price map keyed by canonical BASE/QUOTE pairs.
type Prices map[Pair]*big.Float

// DenomPrices is the public/API-facing price map keyed by vote-target denom.
type DenomPrices map[string]*big.Float

// PricesByDenom projects internal pair prices to public vote-target denom prices.
func PricesByDenom(prices Prices, denoms []string) DenomPrices {
	result := make(DenomPrices, len(prices))
	targets := make(map[string]struct{}, len(denoms))
	for _, denom := range denoms {
		targets[denom] = struct{}{}
	}

	for pair, price := range prices {
		if price == nil {
			continue
		}

		denom := pair.VoteTargetDenom()
		if len(targets) != 0 {
			if _, ok := targets[denom]; !ok {
				continue
			}
		}

		result[denom] = new(big.Float).Copy(price)
	}

	return result
}

// ParsePrice parses a decimal string into an oracle price.
func ParsePrice(s string) (*big.Float, error) {
	price, ok := new(big.Float).SetString(s)
	if !ok {
		return nil, fmt.Errorf("failed to parse oracle price %q", s)
	}

	return price, nil
}
