package oracle

import (
	"fmt"
	"slices"

	"noah/oracle/providers"
	"noah/oracle/providers/base"
	"noah/oracle/providers/base/api"
	"noah/oracle/providers/base/websocket"
	providertypes "noah/oracle/providers/types"
)

// UpdateOracle applies a validated config replacement to the running oracle.
func (o *Oracle) UpdateOracle(cfg Config) error {
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

	plan, err := o.planUpdate(oldCfg, cfg, oldProviders)
	if err != nil {
		return err
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

// planUpdate builds the provider changes needed to move from oldCfg to newCfg.
func (o *Oracle) planUpdate(oldCfg, newCfg Config, oldProviders map[string]*base.Provider) (providerUpdatePlan, error) {
	plan := providerUpdatePlan{
		set: make(map[string]*base.Provider),
	}

	denomsSame := sameDenoms(newCfg.Denoms, oldCfg.Denoms)
	for name, oldProviderCfg := range oldCfg.Providers {
		provider := oldProviders[name]
		if provider == nil {
			return providerUpdatePlan{}, fmt.Errorf("provider %q missing from runtime state", name)
		}

		newProviderCfg, ok := newCfg.Providers[name]
		if !ok {
			plan.stop = append(plan.stop, provider)
			plan.remove = append(plan.remove, name)
			continue
		}

		switch {
		case !sameProviderConfig(oldProviderCfg, newProviderCfg):
			newProvider, err := providers.NewProvider(newProviderCfg, o.logger, newCfg.Denoms)
			if err != nil {
				return providerUpdatePlan{}, err
			}
			plan.stop = append(plan.stop, provider)
			plan.set[name] = newProvider
			plan.start = append(plan.start, newProvider)
		case !sameMarkets(oldProviderCfg.Markets, newProviderCfg.Markets) || !denomsSame:
			plan.updates = append(plan.updates, providerUpdate{
				provider: provider,
				options: []base.UpdateOption{
					base.WithNewDenoms(newCfg.Denoms),
					base.WithNewMarkets(newProviderCfg.Markets),
				},
			})
		}
	}

	for name, newProviderCfg := range newCfg.Providers {
		if _, ok := oldCfg.Providers[name]; ok {
			continue
		}

		newProvider, err := providers.NewProvider(newProviderCfg, o.logger, newCfg.Denoms)
		if err != nil {
			return providerUpdatePlan{}, err
		}
		plan.set[name] = newProvider
		plan.start = append(plan.start, newProvider)
	}

	return plan, nil
}

// sameProviderConfig reports whether two provider configs require the same runtime provider.
func sameProviderConfig(a, b providers.Config) bool {
	if a.Name != b.Name || a.Type != b.Type {
		return false
	}

	switch a.Type {
	case base.API:
		return sameAPIConfig(a.API, b.API)
	case base.WebSocket:
		return sameWebSocketConfig(a.WebSocket, b.WebSocket)
	default:
		return false
	}
}

// sameAPIConfig reports whether two API transport configs are equivalent.
func sameAPIConfig(a, b api.Config) bool {
	return a.Name == b.Name &&
		a.Timeout == b.Timeout &&
		a.Interval == b.Interval &&
		a.RequestsPerSecond == b.RequestsPerSecond &&
		slices.Equal(a.Endpoints, b.Endpoints) &&
		a.BatchSize == b.BatchSize &&
		a.MaxBlockHeightAge == b.MaxBlockHeightAge
}

// sameWebSocketConfig reports whether two WebSocket transport configs are equivalent.
func sameWebSocketConfig(a, b websocket.Config) bool {
	return a.Name == b.Name &&
		a.MaxBufferSize == b.MaxBufferSize &&
		a.ReconnectionTimeout == b.ReconnectionTimeout &&
		a.PostConnectionTimeout == b.PostConnectionTimeout &&
		slices.Equal(a.Endpoints, b.Endpoints) &&
		a.HandshakeTimeout == b.HandshakeTimeout &&
		a.EnableCompression == b.EnableCompression &&
		a.ReadTimeout == b.ReadTimeout &&
		a.WriteTimeout == b.WriteTimeout &&
		a.PingInterval == b.PingInterval &&
		a.WriteInterval == b.WriteInterval &&
		a.MaxReadErrorCount == b.MaxReadErrorCount &&
		a.MaxTickersPerConnection == b.MaxTickersPerConnection &&
		a.MaxSubscriptionsPerBatch == b.MaxSubscriptionsPerBatch
}

// sameDenoms reports whether two denom lists contain the same values.
func sameDenoms(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	counts := make(map[string]int, len(a))
	for _, denom := range a {
		counts[denom]++
	}
	for _, denom := range b {
		count := counts[denom]
		if count == 0 {
			return false
		}
		if count == 1 {
			delete(counts, denom)
		} else {
			counts[denom] = count - 1
		}
	}

	return len(counts) == 0
}

// sameMarkets reports whether two market lists contain the same values.
func sameMarkets(a, b providertypes.Markets) bool {
	if len(a) != len(b) {
		return false
	}

	counts := make(map[providertypes.Market]int, len(a))
	for _, market := range a {
		counts[market]++
	}
	for _, market := range b {
		count := counts[market]
		if count == 0 {
			return false
		}
		if count == 1 {
			delete(counts, market)
		} else {
			counts[market] = count - 1
		}
	}

	return len(counts) == 0
}
