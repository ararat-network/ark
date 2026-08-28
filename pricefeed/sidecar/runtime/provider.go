package runtime

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/log/v2"

	sidecarinternal "ark/pricefeed/sidecar/internal"
	"ark/pricefeed/sidecar/providers"
	provider "ark/pricefeed/sidecar/providers/base"
	providertypes "ark/pricefeed/sidecar/providers/types"
)

// managedProvider keeps provider execution state in the runtime that owns its
// lifecycle. cancel and done are only changed while Runtime.updateMu is held.
type managedProvider struct {
	provider *provider.Provider
	cancel   context.CancelFunc
	done     chan struct{}
}

// providerMarketUpdate retargets one retained provider without rebuilding its
// transport configuration.
type providerMarketUpdate struct {
	managed *managedProvider
	markets providertypes.Markets
}

// newManagedProvider constructs a provider and verifies that the factory
// preserved the configured identity before the provider enters runtime state.
func (r *Runtime) newManagedProvider(
	cfg providers.Config,
	markets providertypes.Markets,
) (*managedProvider, error) {
	built, err := r.providerFactory(cfg, markets)
	if err != nil {
		return nil, err
	}
	if built == nil {
		return nil, fmt.Errorf("provider factory returned nil for %q", cfg.Name)
	}
	if built.Name() != cfg.Name {
		return nil, fmt.Errorf(
			"provider factory returned name %q for config %q",
			built.Name(),
			cfg.Name,
		)
	}

	return &managedProvider{provider: built}, nil
}

// start launches one provider run. The caller holds Runtime.updateMu, which
// serialises this transition with stop.
func (p *managedProvider) start(
	ctx context.Context,
	cancelMain context.CancelCauseFunc,
	logger log.Logger,
) {
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	p.cancel = cancel
	p.done = done

	logger.Info("starting provider", "provider", p.provider.Name())
	go func() {
		defer close(done)
		defer cancel()

		err := sidecarinternal.RunRecovering("provider run loop", func() error {
			return p.provider.Run(runCtx)
		})
		if sidecarinternal.IsPanic(err) {
			logger.Error(
				"provider panicked",
				"provider", p.provider.Name(),
				"error", err,
			)
			if cancelMain != nil {
				cancelMain(err)
			}
			return
		}
		if runCtx.Err() != nil || errors.Is(err, context.Canceled) {
			return
		}
		if err != nil {
			logger.Error(
				"provider exited",
				"provider", p.provider.Name(),
				"error", err,
			)
			return
		}
		logger.Warn("provider exited without error", "provider", p.provider.Name())
	}()
}

// stop cancels one provider run and waits for cleanup to finish. The caller
// holds Runtime.updateMu, so no second run can start during the wait.
func (p *managedProvider) stop() {
	if p.done == nil {
		return
	}

	if p.cancel != nil {
		p.cancel()
	}
	<-p.done
	p.cancel = nil
	p.done = nil
}
