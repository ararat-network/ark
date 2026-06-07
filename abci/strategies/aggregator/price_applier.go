package aggregator

import (
	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"noah/abci/strategies/codec"
	noahabcitypes "noah/abci/types"
	oracletypes "noah/x/oracle/types"
)

// PriceApplier is an interface used in `ExtendVote` and `PreBlock` to apply the prices
// derived from the latest votes to state.
type PriceApplier struct {
	// va is a VoteAggregator that is used to aggregate votes into prices.
	va *VoteAggregator

	// ok is the oracle keeper that is used to write prices to state.
	ok noahabcitypes.OracleKeeper

	// logger
	logger log.Logger

	// codecs
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

// ApplyPricesFromVoteExtensions derives the aggregate prices per asset in accordance with the given
// vote extensions + VoteAggregator. If a price exists for an asset, it is written to state. The
// prices aggregated from vote-extensions are returned if no errors are encountered in execution,
// otherwise an error is returned + nil prices.
func (pa *PriceApplier) ApplyPricesFromVoteExtensions(ctx sdk.Context, req *cometabci.RequestFinalizeBlock) (map[string]math.LegacyDec, error) {
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

		return nil, err
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

		return nil, err
	}
	voteTargets := make(map[string]math.LegacyDec, len(params.TobinTaxes))
	for _, tobinTax := range params.TobinTaxes {
		voteTargets[tobinTax.Denom] = tobinTax.TobinTax
	}

	// Aggregate all oracle vote extensions into a single set of prices.
	prices, scoreMap, err := pa.va.AggregateOracleVotes(ctx, votes, params, voteTargets)
	if err != nil {
		pa.logger.Error(
			"failed to aggregate oracle votes",
			"height", req.Height,
			"err", err,
		)

		err = PriceAggregationError{
			Err: err,
		}
		return nil, err
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

			return nil, err
		}

		pa.logger.Debug(
			"set exchange rate",
			"denom", denom,
			"exchange_rate", price.String(),
		)
	}

	for validator, score := range scoreMap {
		if err := pa.ok.AddScoreWeight(ctx, score.Recipient, score.Weight); err != nil {
			pa.logger.Error(
				"failed to set score",
				"height", req.Height,
				"validator", validator,
				"err", err,
			)

			return nil, err
		}

		if int(score.WinCount) != len(voteTargets) {
			if err := pa.ok.IncrementMissCount(ctx, score.Recipient); err != nil {
				pa.logger.Error(
					"failed to increment miss count",
					"height", req.Height,
					"validator", validator,
					"err", err,
				)

				return nil, err
			}
		}
	}

	return prices, nil
}

// GetPricesForValidator gets the prices reported by a given validator. This method depends
// on the prices from the latest set of aggregated votes.
func (pa *PriceApplier) GetPricesForValidator(validator sdk.ConsAddress) map[string]math.LegacyDec {
	return pa.va.GetPriceForValidator(validator)
}
