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

// PricePrecisionBits is the precision used for provider prices and resolver
// arithmetic. It comfortably exceeds the 18 decimal places serialised at the
// public LegacyDec boundary.
const PricePrecisionBits uint = 256

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
	// Prices is the complete denom-keyed public view committed by the tick.
	Prices DenomPrices
	// Timestamp is the aggregation time shared by every price in Prices.
	Timestamp time.Time
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

// ParsePrice parses a finite decimal string into an oracle price.
func ParsePrice(s string) (*big.Float, error) {
	price, ok := new(big.Float).SetPrec(PricePrecisionBits).SetString(s)
	if !ok || price.IsInf() {
		return nil, fmt.Errorf("failed to parse oracle price %q", s)
	}

	return price, nil
}
