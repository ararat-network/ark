package base

import (
	"fmt"

	"noah/oracle/providers/types"
)

// UpdateOption stages part of a provider runtime update.
//
// Options mutate an updateState draft. Update commits the draft only after all
// options succeed, so failed updates leave the live provider unchanged.
type UpdateOption func(*updateState) error

// updateState is a draft provider state used to make updates atomic.
type updateState struct {
	config       Config
	fetcher      Fetcher
	denoms       []string
	filterPrices bool
}

// WithNewDenoms replaces the chain denoms used to resolve provider tickers.
func WithNewDenoms(denoms []string) UpdateOption {
	return func(state *updateState) error {
		if len(denoms) == 0 {
			return fmt.Errorf("denoms is empty")
		}

		state.denoms = append([]string(nil), denoms...)
		return nil
	}
}

// WithNewConfig replaces provider identity and market mapping for the next
// fetch cycle. Existing prices are kept only when their ticker still exists in
// the new market mapping.
func WithNewConfig(config Config) UpdateOption {
	return func(state *updateState) error {
		if err := config.Validate(); err != nil {
			return err
		}

		state.config = config.Clone()
		state.filterPrices = true
		return nil
	}
}

// WithNewFetcher replaces the fetcher used by the next provider fetch cycle.
func WithNewFetcher(fetcher Fetcher) UpdateOption {
	return func(state *updateState) error {
		if fetcher == nil {
			return fmt.Errorf("fetcher is nil")
		}

		state.fetcher = fetcher
		return nil
	}
}

// Update applies options atomically and restarts the active fetch cycle.
//
// If any option fails, Update returns before committing state or canceling the
// current fetch context.
func (p *Provider) Update(opts ...UpdateOption) error {
	p.logger.Debug("updating provider")
	state := p.updateSnapshot()
	for _, opt := range opts {
		if err := opt(&state); err != nil {
			return err
		}
	}

	if err := validateFetcher(state.config, state.fetcher); err != nil {
		return err
	}

	tickers := p.resolveTickers(state.denoms, state.config.Markets)
	p.applyUpdate(state, tickers)
	p.logger.Debug("provider updated")

	if _, cancel := p.getCycleCtx(); cancel != nil {
		p.logger.Debug("canceling fetch context; restarting provider")
		cancel()
	}

	return nil
}

// updateSnapshot returns a draft copy of the current provider state.
func (p *Provider) updateSnapshot() updateState {
	p.mu.Lock()
	defer p.mu.Unlock()

	return updateState{
		config:  p.config.Clone(),
		fetcher: p.fetcher,
		denoms:  append([]string(nil), p.denoms...),
	}
}

// applyUpdate commits a validated draft and resolved ticker snapshot.
func (p *Provider) applyUpdate(state updateState, tickers []types.Ticker) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.config = state.config.Clone()
	p.fetcher = state.fetcher
	p.denoms = append([]string(nil), state.denoms...)
	p.tickers = append([]types.Ticker(nil), tickers...)
	if state.filterPrices {
		p.prices = filterPricesByMarkets(p.prices, state.config.Markets)
	}
}

// filterPricesByMarkets preserves cached prices whose ticker is still present
// in the current market mapping.
func filterPricesByMarkets(prices map[types.Ticker]types.Result, markets types.Markets) map[types.Ticker]types.Result {
	filtered := make(map[types.Ticker]types.Result, len(prices))
	for ticker, result := range prices {
		if _, ok := markets.TickerToDenom(ticker); ok {
			filtered[ticker] = result
		}
	}

	return filtered
}

// getConfig returns a cloned config snapshot for readers that need Markets.
func (p *Provider) getConfig() Config {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.config.Clone()
}

// providerLabels returns metric labels without cloning Markets.
func (p *Provider) providerLabels() (string, TransportType) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.config.Name, p.config.Type
}

// resolveTickers resolves chain denoms against a market snapshot.
func (p *Provider) resolveTickers(denoms []string, markets types.Markets) []types.Ticker {
	tickers := make([]types.Ticker, 0, len(denoms))
	for _, d := range denoms {
		if ticker, ok := markets.DenomToTicker(d); ok {
			tickers = append(tickers, ticker)
		} else {
			p.logger.Error("failed to convert denom to ticker", "denom", d)
		}
	}

	return tickers
}

func (p *Provider) getFetcher() Fetcher {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.fetcher
}
