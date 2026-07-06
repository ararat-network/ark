package runtime

import (
	"context"
	"errors"
	"maps"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"cosmossdk.io/log/v2"

	"noah/oracle/sidecar/chainstate"
	"noah/oracle/sidecar/providers"
	provider "noah/oracle/sidecar/providers/base"
	"noah/oracle/sidecar/resolver"
	"noah/oracle/sidecar/types"
)

// Runtime runs price providers and exposes aggregated price state.
type Runtime struct {
	mut      sync.RWMutex
	updateMu sync.Mutex
	logger   log.Logger

	// Dependencies.
	resolver PriceResolver
	client   ChainStateClient

	// Lifecycle.
	// mainCtx is cancelled when the oracle stops; providers and tick work derive from it.
	mainCtx    context.Context
	mainCancel context.CancelFunc
	// wg waits for auxiliary runtime goroutines started by the oracle.
	wg      sync.WaitGroup
	running atomic.Bool
	// updateIntervalCh notifies the running fetch loop that its ticker interval changed.
	updateIntervalCh chan struct{}

	// Runtime state guarded by mut.
	providers map[string]*provider.Provider
	// lastPriceSync is the last time the oracle successfully updated its prices.
	lastPriceSync time.Time
	// denoms is the current effective target-denom snapshot used for missing-price accounting.
	denoms []string
	// denomsFromVoteTargets tracks whether denoms came from a successful on-chain vote-targets query.
	denomsFromVoteTargets bool

	// Configuration.
	cfg Config
}

// NewRuntime returns a new Runtime.
func NewRuntime(
	cfg Config,
	opts ...Option,
) (*Runtime, error) {
	cfg = cfg.Clone()
	r := &Runtime{
		cfg:              cfg,
		providers:        make(map[string]*provider.Provider),
		updateIntervalCh: make(chan struct{}, 1),
		logger:           log.NewNopLogger(),
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
	r.logger = r.logger.With("runtime", "oracle")
	if len(r.cfg.FallbackDenoms) != 0 {
		r.denoms = append([]string(nil), r.cfg.FallbackDenoms...)
	}
	if len(r.providers) == 0 {
		pairs := r.cfg.Resolver.MarketPairs(r.denoms)
		for _, providerCfg := range r.cfg.Providers {
			p, err := providers.NewProvider(providerCfg, providerCfg.Markets.FilterPairs(pairs), r.logger)
			if err != nil {
				return nil, err
			}

			r.providers[p.Name()] = p
		}
	}
	if r.resolver == nil {
		resolver, err := resolver.NewResolver(r.cfg.Resolver)
		if err != nil {
			return nil, err
		}
		r.resolver = resolver
	}
	if r.client == nil {
		client, err := chainstate.NewClient(r.cfg.Client)
		if err != nil {
			return nil, err
		}
		r.client = client
	}

	return r, nil
}

// GetProviders returns a snapshot of provider pointers.
func (r *Runtime) GetProviders() map[string]*provider.Provider {
	r.mut.RLock()
	defer r.mut.RUnlock()

	providers := make(map[string]*provider.Provider, len(r.providers))
	maps.Copy(providers, r.providers)

	return providers
}

// GetLastSyncTime returns the last time the oracle aggregated provider prices.
func (r *Runtime) GetLastSyncTime() time.Time {
	r.mut.RLock()
	defer r.mut.RUnlock()
	return r.lastPriceSync
}

// GetPrices returns current aggregated prices keyed by public vote-target denom.
func (r *Runtime) GetPrices() types.DenomPrices {
	r.mut.RLock()
	denoms := append([]string(nil), r.denoms...)
	r.mut.RUnlock()

	prices := types.PricesByDenom(r.resolver.GetPrices(), denoms)
	for _, denom := range denoms {
		if _, ok := prices[denom]; !ok {
			prices[denom] = new(big.Float)
		}
	}

	return prices
}
