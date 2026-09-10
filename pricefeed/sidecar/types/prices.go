package types

import (
	"fmt"
	"math/big"
	"time"
)

// Prices is the oracle-internal price map keyed by canonical BASE/QUOTE pairs.
type Prices map[Pair]*big.Float

// FeedPrices is the public/API-facing price map keyed by feed denom. Each
// value is NOAH per one unit of the feed's denomination, the orientation the
// chain stores.
type FeedPrices map[string]*big.Float

// PricePrecisionBits is the precision used for provider prices and resolver
// arithmetic. It comfortably exceeds the 18 decimal places serialised at the
// public LegacyDec boundary.
const PricePrecisionBits uint = 256

// maxLegacyDecExponent is the largest binary exponent strictly below the
// LegacyDec whole-number ceiling of 2^256.
const maxLegacyDecExponent = 256

// Clone returns a deep copy of p.
func (p FeedPrices) Clone() FeedPrices {
	copied := make(FeedPrices, len(p))
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
	// Prices contains the resolved feed-keyed public values committed by the
	// tick. Feeds without a usable price are absent.
	Prices FeedPrices
	// Timestamp is the aggregation time shared by every price in Prices.
	Timestamp time.Time
}

// PricesByFeed rekeys resolved UNIT/NOAH prices without inversion, retaining only requested feeds.
// An empty authoritative feed set yields nothing; nil-price skipping is a defensive backstop.
func PricesByFeed(prices Prices, feeds []string) FeedPrices {
	active := make(map[string]struct{}, len(feeds))
	for _, denom := range feeds {
		active[denom] = struct{}{}
	}

	result := make(FeedPrices, len(active))
	for pair, price := range prices {
		if price == nil {
			continue
		}

		denom := pair.Denom()
		if _, ok := active[denom]; !ok {
			continue
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

// MaxPriceTextBytes bounds the decimal text ParsePrice accepts. The full
// LegacyDec range renders within 97 characters plus a point; 128 retains
// headroom while refusing unbounded inputs before big.Float parsing.
const MaxPriceTextBytes = 128

// ParsePrice parses a finite, LegacyDec-representable decimal string into an
// oracle price.
func ParsePrice(s string) (*big.Float, error) {
	if len(s) > MaxPriceTextBytes {
		return nil, fmt.Errorf(
			"oracle price text length %d exceeds maximum %d",
			len(s),
			MaxPriceTextBytes,
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
