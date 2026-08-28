package base

import (
	"context"
	"errors"
	"math/big"
	"sync"

	"golang.org/x/sync/errgroup"

	"cosmossdk.io/log/v2"

	sidecarinternal "github.com/ararat-network/ark/pricefeed/sidecar/internal"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	oracletypes "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

// Provider runs a fetcher over provider-specific tickers and exposes pair-keyed prices.
type Provider struct {
	logger        log.Logger
	fetcher       Fetcher
	name          string
	transportType TransportType
	markets       types.Markets

	// mu guards markets and cached pair prices.
	mu     sync.RWMutex
	prices map[oracletypes.Pair]types.Result
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
		prices:        make(map[oracletypes.Pair]types.Result),
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

// Run fetches and caches prices for the current market snapshot until ctx is
// cancelled or the fetcher exits. The caller owns goroutine lifecycle.
func (p *Provider) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	tickers := p.GetTickers()
	if len(tickers) == 0 {
		p.logger.Debug("no tickers set on provider; waiting for cancellation")
		<-ctx.Done()
		return ctx.Err()
	}

	responseCh := make(chan types.Response, p.fetcher.ResponseBufferSize(tickers))
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		return sidecarinternal.RunRecovering("provider recv", func() error {
			p.recv(groupCtx, responseCh)
			return nil
		})
	})
	group.Go(func() error {
		defer close(responseCh)
		return sidecarinternal.RunRecovering("provider fetcher", func() error {
			return p.fetcher.Run(groupCtx, tickers, responseCh)
		})
	})

	p.logger.Debug("started provider fetch and recv routines")
	err := group.Wait()
	p.logger.Debug("provider routines stopped", "error", err)

	return err
}

// Name returns the name of the provider.
func (p *Provider) Name() string {
	return p.name
}

// GetPrices returns a copy of the latest result for each configured pair.
func (p *Provider) GetPrices() map[oracletypes.Pair]types.Result {
	p.mu.RLock()
	defer p.mu.RUnlock()

	cpy := make(map[oracletypes.Pair]types.Result, len(p.prices))
	for pair, result := range p.prices {
		if result.Price != nil {
			result.Price = new(big.Float).Copy(result.Price)
		}
		cpy[pair] = result
	}

	return cpy
}

// GetTickers returns the configured provider-specific symbols.
func (p *Provider) GetTickers() []types.Ticker {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.markets.Tickers()
}

// Type returns the provider transport type.
func (p *Provider) Type() TransportType {
	return p.transportType
}
