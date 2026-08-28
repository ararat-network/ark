package types

import (
	"errors"
	"fmt"

	oracletypes "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

// Market maps an exchange-rate pair to the provider symbol used to fetch its price.
type Market struct {
	// Pair is the internal id for a pair, for example USD/KRW.
	Pair oracletypes.Pair `json:"pair"`
	// Symbol is the provider/API-facing market symbol, for example USDTUSD.
	Symbol Ticker `json:"symbol"`
}

// Markets contains the configured pair-to-provider-symbol mappings for a provider.
type Markets []Market

// TickerToPair returns the configured pair for ticker.
// Ticker comparison is case-insensitive.
func (m Markets) TickerToPair(ticker Ticker) (oracletypes.Pair, bool) {
	for _, market := range m {
		if ticker.Key() == market.Symbol.Key() {
			return market.Pair, true
		}
	}

	return "", false
}

// PairToTicker returns the provider symbol configured for pair.
func (m Markets) PairToTicker(pair oracletypes.Pair) (Ticker, bool) {
	for _, market := range m {
		if pair == market.Pair {
			return market.Symbol, true
		}
	}

	return "", false
}

// Tickers returns the configured provider symbols in market order.
func (m Markets) Tickers() []Ticker {
	tickers := make([]Ticker, 0, len(m))
	for _, market := range m {
		tickers = append(tickers, market.Symbol)
	}
	return tickers
}

// Equal reports whether two market lists contain the same values, regardless of order.
func (m Markets) Equal(other Markets) bool {
	if len(m) != len(other) {
		return false
	}

	counts := make(map[Market]int, len(m))
	for _, market := range m {
		counts[market]++
	}
	for _, market := range other {
		count := counts[market]
		if count == 0 {
			return false
		}
		if count == 1 {
			delete(counts, market)
		} else {
			counts[market] = count - 1
		}
	}

	return len(counts) == 0
}

// FilterPairs returns markets whose pairs are in pairs. A nil set means no
// filtering; a non-nil empty set returns no markets.
func (m Markets) FilterPairs(pairs map[oracletypes.Pair]struct{}) Markets {
	if pairs == nil {
		return append(Markets(nil), m...)
	}

	filtered := make(Markets, 0, len(m))
	for _, market := range m {
		if _, ok := pairs[market.Pair]; ok {
			filtered = append(filtered, market)
		}
	}

	return filtered
}

// Validate checks that markets are non-empty and contain unique pair/symbol mappings.
func (m Markets) Validate() error {
	if len(m) == 0 {
		return errors.New("markets is empty")
	}

	pairs := make(map[oracletypes.Pair]struct{}, len(m))
	symbols := make(map[string]struct{}, len(m))
	for _, market := range m {
		if len(market.Pair) == 0 {
			return errors.New("pair is empty")
		}
		if len(market.Symbol) == 0 {
			return errors.New("symbol is empty")
		}
		if err := market.Pair.Validate(); err != nil {
			return err
		}

		if _, ok := pairs[market.Pair]; ok {
			return fmt.Errorf("duplicate pair %q", market.Pair)
		}
		pairs[market.Pair] = struct{}{}

		symbolKey := market.Symbol.Key()
		if _, ok := symbols[symbolKey]; ok {
			return fmt.Errorf("duplicate symbol %q", symbolKey)
		}
		symbols[symbolKey] = struct{}{}
	}

	return nil
}
