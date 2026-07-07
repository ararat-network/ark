package sidecar

import (
	"fmt"

	"cosmossdk.io/math"

	"noah/oracle/sidecar/types"
	"noah/pkg/encoding"
)

// ToReqPrices encodes public denom prices into the generated transport payload.
//
// Runtime already projects pair prices to active denoms. This helper preserves
// that boundary by accepting denom-keyed prices only and rejecting nil values
// before serializing rates as LegacyDec bytes.
func ToReqPrices(prices types.DenomPrices) (map[string][]byte, error) {
	result := make(map[string][]byte, len(prices))

	for ticker, price := range prices {
		if price == nil {
			return nil, fmt.Errorf("nil price for %s", ticker)
		}

		rate, err := math.LegacyNewDecFromStr(price.Text('f', math.LegacyPrecision))
		if err != nil {
			return nil, fmt.Errorf("convert price %s: %w", ticker, err)
		}
		rawRate, err := encoding.EncodeLegacyDec(rate)
		if err != nil {
			return nil, fmt.Errorf("encoding rate: %w", err)
		}

		result[ticker] = rawRate
	}

	return result, nil
}
