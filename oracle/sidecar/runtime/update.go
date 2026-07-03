package runtime

import (
	"fmt"

	"noah/oracle/sidecar/providers"
	"noah/oracle/sidecar/providers/base"
	providertypes "noah/oracle/sidecar/providers/types"
)

// UpdateConfig applies a validated config replacement to the running oracle.
func (o *Runtime) UpdateConfig(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}

	o.mut.RLock()
	oldCfg := o.cfg
	oldProviders := make(map[string]*base.Provider, len(o.providers))
	for name, provider := range o.providers {
		oldProviders[name] = provider
	}
	running := o.running.Load()
	mainCtx := o.mainCtx
	o.mut.RUnlock()
	intervalChanged := oldCfg.UpdateInterval != cfg.UpdateInterval
	resolverChanged := !oldCfg.Resolver.Equal(cfg.Resolver)

	plan, err := o.planUpdate(oldCfg, cfg, oldProviders)
	if err != nil {
		return err
	}
	if resolverChanged {
		if err := o.resolver.UpdateConfig(cfg.Resolver); err != nil {
			return err
		}
	}

	o.mut.Lock()
	for _, update := range plan.updates {
		if err := update.provider.Update(update.options...); err != nil {
			o.mut.Unlock()
			return err
		}
	}
	for _, name := range plan.remove {
		delete(o.providers, name)
	}
	for name, provider := range plan.set {
		o.providers[name] = provider
	}
	o.cfg = cfg
	o.mut.Unlock()

	for _, provider := range plan.stop {
		provider.Stop()
	}
	if running && mainCtx != nil {
		for _, provider := range plan.start {
			o.startProvider(mainCtx, provider)
		}
		if intervalChanged {
			o.notifyUpdateInterval()
		}
	}

	return nil
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
	options  []base.UpdateOption
}

type runtimeProviderConfig struct {
	cfg     providers.Config
	markets providertypes.Markets
}

func runtimeProviderConfigs(cfg Config) map[string]runtimeProviderConfig {
	pairs := cfg.Resolver.MarketPairs()
	runtimeProviders := make(map[string]runtimeProviderConfig, len(cfg.Providers))
	for name, providerCfg := range cfg.Providers {
		markets := providerCfg.Markets.FilterPairs(pairs)
		if len(markets) == 0 {
			continue
		}

		runtimeProviders[name] = runtimeProviderConfig{
			cfg:     providerCfg,
			markets: markets,
		}
	}

	return runtimeProviders
}

// planUpdate builds the provider changes needed to move from oldCfg to newCfg.
func (o *Runtime) planUpdate(oldCfg, newCfg Config, oldProviders map[string]*base.Provider) (providerUpdatePlan, error) {
	plan := providerUpdatePlan{
		set: make(map[string]*base.Provider),
	}

	oldRuntimeProviders := runtimeProviderConfigs(oldCfg)
	newRuntimeProviders := runtimeProviderConfigs(newCfg)
	for name, oldProviderCfg := range oldRuntimeProviders {
		provider := oldProviders[name]
		if provider == nil {
			return providerUpdatePlan{}, fmt.Errorf("provider %q missing from runtime state", name)
		}

		newProviderCfg, ok := newRuntimeProviders[name]
		if !ok {
			plan.stop = append(plan.stop, provider)
			plan.remove = append(plan.remove, name)
			continue
		}

		switch {
		case !oldProviderCfg.cfg.Equal(newProviderCfg.cfg):
			newProvider, err := providers.NewProvider(newProviderCfg.cfg, newProviderCfg.markets, o.logger)
			if err != nil {
				return providerUpdatePlan{}, err
			}
			plan.stop = append(plan.stop, provider)
			plan.set[name] = newProvider
			plan.start = append(plan.start, newProvider)
		case !oldProviderCfg.markets.Equal(newProviderCfg.markets):
			plan.updates = append(plan.updates, providerUpdate{
				provider: provider,
				options: []base.UpdateOption{
					base.WithNewMarkets(newProviderCfg.markets),
				},
			})
		}
	}

	for name, newProviderCfg := range newRuntimeProviders {
		if _, ok := oldRuntimeProviders[name]; ok {
			continue
		}

		newProvider, err := providers.NewProvider(newProviderCfg.cfg, newProviderCfg.markets, o.logger)
		if err != nil {
			return providerUpdatePlan{}, err
		}
		plan.set[name] = newProvider
		plan.start = append(plan.start, newProvider)
	}

	return plan, nil
}
