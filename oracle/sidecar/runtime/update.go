package runtime

// Update validates and serialises a config replacement with aggregation and
// lifecycle transitions. Replacement providers are built before live state is
// changed, so validation or construction errors leave the runtime unchanged.
func (r *Runtime) Update(cfg Config) error {
	nextCfg := cfg.Clone()
	if err := nextCfg.Validate(); err != nil {
		return err
	}

	r.updateMu.Lock()
	defer r.updateMu.Unlock()

	r.mut.RLock()
	oldCfg := r.cfg
	oldDenoms := append([]string(nil), r.denoms...)
	nextDenoms := append([]string(nil), oldDenoms...)
	if !r.denomsFromVoteTargets {
		nextDenoms = append([]string(nil), nextCfg.FallbackDenoms...)
	}
	mainCtx := r.mainCtx
	mainCancel := r.mainCancel
	r.mut.RUnlock()

	plan, err := r.planProviders(oldCfg, nextCfg, oldDenoms, nextDenoms)
	if err != nil {
		return err
	}

	for _, managed := range plan.stop {
		managed.stop()
	}
	for _, update := range plan.markets {
		update.managed.provider.UpdateMarkets(update.markets)
	}
	r.providers = plan.next

	intervalChanged := oldCfg.UpdateInterval != nextCfg.UpdateInterval
	clientChanged := !oldCfg.Client.Equal(nextCfg.Client)

	r.mut.Lock()
	r.cfg = nextCfg
	r.denoms = append([]string(nil), nextDenoms...)
	r.mut.Unlock()

	if clientChanged {
		r.client.Update(nextCfg.Client)
	}

	if mainCtx == nil || mainCtx.Err() != nil {
		return nil
	}
	for _, managed := range plan.start {
		managed.start(mainCtx, mainCancel, r.logger)
	}
	if intervalChanged && r.updateIntervalCh != nil {
		select {
		case r.updateIntervalCh <- struct{}{}:
		default:
		}
	}
	return nil
}

// providerPlan describes the complete provider transition prepared while
// Runtime.updateMu is held.
type providerPlan struct {
	next    map[string]*managedProvider
	stop    []*managedProvider
	start   []*managedProvider
	markets []providerMarketUpdate
}

// planProviders builds the complete next provider set before any live provider
// is stopped, so a construction error leaves the runtime unchanged.
func (r *Runtime) planProviders(
	oldCfg Config,
	newCfg Config,
	oldDenoms []string,
	newDenoms []string,
) (providerPlan, error) {
	plan := providerPlan{
		next: make(map[string]*managedProvider, len(newCfg.Providers)),
	}

	oldPairs := oldCfg.Resolver.MarketPairs(oldDenoms)
	newPairs := newCfg.Resolver.MarketPairs(newDenoms)
	for name, newProviderCfg := range newCfg.Providers {
		current, exists := r.providers[name]
		newMarkets := newProviderCfg.Markets.FilterPairs(newPairs)
		if !exists {
			managed, err := r.newManagedProvider(newProviderCfg, newMarkets)
			if err != nil {
				return providerPlan{}, err
			}
			plan.next[name] = managed
			plan.start = append(plan.start, managed)
			continue
		}

		oldProviderCfg := oldCfg.Providers[name]
		if !oldProviderCfg.Equal(newProviderCfg) {
			managed, err := r.newManagedProvider(newProviderCfg, newMarkets)
			if err != nil {
				return providerPlan{}, err
			}
			plan.next[name] = managed
			plan.stop = append(plan.stop, current)
			plan.start = append(plan.start, managed)
			continue
		}

		plan.next[name] = current
		oldMarkets := oldProviderCfg.Markets.FilterPairs(oldPairs)
		if !oldMarkets.Equal(newMarkets) {
			plan.stop = append(plan.stop, current)
			plan.start = append(plan.start, current)
			plan.markets = append(plan.markets, providerMarketUpdate{
				managed: current,
				markets: newMarkets,
			})
		}
	}

	for name, current := range r.providers {
		if _, ok := plan.next[name]; !ok {
			plan.stop = append(plan.stop, current)
		}
	}

	return plan, nil
}
