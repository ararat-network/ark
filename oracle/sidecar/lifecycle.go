package sidecar

import (
	"context"
	"net/http"

	gatewayruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
	"google.golang.org/grpc"

	"noah/oracle/sidecar/runtime"
)

// Update applies a validated runtime config replacement.
//
// Closing and closed sidecars reject updates before delegating to runtime so a
// config commit cannot race with transport teardown.
func (o *Oracle) Update(cfg runtime.Config) error {
	o.lifecycleMu.Lock()
	if o.closing || o.closed {
		o.lifecycleMu.Unlock()
		return errOracleClosed
	}
	o.lifecycleMu.Unlock()

	return o.runtime.Update(cfg)
}

// Close requests shutdown of the runtime and transport stack.
//
// Close is idempotent. It cancels the run context first, then shuts down the
// HTTP/gRPC transport. Done is closed by markDone after StartWithListener has
// returned, except for never-started instances where Close owns that transition.
func (o *Oracle) Close() error {
	o.closeOnce.Do(func() {
		o.lifecycleMu.Lock()
		cancel := o.cancel
		unstarted := !o.closed && o.cancel == nil && o.httpSrv == nil && o.grpcSrv == nil
		o.closing = true
		o.lifecycleMu.Unlock()

		if cancel != nil {
			cancel()
		}
		o.closeErr = o.shutdownTransport()
		if unstarted {
			o.markDone()
		}
	})
	return o.closeErr
}

// Done returns a channel closed after runtime and transport shutdown completes.
func (o *Oracle) Done() <-chan struct{} {
	return o.doneCh
}

// startLifecycle commits a newly built transport stack to the process state.
//
// Callers build listeners and servers first, then publish them here atomically.
// A closing or already-started sidecar rejects the commit so shutdown and start
// cannot own the same transport pointers concurrently.
func (o *Oracle) startLifecycle(
	cancel context.CancelFunc,
	httpSrv *http.Server,
	grpcSrv *grpc.Server,
	gatewayMux *gatewayruntime.ServeMux,
) error {
	o.lifecycleMu.Lock()
	defer o.lifecycleMu.Unlock()

	if o.closing || o.closed {
		return errOracleClosed
	}
	if o.cancel != nil {
		return errOracleAlreadyStarted
	}

	o.cancel = cancel
	o.httpSrv = httpSrv
	o.grpcSrv = grpcSrv
	o.gatewayMux = gatewayMux
	return nil
}

// shutdownTransport snapshots transport pointers and performs blocking shutdown
// outside lifecycleMu so concurrent Close callers cannot hold the lifecycle lock
// while HTTP shutdown waits on in-flight requests.
func (o *Oracle) shutdownTransport() error {
	o.lifecycleMu.Lock()
	httpSrv := o.httpSrv
	grpcSrv := o.grpcSrv
	o.lifecycleMu.Unlock()

	var err error
	if httpSrv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), DefaultServerShutdownTimeout)
		err = httpSrv.Shutdown(ctx)
		cancel()
	}
	if grpcSrv != nil {
		grpcSrv.Stop()
	}
	return err
}

// markDone publishes terminal lifecycle state and closes Done exactly once.
func (o *Oracle) markDone() {
	o.doneOnce.Do(func() {
		o.lifecycleMu.Lock()
		o.cancel = nil
		o.closed = true
		o.closing = false
		o.lifecycleMu.Unlock()
		close(o.doneCh)
	})
}
