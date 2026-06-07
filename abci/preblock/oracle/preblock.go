package oracle

import (
	"fmt"
	"math/big"
	"time"

	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/log/v2"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	abciaggregator "noah/abci/strategies/aggregator"
	"noah/abci/strategies/codec"
	noahabcitypes "noah/abci/types"
	"noah/abci/ve"
	servicemetrics "noah/service/metrics"
)

// PreBlockHandler is responsible for aggregating oracle data from each
// validator and writing the oracle data into the store before any transactions
// are executed/finalized for a given block.
type PreBlockHandler struct { //golint:ignore
	logger log.Logger

	// metrics is responsible for reporting / aggregating consensus-specific
	// metrics for this validator.
	metrics servicemetrics.Metrics

	// keeper is the keeper for the oracle module. This is utilized to write
	// oracle data to state.
	keeper noahabcitypes.OracleKeeper

	// pa is the price applier that is used to decode vote-extensions, aggregate price reports, and write prices to state.
	pa *abciaggregator.PriceApplier
}

// NewOraclePreBlockHandler returns a new PreBlockHandler. The handler
// is responsible for writing oracle data included in vote extensions to state.
func NewOraclePreBlockHandler(
	logger log.Logger,
	oracleKeeper noahabcitypes.OracleKeeper,
	metrics servicemetrics.Metrics,
	veCodec codec.VoteExtensionCodec,
	ecCodec codec.ExtendedCommitCodec,
) *PreBlockHandler {
	va := abciaggregator.NewVoteAggregator(
		logger,
	)
	pa := abciaggregator.NewPriceApplier(
		va,
		oracleKeeper,
		veCodec,
		ecCodec,
		logger,
	)

	return &PreBlockHandler{
		logger:  logger,
		keeper:  oracleKeeper,
		metrics: metrics,
		pa:      pa,
	}
}

// WrappedPreBlocker is called by the base app before the block is finalized. It
// is responsible for calling the module manager's PreBlock method, aggregating oracle data from each validator and
// writing the oracle data to the store.
func (h *PreBlockHandler) WrappedPreBlocker(mm *module.Manager) sdk.PreBlocker {
	return func(ctx sdk.Context, req *cometabci.RequestFinalizeBlock) (response *sdk.ResponsePreBlock, err error) {
		if req == nil {
			ctx.Logger().Error(
				"received nil RequestFinalizeBlock in oracle preblocker",
				"height", ctx.BlockHeight(),
			)

			return &sdk.ResponsePreBlock{}, fmt.Errorf("received nil RequestFinalizeBlock in oracle preblocker: height %d", ctx.BlockHeight())
		}

		// call module manager's PreBlocker first in case there is changes made on upgrades
		// that can modify state and lead to serialization/deserialization issues
		response, err = mm.PreBlock(ctx)
		if err != nil {
			return response, err
		}

		start := time.Now()
		var prices map[string]*big.Int
		defer func() {
			// only measure latency in Finalize
			if ctx.ExecMode() == sdk.ExecModeFinalize {
				latency := time.Since(start)
				h.logger.Debug(
					"finished executing the pre-block hook",
					"height", ctx.BlockHeight(),
					"latency (seconds)", latency.Seconds(),
				)
				noahabcitypes.RecordLatencyAndStatus(h.metrics, latency, err, servicemetrics.PreBlock)

				// record prices + ticker metrics per validator (only do so if there was no error writing the prices)
				if err == nil && prices != nil {
					// record price metrics
					h.recordPrices(prices)

					// record validator report metrics
					h.recordValidatorReports(ctx, req.DecidedLastCommit)
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
			"executing the pre-finalize block hook",
			"height", req.Height,
		)

		// decode vote-extensions + apply prices to state
		prices, err = h.pa.ApplyPricesFromVoteExtensions(ctx, req)
		if err != nil {
			h.logger.Error(
				"failed to apply prices from vote extensions",
				"height", req.Height,
				"error", err,
			)

			return response, err
		}

		return response, nil
	}
}
