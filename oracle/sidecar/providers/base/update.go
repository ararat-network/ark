package base

import "noah/oracle/sidecar/providers/types"

// Update applies prevalidated market mappings and restarts the active fetch
// cycle. Callers that accept external config should validate before calling it.
func (p *Provider) Update(markets types.Markets) {
	p.logger.Debug("updating provider")
	p.mu.Lock()
	defer p.mu.Unlock()

	p.setMarkets(markets)
	p.logger.Debug("provider updated")

	if _, cancel := p.getCycleCtx(); cancel != nil {
		p.logger.Debug("canceling fetch context; restarting provider")
		cancel()
	}
}

// setMarkets commits provider markets and retains cached prices only for
// unchanged pair/symbol mappings.
func (p *Provider) setMarkets(markets types.Markets) {
	nextMarkets := append(types.Markets(nil), markets...)
	retained := make(map[types.Ticker]types.Result, len(p.prices))

	for ticker, result := range p.prices {
		currentPair, currentOK := p.markets.TickerToPair(ticker)
		nextPair, nextOK := nextMarkets.TickerToPair(ticker)
		if currentOK && nextOK && currentPair == nextPair {
			retained[ticker] = result
		}
	}

	p.markets = nextMarkets
	p.prices = retained
}
