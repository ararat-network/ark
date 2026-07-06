package runtime

import (
	"context"
	"errors"
	"time"
)

// Start starts the blocking oracle lifecycle and price fetch loop.
func (r *Runtime) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}

	r.logger.Info("starting oracle")
	r.running.Store(true)
	defer r.running.Store(false)

	mainCtx, mainCancel := context.WithCancel(ctx)
	defer mainCancel()
	r.setMainCtx(mainCtx, mainCancel)

	if client := r.getClient(); client != nil {
		if err := client.Start(mainCtx); err != nil {
			if mainCtx.Err() == nil && !errors.Is(err, context.Canceled) {
				r.logger.Error("failed to start vote-target client", "error", err)
			}
		}
	}
	r.startProviders(mainCtx)

	ticker := time.NewTicker(r.getUpdateInterval())
	defer ticker.Stop()

	for {
		select {
		case <-mainCtx.Done():
			r.Stop()
			r.logger.Info("oracle stopped via context")
			return mainCtx.Err()
		case <-r.updateIntervalCh:
			ticker.Reset(r.getUpdateInterval())
		case <-ticker.C:
			r.fetchAllPrices(mainCtx)
		}
	}
}

// Stop stops the oracle. This is a synchronous operation that will
// wait for all providers to exit.
func (r *Runtime) Stop() {
	r.updateMu.Lock()
	defer r.updateMu.Unlock()

	r.logger.Info("stopping oracle")
	if _, cancel := r.getMainCtx(); cancel != nil {
		r.logger.Info("cancelling context")
		cancel()
	}
	if client := r.getClient(); client != nil {
		client.Stop()
	}
	for _, provider := range r.GetProviders() {
		provider.Stop()
	}

	r.logger.Info("oracle exited successfully")
}

// IsRunning returns true while Start is running.
func (r *Runtime) IsRunning() bool { return r.running.Load() }

func (r *Runtime) startProviders(ctx context.Context) {
	r.updateMu.Lock()
	defer r.updateMu.Unlock()

	for _, provider := range r.GetProviders() {
		if err := provider.Start(ctx); err != nil {
			r.logProviderStartError(ctx, provider.Name(), err)
		}
	}
}

func (r *Runtime) logProviderStartError(ctx context.Context, name string, err error) {
	if err == nil {
		return
	}
	if ctx != nil && ctx.Err() != nil {
		return
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	r.logger.Error("failed to start provider", "provider", name, "error", err)
}

// getMainCtx returns the main context for the oracle.
func (r *Runtime) getMainCtx() (context.Context, context.CancelFunc) {
	r.mut.RLock()
	defer r.mut.RUnlock()

	return r.mainCtx, r.mainCancel
}

// setMainCtx sets the main context for the oracle.
func (r *Runtime) setMainCtx(ctx context.Context, cancel context.CancelFunc) {
	r.mut.Lock()
	defer r.mut.Unlock()

	r.mainCtx, r.mainCancel = ctx, cancel
}

func (r *Runtime) getClient() ChainStateClient {
	r.mut.RLock()
	defer r.mut.RUnlock()

	return r.client
}

// getUpdateInterval returns the current price fetch interval.
func (r *Runtime) getUpdateInterval() time.Duration {
	r.mut.RLock()
	defer r.mut.RUnlock()

	return r.cfg.UpdateInterval
}
