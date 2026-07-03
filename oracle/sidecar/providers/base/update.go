package base

import (
	"noah/oracle/sidecar/providers/types"
)

// UpdateOption stages part of a provider runtime update.
//
// Options mutate an updateState draft. Update commits the draft only after all
// options succeed, so failed updates leave the live provider unchanged.
type UpdateOption func(*updateState) error

// updateState is a draft provider state used to make updates atomic.
type updateState struct {
	markets types.Markets
}

// WithNewMarkets replaces the markets used to resolve provider tickers.
func WithNewMarkets(markets types.Markets) UpdateOption {
	return func(state *updateState) error {
		if len(markets) == 0 {
			return nil
		}
		if err := markets.Validate(); err != nil {
			return err
		}

		state.markets = append(types.Markets{}, markets...)
		return nil
	}
}

// Update applies options and restarts the active fetch cycle.
//
// If any option fails, Update returns before canceling the current fetch
// context.
func (p *Provider) Update(opts ...UpdateOption) error {
	p.logger.Debug("updating provider")
	p.mu.Lock()
	defer p.mu.Unlock()

	state := p.updateSnapshot()
	for _, opt := range opts {
		if err := opt(&state); err != nil {
			return err
		}
	}

	p.setMarkets(state.markets)
	p.logger.Debug("provider updated")

	if _, cancel := p.getCycleCtx(); cancel != nil {
		p.logger.Debug("canceling fetch context; restarting provider")
		cancel()
	}

	return nil
}

// updateSnapshot returns a draft copy of the current provider state.
func (p *Provider) updateSnapshot() updateState {
	return updateState{
		markets: append(types.Markets(nil), p.markets...),
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
