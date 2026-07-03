package oracle

import (
	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"noah/abci/codec"
	noahabcitypes "noah/abci/types"
	oracletypes "noah/x/oracle/types"
)

// PriceApplier applies prices derived from vote extensions to state.
type PriceApplier struct {
	// va is a VoteAggregator that is used to aggregate votes into prices.
	va *VoteAggregator

	// ok is the oracle keeper that is used to write prices to state.
	ok noahabcitypes.OracleKeeper

	// logger is used for price application diagnostics.
	logger log.Logger

	// codecs decode vote extensions and extended commit info.
	voteExtensionCodec  codec.VoteExtensionCodec
	extendedCommitCodec codec.ExtendedCommitCodec
}

// NewPriceApplier returns a new PriceApplier.
func NewPriceApplier(
	va *VoteAggregator,
	ok noahabcitypes.OracleKeeper,
	voteExtensionCodec codec.VoteExtensionCodec,
	extendedCommitCodec codec.ExtendedCommitCodec,
	logger log.Logger,
) *PriceApplier {
	return &PriceApplier{
		va:                  va,
		ok:                  ok,
		logger:              logger,
		voteExtensionCodec:  voteExtensionCodec,
		extendedCommitCodec: extendedCommitCodec,
	}
}

// ApplyPricesFromVoteExtensions derives aggregate prices from vote extensions
// using the VoteAggregator. If a price exists for an asset, it is written to
// state. Aggregated prices and vote targets are returned on success; otherwise
// an error is returned with nil maps.
func (pa *PriceApplier) ApplyPricesFromVoteExtensions(ctx sdk.Context, req *cometabci.RequestFinalizeBlock) (map[string]math.LegacyDec, map[string]math.LegacyDec, error) {
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

		return nil, nil, OracleKeeperError{Err: err}
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

		return nil, nil, err
	}
	voteTargets, err := pa.ok.GetVoteTargets(ctx)
	if err != nil {
		pa.logger.Error(
			"failed to get vote targets",
			"height", req.Height,
			"err", err,
		)

		return nil, nil, err
	}

	// Aggregate all oracle vote extensions into a single set of prices.
	prices, scoreMap, err := pa.va.aggregateOracleVotes(ctx, votes, params, voteTargets)
	if err != nil {
		pa.logger.Error(
			"failed to aggregate oracle votes",
			"height", req.Height,
			"err", err,
		)

		return nil, nil, PriceAggregationError{Err: err}
	}

	for denom, price := range prices {
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

			return nil, nil, OracleKeeperError{Err: err}
		}

		pa.logger.Debug(
			"set exchange rate",
			"denom", denom,
			"exchange_rate", price.String(),
		)
	}

	// Update scores in oracle.
	for validator, score := range scoreMap {
		if err := pa.ok.AddScoreWeight(ctx, score.Recipient, score.Weight); err != nil {
			pa.logger.Error(
				"failed to set score",
				"height", req.Height,
				"validator", validator,
				"err", err,
			)

			return nil, nil, OracleKeeperError{Err: err}
		}

		if int(score.WinCount) != len(voteTargets) {
			if err := pa.ok.IncrementMissCount(ctx, score.Recipient); err != nil {
				pa.logger.Error(
					"failed to increment miss count",
					"height", req.Height,
					"validator", validator,
					"err", err,
				)

				return nil, nil, OracleKeeperError{Err: err}
			}
		}
	}

	return prices, voteTargets, nil
}

// GetPricesForValidator gets the rates reported by a validator in the latest
// aggregation.
func (pa *PriceApplier) GetPricesForValidator(validator sdk.ConsAddress) map[string]math.LegacyDec {
	return pa.va.GetPriceForValidator(validator)
}
