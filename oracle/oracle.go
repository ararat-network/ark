package oracle

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"cosmossdk.io/log/v2"

	"noah/oracle/providers"
	provider "noah/oracle/providers/base"
	"noah/oracle/types"
)

// Oracle runs price providers and exposes aggregated price state.
type Oracle struct {
	mut     sync.RWMutex
	logger  log.Logger
	running atomic.Bool

	// -------------------Lifecycle Fields-------------------//
	//
	// mainCtx is the main context for the oracle.
	mainCtx context.Context
	// mainCancel is the main context cancel function.
	mainCancel context.CancelFunc
	// wg is the wait group for the oracle.
	wg sync.WaitGroup
	// updateIntervalCh notifies the running fetch loop that its ticker interval changed.
	updateIntervalCh chan struct{}

	// -------------------Stateful Fields-------------------//
	//
	// providers is a map of all price providers that the oracle is using.
	providers map[string]*provider.Provider
	// aggregator is the price aggregator.
	aggregator PriceAggregator
	// lastPriceSync is the last time the oracle successfully updated its prices.
	lastPriceSync time.Time

	// -------------------Oracle Configuration Fields-------------------//
	//
	// cfg is the oracle configuration.
	cfg Config
	// lastUpdated tracks the last block height associated with an oracle update.
	lastUpdated uint64
}

// NewOracle returns a new Oracle.
func NewOracle(
	cfg Config,
	aggregator PriceAggregator,
	opts ...Option,
) (*Oracle, error) {
	o := &Oracle{
		cfg:              cfg,
		aggregator:       aggregator,
		providers:        make(map[string]*provider.Provider),
		updateIntervalCh: make(chan struct{}, 1),
		logger:           log.NewNopLogger(),
	}

	for _, opt := range opts {
		opt(o)
	}

	if err := o.cfg.Validate(); err != nil {
		return nil, err
	}
	if o.logger == nil {
		return nil, errors.New("logger is nil")
	}
	if o.aggregator == nil {
		return nil, errors.New("aggregator is required")
	}

	if len(o.providers) == 0 {
		for _, providerCfg := range o.cfg.Providers {
			p, err := providers.NewProvider(providerCfg, o.logger, o.cfg.Denoms)
			if err != nil {
				return nil, err
			}

			o.providers[p.Name()] = p
		}
	}

	return o, nil
}

// GetProviders returns a snapshot of provider pointers.
func (o *Oracle) GetProviders() map[string]*provider.Provider {
	o.mut.RLock()
	defer o.mut.RUnlock()

	providers := make(map[string]*provider.Provider, len(o.providers))
	for name, p := range o.providers {
		providers[name] = p
	}

	return providers
}

// GetLastSyncTime returns the last time the oracle aggregated provider prices.
func (o *Oracle) GetLastSyncTime() time.Time {
	o.mut.RLock()
	defer o.mut.RUnlock()
	return o.lastPriceSync
}

// GetPrices returns the current aggregated prices.
func (o *Oracle) GetPrices() types.Prices {
	return o.aggregator.GetPrices()
}
