package preblock

import (
	"errors"
	"fmt"
	"time"

	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"ark/abci/codec"
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

	// codec owns reusable vote-extension decompression state.
	codec *codec.VoteExtensionCodec
}

// NewHandler returns a new Handler. The handler
// is responsible for writing oracle data included in vote extensions to state.
func NewHandler(
	oracleKeeper arkabcitypes.OracleKeeper,
	voteExtensionCodec *codec.VoteExtensionCodec,
) *Handler {
	return &Handler{
		oracleKeeper: oracleKeeper,
		codec:        voteExtensionCodec,
	}
}

// WrappedPreBlocker is called by the base app before the block is finalised. It
// is responsible for calling the module manager's PreBlock method, aggregating oracle data from each validator and
// writing the oracle data to the store.
func (h *Handler) WrappedPreBlocker(mm *module.Manager) sdk.PreBlocker {
	return func(ctx sdk.Context, req *cometabci.RequestFinalizeBlock) (response *sdk.ResponsePreBlock, err error) {
		if req == nil {
			return &sdk.ResponsePreBlock{}, fmt.Errorf("%w for %s", arkabcitypes.ErrNilRequest, arkmetrics.PreBlock)
		}

		// call module manager's PreBlocker first in case there is changes made on upgrades
		// that can modify state and lead to serialisation/deserialisation issues
		response, err = mm.PreBlock(ctx)
		if err != nil {
			return response, fmt.Errorf("%w for %s: %w", arkabcitypes.ErrWrappedHandler, arkmetrics.PreBlock, err)
		}

		start := time.Now()
		var prices map[string]math.LegacyDec
		defer func() {
			// only measure latency in Finalise
			if ctx.ExecMode() == sdk.ExecModeFinalize {
				latency := time.Since(start)
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

		if voteextension.VoteExtensionsAvailable(ctx) {
			// Decode vote extensions and apply prices to state.
			prices, err = abcioracle.ProcessVoteExtensions(ctx, h.oracleKeeper, h.codec, req)
			if err != nil {
				return response, err
			}
		}

		err = h.oracleKeeper.AdvanceVoteTargets(ctx)
		if err != nil {
			return response, fmt.Errorf(
				"%w: advance vote targets for height %d: %w",
				arkabcitypes.ErrOracleKeeper,
				req.Height,
				err,
			)
		}

		return response, nil
	}
}

func preblockStatus(err error) arkmetrics.Status {
	switch {
	case err == nil:
		return arkmetrics.StatusSuccess
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
