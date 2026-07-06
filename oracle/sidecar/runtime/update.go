package runtime

import (
	"context"
	"maps"

	"noah/oracle/sidecar/providers"
	"noah/oracle/sidecar/providers/base"
	providertypes "noah/oracle/sidecar/providers/types"
)

// Update applies a validated config replacement to the running oracle.
func (r *Runtime) Update(cfg Config) error {
	nextCfg := cfg.Clone()
	if err := nextCfg.Validate(); err != nil {
		return err
	}

	r.updateMu.Lock()
	defer r.updateMu.Unlock()

	plan, err := r.planConfigUpdate(nextCfg)
	if err != nil {
		return err
	}

	r.commitConfigUpdate(plan)
	r.applyConfigUpdateSideEffects(plan)
	return nil
}

type configUpdatePlan struct {
	new    Config
	denoms []string

	providers providerUpdatePlan

	running bool
	mainCtx context.Context

	intervalChanged bool
	resolverChanged bool
	clientChanged   bool
}

type providerUpdatePlan struct {
	remove  []string
	set     map[string]*base.Provider
	stop    []*base.Provider
	start   []*base.Provider
	updates []providerUpdate
}

type providerUpdate struct {
	provider *base.Provider
	markets  providertypes.Markets
}

func (r *Runtime) planConfigUpdate(newCfg Config) (configUpdatePlan, error) {
	r.mut.RLock()
	oldCfg := r.cfg.Clone()
	oldDenoms := append([]string(nil), r.denoms...)
	newDenoms := append([]string(nil), oldDenoms...)
	if !r.denomsFromVoteTargets {
		newDenoms = append([]string(nil), newCfg.FallbackDenoms...)
	}
	oldProviders := make(map[string]*base.Provider, len(r.providers))
	maps.Copy(oldProviders, r.providers)
	running := r.running.Load()
	mainCtx := r.mainCtx
	r.mut.RUnlock()

	providerPlan, err := r.planProviderUpdate(oldCfg, newCfg, oldDenoms, newDenoms, oldProviders)
	if err != nil {
		return configUpdatePlan{}, err
	}

	return configUpdatePlan{
		new:    newCfg,
		denoms: newDenoms,

		providers: providerPlan,

		running: running,
		mainCtx: mainCtx,

		intervalChanged: oldCfg.UpdateInterval != newCfg.UpdateInterval,
		resolverChanged: !oldCfg.Resolver.Equal(newCfg.Resolver),
		clientChanged:   !oldCfg.Client.Equal(newCfg.Client),
	}, nil
}

func (r *Runtime) commitConfigUpdate(plan configUpdatePlan) {
	r.mut.Lock()
	defer r.mut.Unlock()

	for _, update := range plan.providers.updates {
		update.provider.Update(update.markets)
	}
	for _, name := range plan.providers.remove {
		delete(r.providers, name)
	}
	maps.Copy(r.providers, plan.providers.set)

	if plan.resolverChanged {
		r.resolver.Update(plan.new.Resolver)
	}
	if plan.clientChanged {
		r.client.Update(plan.new.Client)
	}

	r.cfg = plan.new
	r.denoms = append([]string(nil), plan.denoms...)
}

func (r *Runtime) applyConfigUpdateSideEffects(plan configUpdatePlan) {
	for _, provider := range plan.providers.stop {
		provider.Stop()
	}
	if !plan.running || plan.mainCtx == nil {
		return
	}
	for _, provider := range plan.providers.start {
		if err := provider.Start(plan.mainCtx); err != nil {
			r.logProviderStartError(plan.mainCtx, provider.Name(), err)
		}
	}
	for _, update := range plan.providers.updates {
		if err := update.provider.Start(plan.mainCtx); err != nil {
			r.logProviderStartError(plan.mainCtx, update.provider.Name(), err)
		}
	}
	if plan.intervalChanged && r.updateIntervalCh != nil {
		select {
		case r.updateIntervalCh <- struct{}{}:
		default:
		}
	}
}

// planProviderUpdate builds the provider changes needed to move from oldCfg to newCfg.
func (r *Runtime) planProviderUpdate(
	oldCfg Config,
	newCfg Config,
	oldDenoms []string,
	newDenoms []string,
	oldProviders map[string]*base.Provider,
) (providerUpdatePlan, error) {
	plan := providerUpdatePlan{
		set: make(map[string]*base.Provider),
	}

	oldPairs := oldCfg.Resolver.MarketPairs(oldDenoms)
	newPairs := newCfg.Resolver.MarketPairs(newDenoms)
	for name, provider := range oldProviders {
		oldProviderCfg, oldOK := oldCfg.Providers[name]
		newProviderCfg, ok := newCfg.Providers[name]
		if !ok {
			plan.stop = append(plan.stop, provider)
			plan.remove = append(plan.remove, name)
			continue
		}

		oldMarkets := oldProviderCfg.Markets.FilterPairs(oldPairs)
		newMarkets := newProviderCfg.Markets.FilterPairs(newPairs)
		switch {
		case !oldOK || !oldProviderCfg.Equal(newProviderCfg):
			newProvider, err := providers.NewProvider(newProviderCfg, newMarkets, r.logger)
			if err != nil {
				return providerUpdatePlan{}, err
			}
			plan.stop = append(plan.stop, provider)
			plan.set[name] = newProvider
			plan.start = append(plan.start, newProvider)
		case !oldMarkets.Equal(newMarkets):
			plan.updates = append(plan.updates, providerUpdate{
				provider: provider,
				markets:  newMarkets,
			})
		}
	}

	for name, newProviderCfg := range newCfg.Providers {
		if _, ok := oldProviders[name]; ok {
			continue
		}

		newProvider, err := providers.NewProvider(newProviderCfg, newProviderCfg.Markets.FilterPairs(newPairs), r.logger)
		if err != nil {
			return providerUpdatePlan{}, err
		}
		plan.set[name] = newProvider
		plan.start = append(plan.start, newProvider)
	}

	return plan, nil
}
