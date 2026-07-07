package runtime

import (
	"context"
	"errors"
	"maps"
	"math/big"
	"sync"
	"sync/atomic"

	"cosmossdk.io/log/v2"

	"noah/oracle/sidecar/chainstate"
	"noah/oracle/sidecar/providers"
	provider "noah/oracle/sidecar/providers/base"
	"noah/oracle/sidecar/resolver"
	"noah/oracle/sidecar/types"
)

// Runtime runs price providers and exposes aggregated price state.
type Runtime struct {
	// updateMu serialises config updates, price aggregation ticks,
	// vote-target market retargeting, and provider lifecycle transitions.
	// If both locks are needed, take updateMu before mut.
	updateMu sync.Mutex

	// mut guards the mutable runtime state below: cfg, providers, mainCtx,
	// mainCancel, priceSnapshot, denoms, and denomsFromVoteTargets.
	mut sync.RWMutex

	logger log.Logger

	// Collaborators owned by the runtime.
	resolver PriceResolver
	client   ChainStateClient

	// Lifecycle state.
	mainCtx    context.Context
	mainCancel context.CancelFunc
	running    atomic.Bool

	// updateIntervalCh is created once and wakes the Start loop after an
	// UpdateInterval config change.
	updateIntervalCh chan struct{}

	// Config and provider state guarded by mut.
	cfg       Config
	providers map[string]*provider.Provider

	// Price aggregation state guarded by mut.
	priceSnapshot types.PriceSnapshot

	// Vote-target state guarded by mut. denoms is the effective denom snapshot
	// used for provider market filtering, price output, and missing-price metrics.
	denoms []string

	// denomsFromVoteTargets is false while denoms comes from fallback config;
	// once true, vote-target refresh failures preserve the last on-chain snapshot.
	denomsFromVoteTargets bool
}

// NewRuntime returns a new Runtime.
func NewRuntime(cfg Config, opts ...Option) (*Runtime, error) {
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

// GetPriceSnapshot returns the latest committed public price snapshot.
func (r *Runtime) GetPriceSnapshot() types.PriceSnapshot {
	r.mut.RLock()
	defer r.mut.RUnlock()

	prices := make(types.DenomPrices, len(r.denoms))
	for _, denom := range r.denoms {
		price, ok := r.priceSnapshot.Prices[denom]
		if !ok || price == nil {
			prices[denom] = new(big.Float)
			continue
		}
		prices[denom] = new(big.Float).Copy(price)
	}

	return types.PriceSnapshot{
		Prices:    prices,
		Timestamp: r.priceSnapshot.Timestamp,
	}
}

// getProviders returns a snapshot of provider pointers for runtime-owned lifecycle work.
func (r *Runtime) getProviders() map[string]*provider.Provider {
	r.mut.RLock()
	defer r.mut.RUnlock()

	providers := make(map[string]*provider.Provider, len(r.providers))
	maps.Copy(providers, r.providers)

	return providers
}
