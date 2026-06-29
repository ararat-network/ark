package base

import (
	"context"
	"fmt"
	"math/big"
	"sync"

	"golang.org/x/sync/errgroup"

	"cosmossdk.io/log/v2"

	"noah/oracle/providers/types"
)

// Provider runs a fetcher over provider-specific tickers and exposes denom-keyed prices.
type Provider struct {
	logger  log.Logger
	fetcher Fetcher
	denoms  []string
	config  Config

	mu         sync.Mutex
	prices     map[types.Ticker]types.Result
	tickers    []types.Ticker
	responseCh chan types.Response

	lifecycleMu   sync.Mutex
	mainCtx       context.Context
	cancelMainFn  context.CancelFunc
	cycleCtx      context.Context
	cancelCycleFn context.CancelFunc
}

// NewProvider returns a provider using fetcher for provider-specific price data.
func NewProvider(cfg Config, fetcher Fetcher, opts ...Option) (*Provider, error) {
	p := &Provider{
		logger:  log.NewNopLogger(),
		fetcher: fetcher,
		config:  cfg.Clone(),
		tickers: make([]types.Ticker, 0),
		prices:  make(map[types.Ticker]types.Result),
	}

	for _, opt := range opts {
		opt(p)
	}

	if p.logger == nil {
		return nil, fmt.Errorf("logger is nil")
	}
	if err := p.config.Validate(); err != nil {
		return nil, err
	}
	if err := validateFetcher(p.config, p.fetcher); err != nil {
		return nil, err
	}

	p.logger = p.logger.With("provider", p.config.Name)
	p.tickers = p.resolveTickers(p.denoms, p.config.Markets)

	return p, nil
}

func validateFetcher(config Config, fetcher Fetcher) error {
	if fetcher == nil {
		return fmt.Errorf("fetcher is nil")
	}
	if fetcher.Type() != config.Type {
		return fmt.Errorf("mismatched provider and fetcher type")
	}
	if fetcher.Name() != config.Name {
		return fmt.Errorf("mismatched provider and fetcher name")
	}

	return nil
}

// Start runs the provider's fetch loop until the provider is stopped or the main
// context is cancelled.
func (p *Provider) Start(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("context cannot be nil")
	}

	p.logger.Info("starting provider")
	mainCtx, mainCancel := context.WithCancel(ctx)
	defer mainCancel()
	p.setMainCtx(mainCtx, mainCancel)

	// Start the main loop. Each cycle runs the fetcher for the current provider tickers and
	// updates cached prices from fetcher responses. Runtime updates cancel only the current
	// fetch cycle, allowing the loop to restart with the new provider configuration.
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
	return p.getConfig().Name
}

// GetPrices returns a copy of the latest result for each configured denom.
func (p *Provider) GetPrices() map[string]types.Result {
	p.mu.Lock()
	defer p.mu.Unlock()

	cpy := make(map[string]types.Result, len(p.prices))
	for ticker, result := range p.prices {
		denom, ok := p.config.Markets.TickerToDenom(ticker)
		if !ok {
			continue
		}
		if result.Price != nil {
			result.Price = new(big.Float).Copy(result.Price)
		}
		cpy[denom] = result
	}

	return cpy
}

// GetTickers returns a copy of the configured provider-specific symbols.
func (p *Provider) GetTickers() []types.Ticker {
	p.mu.Lock()
	defer p.mu.Unlock()

	tickers := make([]types.Ticker, len(p.tickers))
	copy(tickers, p.tickers)

	return tickers
}

// Type returns the provider type.
func (p *Provider) Type() TransportType {
	return p.getConfig().Type
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
