package runtime

import (
	"context"
	"errors"
	"sync"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/chainstate"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base"
	providertypes "github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	"github.com/ararat-network/ark/pricefeed/sidecar/types"
)

// Runtime runs price providers and exposes aggregated price state.
type Runtime struct {
	// updateMu serialises config updates, price aggregation ticks,
	// feed market retargeting, and provider lifecycle transitions.
	// If both locks are needed, take updateMu before mut.
	updateMu sync.Mutex

	// mut guards the mutable runtime state below: cfg, mainCtx, mainCancel,
	// priceSnapshot, feeds, and feedsFromChain.
	mut sync.RWMutex

	logger log.Logger

	// Collaborators owned by the runtime.
	providerFactory func(providers.Config, providertypes.Markets) (*base.Provider, error)
	client          ChainStateClient

	// mainCtx and mainCancel are non-nil while the blocking Run lifecycle is
	// active. They are published and cleared together during updateMu-protected
	// lifecycle transitions.
	mainCtx    context.Context
	mainCancel context.CancelCauseFunc

	// updateIntervalCh is created once and wakes the Run loop after an
	// UpdateInterval config change.
	updateIntervalCh chan struct{}

	// Config state guarded by mut.
	cfg Config

	// Provider map membership is guarded by updateMu.
	providers map[string]*managedProvider

	// feedReadFailed is guarded by updateMu and makes feed-read logging
	// edge-triggered. The chainstate client already reports each underlying poll
	// failure, so the runtime reports only transitions in its own feed state.
	feedReadFailed bool

	// Price aggregation state guarded by mut.
	priceSnapshot types.PriceSnapshot

	// Feed state guarded by mut. feeds is the effective feed-denom snapshot used
	// for provider market filtering, price output, and missing-price metrics.
	feeds []string

	// feedsFromChain is false while feeds comes from fallback config; once true,
	// feed refresh failures preserve the last on-chain snapshot.
	feedsFromChain bool
}

// NewRuntime clones and validates cfg, constructs the configured providers and
// chainstate client, and returns them unstarted under runtime ownership.
func NewRuntime(cfg Config, opts ...Option) (*Runtime, error) {
	cfg = cfg.Clone()
	r := &Runtime{
		cfg:              cfg,
		providers:        make(map[string]*managedProvider),
		updateIntervalCh: make(chan struct{}, 1),
		logger:           log.NewNopLogger(),
	}
	r.providerFactory = func(cfg providers.Config, markets providertypes.Markets) (*base.Provider, error) {
		return providers.NewProvider(cfg, markets, r.logger)
	}

	for _, opt := range opts {
		opt(r)
	}

	if err := r.cfg.Validate(); err != nil {
		return nil, err
	}
	if r.logger == nil {
		return nil, errors.New("logger is nil")
	}
	if r.providerFactory == nil {
		return nil, errors.New("provider factory is nil")
	}
	r.logger = r.logger.With("component", "runtime")
	if len(r.cfg.FallbackFeeds) != 0 {
		r.feeds = append([]string(nil), r.cfg.FallbackFeeds...)
	}
	pairs := r.cfg.Resolver.MarketPairs(r.feeds)
	for _, providerCfg := range r.cfg.Providers {
		managed, err := r.newManagedProvider(providerCfg, providerCfg.Markets.FilterPairs(pairs))
		if err != nil {
			return nil, err
		}

		r.providers[managed.provider.Name()] = managed
	}
	if r.client == nil {
		client, err := chainstate.NewClient(r.cfg.Client, chainstate.WithLogger(r.logger))
		if err != nil {
			return nil, err
		}
		r.client = client
	}

	return r, nil
}

// GetPriceSnapshot returns a deep copy of the latest coherent public price and
// timestamp snapshot, so callers cannot mutate runtime-owned cache state.
func (r *Runtime) GetPriceSnapshot() types.PriceSnapshot {
	r.mut.RLock()
	defer r.mut.RUnlock()

	return types.PriceSnapshot{
		Prices:    r.priceSnapshot.Prices.Clone(),
		Timestamp: r.priceSnapshot.Timestamp,
	}
}
