package types

import (
	"fmt"
	"math/big"
	"time"

	"ark/pkg/encoding"
)

// Prices is the oracle-internal price map keyed by canonical BASE/QUOTE pairs.
type Prices map[Pair]*big.Float

// DenomPrices is the public/API-facing price map keyed by vote-target denom.
type DenomPrices map[string]*big.Float

// PricePrecisionBits is the precision used for provider prices and resolver
// arithmetic. It comfortably exceeds the 18 decimal places serialised at the
// public LegacyDec boundary.
const PricePrecisionBits uint = 256

// maxLegacyDecExponent is the largest binary exponent strictly below the
// LegacyDec whole-number ceiling of 2^256.
const maxLegacyDecExponent = 256

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
	// Prices contains the resolved denom-keyed public values committed by the
	// tick. Denoms without a usable price are absent.
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

// ValidatePrice checks that price is finite and can be converted to LegacyDec
// without expanding an unbounded fixed-point string. Positivity is enforced by
// the resolver; missing prices are omitted from public snapshots.
func ValidatePrice(price *big.Float) error {
	if price == nil {
		return fmt.Errorf("oracle price is nil")
	}
	if price.IsInf() {
		return fmt.Errorf("oracle price is infinite")
	}
	if price.MantExp(nil) > maxLegacyDecExponent {
		return fmt.Errorf("oracle price magnitude exceeds LegacyDec range")
	}

	return nil
}

// ParsePrice parses a finite, LegacyDec-representable decimal string into an
// oracle price.
func ParsePrice(s string) (*big.Float, error) {
	if len(s) > encoding.MaxEncodedLegacyDecBytes {
		return nil, fmt.Errorf(
			"oracle price text length %d exceeds maximum %d",
			len(s),
			encoding.MaxEncodedLegacyDecBytes,
		)
	}

	price, ok := new(big.Float).SetPrec(PricePrecisionBits).SetString(s)
	if !ok {
		return nil, fmt.Errorf("failed to parse oracle price %q", s)
	}
	if err := ValidatePrice(price); err != nil {
		return nil, fmt.Errorf("invalid oracle price %q: %w", s, err)
	}

	return price, nil
}
