package types

import "fmt"

// Market maps a chain denom to the provider symbol used to fetch its price.
type Market struct {
	// Denom is the chain/oracle-facing denom, for example uusd.
	Denom string `json:"denom"`
	// Symbol is the provider/API-facing market symbol, for example USDTUSD.
	Symbol Ticker `json:"symbol"`
}

// Markets contains the configured denom-to-provider-symbol mappings for a provider.
type Markets []Market

// TickerToDenom returns the chain denom configured for ticker.
// Ticker comparison is case-insensitive.
func (m Markets) TickerToDenom(ticker Ticker) (string, bool) {
	for _, market := range m {
		if ticker.Key() == market.Symbol.Key() {
			return market.Denom, true
		}
	}

	return "", false
}

// DenomToTicker returns the provider symbol configured for denom.
func (m Markets) DenomToTicker(denom string) (Ticker, bool) {
	for _, market := range m {
		if denom == market.Denom {
			return market.Symbol, true
		}
	}

	return "", false
}

// Validate checks that markets are non-empty and contain unique denom/symbol mappings.
func (m Markets) Validate() error {
	if len(m) == 0 {
		return fmt.Errorf("markets is empty")
	}

	denoms := make(map[string]struct{}, len(m))
	symbols := make(map[string]struct{}, len(m))
	for _, market := range m {
		if market.Denom == "" {
			return fmt.Errorf("denom is empty")
		}
		if market.Symbol == "" {
			return fmt.Errorf("symbol is empty")
		}

		if _, ok := denoms[market.Denom]; ok {
			return fmt.Errorf("duplicate denom %q", market.Denom)
		}
		denoms[market.Denom] = struct{}{}

		symbolKey := market.Symbol.Key()
		if _, ok := symbols[symbolKey]; ok {
			return fmt.Errorf("duplicate symbol %q", symbolKey)
		}
		symbols[symbolKey] = struct{}{}
	}

	return nil
}
