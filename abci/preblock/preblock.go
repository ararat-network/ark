package preblock

import (
	"errors"
	"fmt"
	"time"

	cmtabci "github.com/cometbft/cometbft/abci/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	abcimetrics "github.com/ararat-network/ark/abci/metrics"
	abcioracle "github.com/ararat-network/ark/abci/oracle"
	abcitypes "github.com/ararat-network/ark/abci/types"
	"github.com/ararat-network/ark/abci/voteextension"
)

// Handler is responsible for aggregating oracle data from each
// validator and writing the oracle data into the store before any transactions
// are executed/finalised for a given block.
type Handler struct {
	// oracleKeeper provides the oracle state used during preblock processing.
	oracleKeeper abcitypes.OracleKeeper
}

// NewHandler returns a new Handler. The handler
// is responsible for writing oracle data included in vote extensions to state.
func NewHandler(oracleKeeper abcitypes.OracleKeeper) *Handler {
	return &Handler{oracleKeeper: oracleKeeper}
}

// WrappedPreBlocker is called by the base app before the block is finalised. It
// is responsible for calling the module manager's PreBlock method, aggregating oracle data from each validator and
// writing the oracle data to the store.
func (h *Handler) WrappedPreBlocker(mm *module.Manager) sdk.PreBlocker {
	return func(ctx sdk.Context, req *cmtabci.RequestFinalizeBlock) (response *sdk.ResponsePreBlock, err error) {
		start := time.Now()
		completed := false
		var wrappedPreBlockLatency time.Duration
		defer func() {
			// only measure latency in Finalise, excluding the wrapped module
			// manager preblockers
			if ctx.ExecMode() == sdk.ExecModeFinalize {
				latency := time.Since(start) - wrappedPreBlockLatency
				status := preblockStatus(err)
				if !completed {
					status = abcimetrics.StatusPanic
				}
				abcimetrics.RecordLatencyAndStatus(latency, status, abcimetrics.PreBlock)
			}
		}()

		response, err = func() (response *sdk.ResponsePreBlock, err error) {
			if req == nil {
				return &sdk.ResponsePreBlock{}, fmt.Errorf("%w for %s", abcitypes.ErrNilRequest, abcimetrics.PreBlock)
			}

			// Apply module upgrades before reading oracle state under its current schema.
			wrappedStart := time.Now()
			response, err = mm.PreBlock(ctx)
			wrappedPreBlockLatency = time.Since(wrappedStart)
			if err != nil {
				return response, fmt.Errorf("%w for %s: %w", abcitypes.ErrWrappedHandler, abcimetrics.PreBlock, err)
			}

			if voteextension.VoteExtensionsAvailable(ctx) {
				// Consume the previous height's reports before AdvanceFeeds discards
				// their signing epoch. See abci/README.md, "Preblock order".
				err = abcioracle.ProcessVoteExtensions(ctx, h.oracleKeeper, req)
				if err != nil {
					return response, err
				}
			}

			err = h.oracleKeeper.AdvanceFeeds(ctx)
			if err != nil {
				return response, fmt.Errorf(
					"%w: advance feeds for height %d: %w",
					abcitypes.ErrOracleKeeper,
					req.Height,
					err,
				)
			}

			// Prices and feeds for the block are final here. Consumers that derive
			// block-local state from them — Treasury's liability snapshot among
			// them — do so in their own BeginBlocker, which the ABCI lifecycle
			// already sequences after every PreBlocker.
			return response, nil
		}()
		completed = true
		return response, err
	}
}

func preblockStatus(err error) abcimetrics.Status {
	switch {
	case err == nil:
		return abcimetrics.StatusSuccess
	case errors.Is(err, abcitypes.ErrNilRequest):
		return abcimetrics.StatusNilRequest
	case errors.Is(err, abcitypes.ErrWrappedHandler):
		return abcimetrics.StatusWrappedHandler
	case errors.Is(err, abcitypes.ErrOracleKeeper):
		return abcimetrics.StatusOracleKeeper
	case errors.Is(err, abcitypes.ErrCodec):
		return abcimetrics.StatusCodec
	case errors.Is(err, abcitypes.ErrMissingCommitInfo):
		return abcimetrics.StatusMissingCommitInfo
	default:
		return abcimetrics.StatusFailure
	}
}
