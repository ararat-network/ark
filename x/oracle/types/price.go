package types

import (
	time "time"

	"cosmossdk.io/math"
)

// NewExchangeRate creates a ExchangeRate instance
func NewExchangeRate(denom string, rate math.LegacyDec, timestamp time.Time, height uint64) ExchangeRate {
	return ExchangeRate{
		denom,
		rate,
		timestamp,
		height,
	}
}

// ExchangeRates - array of ExchangeRate
type ExchangeRates []ExchangeRate

// TobinTaxes is a list of TobinTax
type TobinTaxes []TobinTax
