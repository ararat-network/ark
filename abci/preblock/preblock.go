package preblock

import (
	"errors"
	"fmt"
	"time"

	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	arkmetrics "ark/abci/metrics"
	abcioracle "ark/abci/oracle"
	oraclemetrics "ark/abci/oracle/metrics"
	arkabcitypes "ark/abci/types"
	"ark/abci/voteextension"
)

// Handler is responsible for aggregating oracle data from each
// validator and writing the oracle data into the store before any transactions
// are executed/finalised for a given block.
type Handler struct {
	// oracleKeeper provides the oracle state used during preblock processing.
	oracleKeeper arkabcitypes.OracleKeeper
}

// NewHandler returns a new Handler. The handler
// is responsible for writing oracle data included in vote extensions to state.
func NewHandler(oracleKeeper arkabcitypes.OracleKeeper) *Handler {
	return &Handler{oracleKeeper: oracleKeeper}
}

// WrappedPreBlocker is called by the base app before the block is finalised. It
// is responsible for calling the module manager's PreBlock method, aggregating oracle data from each validator and
// writing the oracle data to the store.
func (h *Handler) WrappedPreBlocker(mm *module.Manager) sdk.PreBlocker {
	return func(ctx sdk.Context, req *cometabci.RequestFinalizeBlock) (response *sdk.ResponsePreBlock, err error) {
		start := time.Now()
		var (
			prices                 map[string]math.LegacyDec
			wrappedPreBlockLatency time.Duration
		)
		defer func() {
			// only measure latency in Finalise, excluding the wrapped module
			// manager preblockers
			if ctx.ExecMode() == sdk.ExecModeFinalize {
				latency := time.Since(start) - wrappedPreBlockLatency
				arkmetrics.RecordLatencyAndStatus(latency, preblockStatus(err), arkmetrics.PreBlock)

				// Record prices only if they were written successfully.
				if err == nil && prices != nil {
					for denom, price := range prices {
						floatPrice, _ := price.Float64()
						oraclemetrics.ObservePriceForTicker(denom, floatPrice)
					}
				}
			}
		}()

		if req == nil {
			return &sdk.ResponsePreBlock{}, fmt.Errorf("%w for %s", arkabcitypes.ErrNilRequest, arkmetrics.PreBlock)
		}

		// call module manager's PreBlocker first in case there is changes made on upgrades
		// that can modify state and lead to serialisation/deserialisation issues
		wrappedStart := time.Now()
		response, err = mm.PreBlock(ctx)
		wrappedPreBlockLatency = time.Since(wrappedStart)
		if err != nil {
			return response, fmt.Errorf("%w for %s: %w", arkabcitypes.ErrWrappedHandler, arkmetrics.PreBlock, err)
		}

		if voteextension.VoteExtensionsAvailable(ctx) {
			// Decode vote extensions and apply prices to state. This must run
			// before AdvanceFeeds: the injected votes were signed for height
			// req.Height-1 and validate against the feed epoch
			// AtHeight(req.Height-1), which an advance due at req.Height folds
			// away. Advancing first would reject every report with a version
			// mismatch exactly at an activation height. This ordering is a
			// consensus invariant, not an implementation detail.
			prices, err = abcioracle.ProcessVoteExtensions(ctx, h.oracleKeeper, req)
			if err != nil {
				return response, err
			}
		}

		err = h.oracleKeeper.AdvanceFeeds(ctx)
		if err != nil {
			return response, fmt.Errorf(
				"%w: advance feeds for height %d: %w",
				arkabcitypes.ErrOracleKeeper,
				req.Height,
				err,
			)
		}

		// Prices and feeds for the block are final here. Consumers that derive
		// block-local state from them — Treasury's liability snapshot among
		// them — do so in their own BeginBlocker, which the ABCI lifecycle
		// already sequences after every PreBlocker.
		return response, nil
	}
}

func preblockStatus(err error) arkmetrics.Status {
	switch {
	case err == nil:
		return arkmetrics.StatusSuccess
	case errors.Is(err, arkabcitypes.ErrNilRequest):
		return arkmetrics.StatusNilRequest
	case errors.Is(err, arkabcitypes.ErrWrappedHandler):
		return arkmetrics.StatusWrappedHandler
	case errors.Is(err, arkabcitypes.ErrOracleKeeper):
		return arkmetrics.StatusOracleKeeper
	case errors.Is(err, arkabcitypes.ErrCodec):
		return arkmetrics.StatusCodec
	case errors.Is(err, arkabcitypes.ErrMissingCommitInfo):
		return arkmetrics.StatusMissingCommitInfo
	default:
		return arkmetrics.StatusFailure
	}
}
