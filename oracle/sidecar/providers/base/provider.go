package base

import (
	"context"
	"errors"
	"math/big"
	"sync"

	"golang.org/x/sync/errgroup"

	"cosmossdk.io/log/v2"

	"noah/oracle/sidecar/providers/types"
	oracletypes "noah/oracle/sidecar/types"
)

// Provider runs a fetcher over provider-specific tickers and exposes pair-keyed prices.
type Provider struct {
	logger        log.Logger
	fetcher       Fetcher
	name          string
	transportType TransportType
	markets       types.Markets

	mu         sync.Mutex
	prices     map[types.Ticker]types.Result
	responseCh chan types.Response

	lifecycleMu   sync.Mutex
	mainCtx       context.Context
	cancelMainFn  context.CancelFunc
	cycleCtx      context.Context
	cancelCycleFn context.CancelFunc
}

// NewProvider returns a provider using fetcher for provider-specific price data.
func NewProvider(
	name string,
	transportType TransportType,
	markets types.Markets,
	fetcher Fetcher,
	opts ...Option,
) (*Provider, error) {
	p := &Provider{
		logger:        log.NewNopLogger(),
		fetcher:       fetcher,
		name:          name,
		transportType: transportType,
		markets:       append(types.Markets{}, markets...),
		prices:        make(map[types.Ticker]types.Result),
	}

	for _, opt := range opts {
		opt(p)
	}

	if p.logger == nil {
		return nil, errors.New("logger is nil")
	}
	if len(p.name) == 0 {
		return nil, errors.New("provider name cannot be empty")
	}
	if len(p.transportType) == 0 {
		return nil, errors.New("provider transport type cannot be empty")
	}
	if err := p.markets.Validate(); err != nil {
		return nil, err
	}
	if fetcher == nil {
		return nil, errors.New("fetcher is nil")
	}
	if fetcher.Type() != transportType {
		return nil, errors.New("mismatched provider and fetcher type")
	}
	if fetcher.Name() != name {
		return nil, errors.New("mismatched provider and fetcher name")
	}

	p.logger = p.logger.With("provider", p.name)

	return p, nil
}

// Start runs the provider's fetch loop until the provider is stopped or the main
// context is cancelled.
func (p *Provider) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}

	p.logger.Info("starting provider")
	mainCtx, mainCancel := context.WithCancel(ctx)
	defer mainCancel()
	p.setMainCtx(mainCtx, mainCancel)

	// Start the main loop. Each cycle runs the fetcher for the current provider tickers and
	// updates cached prices from fetcher responses. Runtime ticker updates cancel only the
	// current fetch cycle, allowing the loop to restart with the new ticker set.
	for {
		tickers := p.GetTickers()
		// Ensure that the provider has tickers set. This could be reset if the provider is
		// restarted / reconfigured.
		if len(tickers) == 0 {
			p.logger.Debug("no tickers set on provider; exiting")
			return nil
		}

		// Create the response channel used to receive fetcher responses.
		fetcher := p.getFetcher()
		p.responseCh = make(chan types.Response, fetcher.ResponseBufferSize(tickers))

		// Create a new context for the fetch loop. This allows us to cancel the fetch loop
		// when the provider needs to be restarted.
		cycleCtx, cycleCancel := context.WithCancel(mainCtx)
		p.setCycleCtx(cycleCtx, cycleCancel)
		group, groupCtx := errgroup.WithContext(cycleCtx)
		group.Go(func() error {
			p.recv(groupCtx)
			return nil
		})
		group.Go(func() error {
			defer close(p.responseCh)
			return fetcher.Run(groupCtx, tickers, p.responseCh)
		})

		p.logger.Debug("started provider fetch and recv routines")
		err := group.Wait()
		p.logger.Debug("provider routines stopped", "error", err)

		if mainCtx.Err() != nil {
			p.logger.Info(
				"main provider context has been cancelled; provider is exiting",
				"error", mainCtx.Err(),
			)

			return err
		}
		// Continue to next cycle if context cancellation is on cycleCtx
		if cycleCtx.Err() != nil {
			continue
		}

		return err
	}
}

// Stop stops the provider's main loop.
func (p *Provider) Stop() {
	mainCtx, cancelMain := p.getMainCtx()
	if mainCtx == nil {
		p.logger.Debug("provider is not running")
		return
	}

	select {
	case <-mainCtx.Done():
		// The provider is already stopped.
		p.logger.Debug("provider is not running")
		return
	default:
		// Cancel the main context to stop the provider.
		p.logger.Debug("manually stopping provider")
		cancelMain()
	}
}

// IsRunning returns true if the provider is running.
func (p *Provider) IsRunning() bool {
	mainCtx, _ := p.getMainCtx()
	if mainCtx == nil {
		return false
	}

	select {
	case <-mainCtx.Done():
		return false
	default:
		return true
	}
}

// Name returns the name of the provider.
func (p *Provider) Name() string {
	return p.name
}

// GetPrices returns a copy of the latest result for each configured pair.
func (p *Provider) GetPrices() map[oracletypes.Pair]types.Result {
	p.mu.Lock()
	defer p.mu.Unlock()

	cpy := make(map[oracletypes.Pair]types.Result, len(p.prices))
	for ticker, result := range p.prices {
		pair, ok := p.markets.TickerToPair(ticker)
		if !ok {
			continue
		}
		if result.Price != nil {
			result.Price = new(big.Float).Copy(result.Price)
		}
		cpy[pair] = result
	}

	return cpy
}

// GetTickers returns the configured provider-specific symbols.
func (p *Provider) GetTickers() []types.Ticker {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.markets.Tickers()
}

// Type returns the provider transport type.
func (p *Provider) Type() TransportType {
	return p.transportType
}

// getFetcher gets the providers fetcher
func (p *Provider) getFetcher() Fetcher {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.fetcher
}

// Provider lifecycle context helpers.

// setMainCtx stores the provider lifecycle context.
func (p *Provider) setMainCtx(ctx context.Context, cancel context.CancelFunc) {
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()

	p.mainCtx, p.cancelMainFn = ctx, cancel
}

// getMainCtx returns the provider lifecycle context and its cancel function.
func (p *Provider) getMainCtx() (context.Context, context.CancelFunc) {
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()

	return p.mainCtx, p.cancelMainFn
}

// setCycleCtx stores the current fetch-cycle context.
func (p *Provider) setCycleCtx(ctx context.Context, cancel context.CancelFunc) {
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()

	p.cycleCtx, p.cancelCycleFn = ctx, cancel
}

// getCycleCtx returns the current fetch-cycle context and its cancel function.
func (p *Provider) getCycleCtx() (context.Context, context.CancelFunc) {
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()

	return p.cycleCtx, p.cancelCycleFn
}
