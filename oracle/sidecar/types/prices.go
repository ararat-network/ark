package types

import (
	"fmt"
	"math/big"
	"time"
)

// Prices is the oracle-internal price map keyed by canonical BASE/QUOTE pairs.
type Prices map[Pair]*big.Float

// DenomPrices is the public/API-facing price map keyed by vote-target denom.
type DenomPrices map[string]*big.Float

// Clone returns a deep copy of p.
func (p DenomPrices) Clone() DenomPrices {
	copied := make(DenomPrices, len(p))
	for denom, price := range p {
		if price == nil {
			copied[denom] = nil
			continue
		}
		copied[denom] = new(big.Float).Copy(price)
	}
	return copied
}

// PriceSnapshot is a committed public price view from one runtime aggregation.
type PriceSnapshot struct {
	Prices    DenomPrices
	Timestamp time.Time
}

// Clone returns a deep copy of s.
func (s PriceSnapshot) Clone() PriceSnapshot {
	return PriceSnapshot{
		Prices:    s.Prices.Clone(),
		Timestamp: s.Timestamp,
	}
}

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
