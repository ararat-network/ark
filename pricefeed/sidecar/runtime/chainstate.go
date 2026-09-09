package runtime

import (
	"context"
	"errors"

	"github.com/ararat-network/ark/pricefeed/sidecar/chainstate"
	sidecarinternal "github.com/ararat-network/ark/pricefeed/sidecar/internal"
)

var errChainStateClientExited = errors.New("chain state client exited without error")

// ChainStateClient fetches the current on-chain oracle feeds.
type ChainStateClient interface {
	Run(context.Context) error
	Update(chainstate.Config) error
	Feeds() ([]string, error)
}

// startClient runs the runtime-owned chainstate client behind a panic boundary.
// An unexpected exit cancels the main runtime, and the returned channel closes
// only after the client goroutine has completed cleanup.
func (r *Runtime) startClient(ctx context.Context, cancel context.CancelCauseFunc) <-chan struct{} {
	client := r.client
	if client == nil {
		return nil
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		err := sidecarinternal.RunRecovering("chain state client", func() error {
			return client.Run(ctx)
		})
		if ctx.Err() != nil && (err == nil || errors.Is(err, context.Canceled)) {
			return
		}
		if err == nil {
			err = errChainStateClientExited
		}

		r.logger.Error("chain state client exited", "error", err)
		cancel(err)
	}()
	return done
}
