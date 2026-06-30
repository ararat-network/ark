package oracle

import (
	"context"
	"errors"
	"time"

	"noah/oracle/providers/base"
)

// Start starts the blocking oracle lifecycle and price fetch loop.
func (o *Oracle) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}

	o.logger.Info("starting oracle")
	o.running.Store(true)
	defer o.running.Store(false)

	mainCtx, mainCancel := context.WithCancel(ctx)
	defer mainCancel()
	o.setMainCtx(mainCtx, mainCancel)

	// Start all configured price providers.
	for _, provider := range o.providers {
		o.startProvider(mainCtx, provider)
	}

	ticker := time.NewTicker(o.getUpdateInterval())
	defer ticker.Stop()

	for {
		select {
		case <-mainCtx.Done():
			o.Stop()
			o.logger.Info("oracle stopped via context")
			return mainCtx.Err()
		case <-o.updateIntervalCh:
			ticker.Reset(o.getUpdateInterval())
		case <-ticker.C:
			o.fetchAllPrices()
		}
	}
}

// Stop stops the oracle. This is a synchronous operation that will
// wait for all providers to exit.
func (o *Oracle) Stop() {
	o.logger.Info("stopping oracle")
	if _, cancel := o.getMainCtx(); cancel != nil {
		o.logger.Info("cancelling context")
		cancel()
	}

	o.logger.Info("waiting for routines to stop")
	o.wg.Wait()
	o.logger.Info("oracle exited successfully")
}

// IsRunning returns true while Start is running.
func (o *Oracle) IsRunning() bool { return o.running.Load() }

// startProvider runs a provider under the oracle wait group and records unexpected failures.
func (o *Oracle) startProvider(ctx context.Context, provider *base.Provider) {
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				o.logger.Error(
					"provider panicked",
					"provider", provider.Name(),
					"error", r,
				)
			}
		}()

		if ctx == nil {
			o.logger.Error("main context is nil; cannot start provider", "provider", provider.Name())
			return
		}

		if err := provider.Start(ctx); err != nil {
			o.logger.Error("provider exited", "provider", provider.Name(), "error", err)
		}
	}()
}

// getMainCtx returns the main context for the oracle.
func (o *Oracle) getMainCtx() (context.Context, context.CancelFunc) {
	o.mut.RLock()
	defer o.mut.RUnlock()

	return o.mainCtx, o.mainCancel
}

// setMainCtx sets the main context for the oracle.
func (o *Oracle) setMainCtx(ctx context.Context, cancel context.CancelFunc) {
	o.mut.Lock()
	defer o.mut.Unlock()

	o.mainCtx, o.mainCancel = ctx, cancel
}

// getUpdateInterval returns the current price fetch interval.
func (o *Oracle) getUpdateInterval() time.Duration {
	o.mut.RLock()
	defer o.mut.RUnlock()

	return o.cfg.UpdateInterval
}

// notifyUpdateInterval coalesces update-interval changes for the running fetch loop.
func (o *Oracle) notifyUpdateInterval() {
	if o.updateIntervalCh == nil {
		return
	}

	select {
	case o.updateIntervalCh <- struct{}{}:
		return
	default:
	}

	select {
	case <-o.updateIntervalCh:
	default:
	}

	select {
	case o.updateIntervalCh <- struct{}{}:
	default:
	}
}
