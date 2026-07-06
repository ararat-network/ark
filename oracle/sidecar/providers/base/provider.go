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
	doneCh        chan struct{}
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
	if len(p.markets) > 0 {
		if err := p.markets.Validate(); err != nil {
			return nil, err
		}
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

// Start starts the provider's fetch loop if it is not already running.
func (p *Provider) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		mainCtx, mainCancel := context.WithCancel(ctx)
		doneCh := make(chan struct{})

		p.lifecycleMu.Lock()
		if p.doneCh != nil {
			running := p.mainCtx != nil && p.mainCtx.Err() == nil
			done := p.doneCh
			p.lifecycleMu.Unlock()
			mainCancel()

			if running {
				p.logger.Debug("provider is already running")
				return nil
			}

			select {
			case <-done:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		p.mainCtx = mainCtx
		p.cancelMainFn = mainCancel
		p.cycleCtx = nil
		p.cancelCycleFn = nil
		p.doneCh = doneCh
		p.lifecycleMu.Unlock()

		p.logger.Info("starting provider")
		go p.run(mainCtx, mainCancel, doneCh)
		return nil
	}
}

func (p *Provider) run(mainCtx context.Context, mainCancel context.CancelFunc, doneCh chan struct{}) {
	defer func() {
		mainCancel()

		p.lifecycleMu.Lock()
		defer p.lifecycleMu.Unlock()

		p.mainCtx = nil
		p.cancelMainFn = nil
		p.cycleCtx = nil
		p.cancelCycleFn = nil
		p.doneCh = nil
		close(doneCh)
	}()

	err := RunRecovering("provider run loop", func() error {
		return p.runLoop(mainCtx)
	})
	if err != nil {
		if mainCtx.Err() != nil || errors.Is(err, context.Canceled) {
			return
		}
		p.logger.Error("provider exited", "error", err)
		return
	}
	if mainCtx.Err() == nil {
		p.logger.Warn("provider exited without error")
	}
}

// runLoop runs fetch cycles until the provider is stopped or mainCtx is cancelled.
func (p *Provider) runLoop(mainCtx context.Context) error {
	// Start the main loop. Each cycle runs the fetcher for the current provider tickers and
	// updates cached prices from fetcher responses. Runtime ticker updates cancel only the
	// current fetch cycle, allowing the loop to restart with the new ticker set.
	for {
		// Create a new context for this cycle before reading tickers, so updates
		// can wake providers parked with no active markets.
		cycleCtx, cycleCancel := context.WithCancel(mainCtx)
		p.setCycleCtx(cycleCtx, cycleCancel)

		tickers := p.GetTickers()
		if len(tickers) == 0 {
			p.logger.Debug("no tickers set on provider; waiting for update")
			<-cycleCtx.Done()
			if mainCtx.Err() != nil {
				p.logger.Info(
					"main provider context has been cancelled; provider is exiting",
					"error", mainCtx.Err(),
				)

				return mainCtx.Err()
			}
			continue
		}

		// Create the response channel used to receive fetcher responses.
		fetcher := p.getFetcher()
		p.responseCh = make(chan types.Response, fetcher.ResponseBufferSize(tickers))

		group, groupCtx := errgroup.WithContext(cycleCtx)
		group.Go(func() (err error) {
			return RunRecovering("provider recv", func() error {
				p.recv(groupCtx)
				return nil
			})
		})

		group.Go(func() (err error) {
			defer close(p.responseCh)
			return RunRecovering("provider fetcher", func() error {
				return fetcher.Run(groupCtx, tickers, p.responseCh)
			})
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

// Stop stops the provider's main loop and waits for it to exit.
func (p *Provider) Stop() {
	p.lifecycleMu.Lock()
	mainCtx := p.mainCtx
	cancelMain := p.cancelMainFn
	doneCh := p.doneCh
	p.lifecycleMu.Unlock()

	if doneCh == nil {
		p.logger.Debug("provider is not running")
		return
	}

	if cancelMain != nil {
		if mainCtx != nil && mainCtx.Err() == nil {
			p.logger.Debug("manually stopping provider")
		}
		cancelMain()
	}
	if doneCh != nil {
		<-doneCh
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
