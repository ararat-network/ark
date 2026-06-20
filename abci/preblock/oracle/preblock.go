package oracle

import (
	"fmt"
	"time"

	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"noah/abci/codec"
	noahmetrics "noah/abci/metrics"
	abcioracle "noah/abci/oracle"
	noahabcitypes "noah/abci/types"
	"noah/abci/ve"
)

// PreBlockHandler is responsible for aggregating oracle data from each
// validator and writing the oracle data into the store before any transactions
// are executed/finalised for a given block.
type PreBlockHandler struct { //golint:ignore
	logger log.Logger

	// ok is the ok for the oracle module. This is utilised to write
	// oracle data to state.
	ok noahabcitypes.OracleKeeper

	// pa is the price applier that is used to decode vote-extensions, aggregate price reports, and write prices to state.
	pa *abcioracle.PriceApplier
}

// NewOraclePreBlockHandler returns a new PreBlockHandler. The handler
// is responsible for writing oracle data included in vote extensions to state.
func NewOraclePreBlockHandler(
	logger log.Logger,
	oracleKeeper noahabcitypes.OracleKeeper,
	veCodec codec.VoteExtensionCodec,
	ecCodec codec.ExtendedCommitCodec,
) *PreBlockHandler {
	va := abcioracle.NewVoteAggregator(
		logger,
	)
	pa := abcioracle.NewPriceApplier(
		va,
		oracleKeeper,
		veCodec,
		ecCodec,
		logger,
	)

	return &PreBlockHandler{
		logger: logger,
		ok:     oracleKeeper,
		pa:     pa,
	}
}

// WrappedPreBlocker is called by the base app before the block is finalised. It
// is responsible for calling the module manager's PreBlock method, aggregating oracle data from each validator and
// writing the oracle data to the store.
func (h *PreBlockHandler) WrappedPreBlocker(mm *module.Manager) sdk.PreBlocker {
	return func(ctx sdk.Context, req *cometabci.RequestFinalizeBlock) (response *sdk.ResponsePreBlock, err error) {
		if req == nil {
			h.logger.Error(
				"received nil RequestFinalizeBlock in oracle preblocker",
				"height", ctx.BlockHeight(),
			)

			return &sdk.ResponsePreBlock{}, fmt.Errorf("received nil RequestFinalizeBlock in oracle preblocker: height %d", ctx.BlockHeight())
		}

		// call module manager's PreBlocker first in case there is changes made on upgrades
		// that can modify state and lead to serialisation/deserialisation issues
		response, err = mm.PreBlock(ctx)
		if err != nil {
			return response, err
		}

		start := time.Now()
		var prices map[string]math.LegacyDec
		var voteTargets map[string]math.LegacyDec
		defer func() {
			// only measure latency in Finalise
			if ctx.ExecMode() == sdk.ExecModeFinalize {
				latency := time.Since(start)
				h.logger.Debug(
					"finished executing the pre-block hook",
					"height", ctx.BlockHeight(),
					"latency (seconds)", latency.Seconds(),
				)
				noahmetrics.RecordLatencyAndStatus(latency, err, noahmetrics.PreBlock)

				// Record price and validator-report metrics only if prices were written successfully.
				if err == nil && prices != nil {
					// record price metrics
					h.recordPrices(prices)

					// record validator report metrics
					h.recordValidatorReports(req.DecidedLastCommit, voteTargets)
				}
			}
		}()

		// If vote extensions are not enabled, then we don't need to do anything.
		if !ve.VoteExtensionsEnabled(ctx) {
			h.logger.Info(
				"vote extensions are not enabled",
				"height", ctx.BlockHeight(),
			)

			return response, nil
		}

		h.logger.Debug(
			"executing the pre-finalise block hook",
			"height", req.Height,
		)

		// decode vote-extensions + apply prices to state
		prices, voteTargets, err = h.pa.ApplyPricesFromVoteExtensions(ctx, req)
		if err != nil {
			h.logger.Error(
				"failed to apply prices from vote extensions",
				"height", req.Height,
				"error", err,
			)

			return response, err
		}

		err = h.ok.SyncTobinTax(ctx, voteTargets)
		if err != nil {
			h.logger.Error(
				"failed to sync tobin taxes",
				"height", req.Height,
				"error", err,
			)
			return response, err
		}

		return response, nil
	}
}
