package runtime

import (
	"context"
	"errors"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"cosmossdk.io/log/v2"

	"noah/oracle/sidecar/providers"
	provider "noah/oracle/sidecar/providers/base"
	"noah/oracle/sidecar/resolver"
	"noah/oracle/sidecar/types"
)

// Runtime runs price providers and exposes aggregated price state.
type Runtime struct {
	mut    sync.RWMutex
	logger log.Logger

	// Dependencies.
	resolver          PriceResolver
	voteTargetsClient VoteTargetsClient

	// Lifecycle.
	// mainCtx is cancelled when the oracle stops; providers and tick work derive from it.
	mainCtx    context.Context
	mainCancel context.CancelFunc
	// wg waits for provider goroutines started by the oracle.
	wg      sync.WaitGroup
	running atomic.Bool
	// updateIntervalCh notifies the running fetch loop that its ticker interval changed.
	updateIntervalCh chan struct{}

	// Runtime state guarded by mut.
	providers map[string]*provider.Provider
	// lastPriceSync is the last time the oracle successfully updated its prices.
	lastPriceSync time.Time
	// lastUpdated tracks the last block height associated with an oracle update.
	lastUpdated uint64
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
	o := &Runtime{
		cfg:              cfg,
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

	if len(o.providers) == 0 {
		for _, providerCfg := range runtimeProviderConfigs(o.cfg) {
			p, err := providers.NewProvider(providerCfg.cfg, providerCfg.markets, o.logger)
			if err != nil {
				return nil, err
			}

			o.providers[p.Name()] = p
		}
	}
	if len(o.cfg.FallbackDenoms) != 0 {
		o.denoms = append([]string(nil), o.cfg.FallbackDenoms...)
	}

	if o.resolver == nil {
		resolver, err := resolver.NewResolver(o.cfg.Resolver)
		if err != nil {
			return nil, err
		}
		o.resolver = resolver
	}

	return o, nil
}

// GetProviders returns a snapshot of provider pointers.
func (o *Runtime) GetProviders() map[string]*provider.Provider {
	o.mut.RLock()
	defer o.mut.RUnlock()

	providers := make(map[string]*provider.Provider, len(o.providers))
	for name, p := range o.providers {
		providers[name] = p
	}

	return providers
}

// GetLastSyncTime returns the last time the oracle aggregated provider prices.
func (o *Runtime) GetLastSyncTime() time.Time {
	o.mut.RLock()
	defer o.mut.RUnlock()
	return o.lastPriceSync
}

// GetPrices returns current aggregated prices keyed by public vote-target denom.
func (o *Runtime) GetPrices() types.DenomPrices {
	o.mut.RLock()
	denoms := append([]string(nil), o.denoms...)
	abstainDenoms := append([]string(nil), o.cfg.AbstainDenoms...)
	o.mut.RUnlock()

	prices := types.PricesByDenom(o.resolver.GetPrices(), denoms)
	if len(abstainDenoms) == 0 {
		return prices
	}

	activeDenoms := make(map[string]struct{}, len(denoms))
	for _, denom := range denoms {
		activeDenoms[denom] = struct{}{}
	}
	for _, denom := range abstainDenoms {
		if _, ok := activeDenoms[denom]; ok {
			prices[denom] = new(big.Float)
		}
	}

	return prices
}
