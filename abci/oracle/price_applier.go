package oracle

import (
	"slices"

	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/log/v2"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/abci/codec"
	arkabcitypes "ark/abci/types"
	oracletypes "ark/x/oracle/types"
)

// PriceApplier applies prices derived from vote extensions to state.
type PriceApplier struct {
	// ok is the oracle keeper that is used to write prices to state.
	ok arkabcitypes.OracleKeeper

	// logger is used for price application diagnostics.
	logger log.Logger

	// codecs decode vote extensions and extended commit info.
	voteExtensionCodec  codec.VoteExtensionCodec
	extendedCommitCodec codec.ExtendedCommitCodec
}

// NewPriceApplier returns a new PriceApplier.
func NewPriceApplier(
	ok arkabcitypes.OracleKeeper,
	voteExtensionCodec codec.VoteExtensionCodec,
	extendedCommitCodec codec.ExtendedCommitCodec,
	logger log.Logger,
) *PriceApplier {
	return &PriceApplier{
		ok:                  ok,
		logger:              logger,
		voteExtensionCodec:  voteExtensionCodec,
		extendedCommitCodec: extendedCommitCodec,
	}
}

// ApplyPricesFromVoteExtensions derives aggregate prices from vote extensions.
// If a price exists for an asset, it is written to state. The complete
// aggregation result is returned for state synchronization and telemetry.
func (pa *PriceApplier) ApplyPricesFromVoteExtensions(ctx sdk.Context, req *cometabci.RequestFinalizeBlock) (AggregationResult, error) {
	// If vote extensions have been enabled, the extended commit info - which
	// contains the vote extensions - must be included in the request.
	votes, err := GetOracleVotes(req.Txs, pa.voteExtensionCodec, pa.extendedCommitCodec)
	if err != nil {
		pa.logger.Error(
			"failed to get extended commit info from proposal",
			"height", req.Height,
			"num_txs", len(req.Txs),
			"err", err,
		)

		return AggregationResult{}, OracleKeeperError{Err: err}
	}

	pa.logger.Debug(
		"got oracle vote extensions",
		"height", req.Height,
		"num_votes", len(votes),
	)

	params, err := pa.ok.GetParams(ctx)
	if err != nil {
		pa.logger.Error(
			"failed to get oracle params",
			"height", req.Height,
			"err", err,
		)

		return AggregationResult{}, err
	}
	voteTargets, err := pa.ok.GetVoteTargets(ctx)
	if err != nil {
		pa.logger.Error(
			"failed to get vote targets",
			"height", req.Height,
			"err", err,
		)

		return AggregationResult{}, err
	}

	// Aggregate all oracle vote extensions into a single set of prices.
	result, err := aggregateOracleVotes(votes, params, voteTargets)
	if err != nil {
		pa.logger.Error(
			"failed to aggregate oracle votes",
			"height", req.Height,
			"err", err,
		)

		return AggregationResult{}, PriceAggregationError{Err: err}
	}

	priceDenoms := make([]string, 0, len(result.Prices))
	for denom := range result.Prices {
		priceDenoms = append(priceDenoms, denom)
	}
	slices.Sort(priceDenoms)

	for _, denom := range priceDenoms {
		price := result.Prices[denom]
		exchangeRate := oracletypes.NewExchangeRate(
			denom,
			price,
			ctx.BlockHeader().Time,
			uint64(ctx.BlockHeight()),
		)
		if err := pa.ok.SetExchangeRateWithEvent(ctx, exchangeRate); err != nil {
			pa.logger.Error(
				"failed to set exchange rate",
				"denom", denom,
				"exchange_rate", price.String(),
				"err", err,
			)

			return AggregationResult{}, OracleKeeperError{Err: err}
		}

		pa.logger.Debug(
			"set exchange rate",
			"denom", denom,
			"exchange_rate", price.String(),
		)
	}

	// Update scores in oracle.
	for _, score := range result.scores {
		if err := pa.ok.RecordVoteAccounting(ctx, score.recipient, score.weight, score.missed); err != nil {
			pa.logger.Error(
				"failed to record vote accounting",
				"height", req.Height,
				"validator", score.recipient.String(),
				"err", err,
			)

			return AggregationResult{}, OracleKeeperError{Err: err}
		}
	}

	return result, nil
}
