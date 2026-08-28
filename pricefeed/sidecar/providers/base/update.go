package base

import (
	"ark/pricefeed/sidecar/providers/types"
	oracletypes "ark/pricefeed/sidecar/types"
)

// UpdateMarkets applies prevalidated market mappings. The runtime stops the
// active run before calling this method and starts a new run afterward.
func (p *Provider) UpdateMarkets(markets types.Markets) {
	p.logger.Debug("updating provider")
	p.mu.Lock()
	defer p.mu.Unlock()

	p.setMarkets(markets)
	p.logger.Debug("provider updated")
}

// setMarkets commits provider markets and retains cached prices only for
// unchanged pair/symbol mappings.
func (p *Provider) setMarkets(markets types.Markets) {
	nextMarkets := append(types.Markets(nil), markets...)
	retained := make(map[oracletypes.Pair]types.Result, len(p.prices))

	for pair, result := range p.prices {
		currentTicker, currentOK := p.markets.PairToTicker(pair)
		nextTicker, nextOK := nextMarkets.PairToTicker(pair)
		if currentOK && nextOK && currentTicker.Key() == nextTicker.Key() {
			retained[pair] = result
		}
	}

	p.markets = nextMarkets
	p.prices = retained
}
