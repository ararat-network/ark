package runtime

import (
	"context"
	"errors"
	"time"

	sidecarmetrics "github.com/ararat-network/ark/pricefeed/sidecar/metrics"
)

// Run is a blocking, single-use lifecycle call. It starts runtime-owned
// providers and chainstate polling, runs price aggregation until cancellation
// or a fatal child failure, and waits for collaborator cleanup before returning.
func (r *Runtime) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}

	r.logger.Info("starting oracle")

	mainCtx, cancelCause := context.WithCancelCause(ctx)
	var clientDone <-chan struct{}
	r.updateMu.Lock()
	r.initialiseMetrics(mainCtx)
	clientDone = r.startClient(mainCtx, cancelCause)
	for _, managed := range r.providers {
		managed.start(mainCtx, cancelCause, r.logger)
	}
	r.mut.Lock()
	r.mainCtx = mainCtx
	r.mainCancel = cancelCause
	r.mut.Unlock()
	r.updateMu.Unlock()

	defer r.shutdown(cancelCause, clientDone)

	ticker := time.NewTicker(r.getUpdateInterval())
	defer ticker.Stop()

	for {
		select {
		case <-mainCtx.Done():
			cause := context.Cause(mainCtx)
			r.logger.Info("oracle stopped via context", "error", cause)
			return cause
		case <-r.updateIntervalCh:
			ticker.Reset(r.getUpdateInterval())
		case <-ticker.C:
			r.updatePriceSnapshot(mainCtx)
		}
	}
}

// IsRunning returns true while Run is active.
func (r *Runtime) IsRunning() bool {
	r.mut.RLock()
	defer r.mut.RUnlock()

	return r.mainCtx != nil
}

// shutdown stops runtime-owned collaborators before Run returns.
func (r *Runtime) shutdown(cancel context.CancelCauseFunc, clientDone <-chan struct{}) {
	r.updateMu.Lock()
	defer r.updateMu.Unlock()

	r.logger.Info("stopping oracle")
	r.logger.Info("cancelling context")
	cancel(context.Canceled)
	for _, managed := range r.providers {
		managed.stop()
	}
	if clientDone != nil {
		<-clientDone
	}

	r.mut.Lock()
	r.mainCtx = nil
	r.mainCancel = nil
	r.mut.Unlock()

	sidecarmetrics.PublishAggregationSnapshot(sidecarmetrics.AggregationSnapshot{})
	r.logger.Info("oracle exited successfully")
}

// getUpdateInterval returns the current price aggregation interval.
func (r *Runtime) getUpdateInterval() time.Duration {
	r.mut.RLock()
	defer r.mut.RUnlock()

	return r.cfg.UpdateInterval
}

// initialiseMetrics is called with updateMu held after a config/feed change.
func (r *Runtime) initialiseMetrics(ctx context.Context) {
	pairs := r.cfg.Resolver.MarketPairs(r.feeds)
	names := make([]string, 0, len(pairs))
	for pair := range pairs {
		names = append(names, pair.String())
	}
	sidecarmetrics.Initialise(ctx, r.feeds, names)
}
