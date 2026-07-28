package oracle

import (
	"fmt"

	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	abcicodec "ark/abci/codec"
	arkabcitypes "ark/abci/types"
	oracletypes "ark/x/oracle/types"
)

// ProcessVoteExtensions derives aggregate prices from vote extensions.
// If a price exists for an asset, it is written to state. Successfully written
// prices are returned for telemetry.
func ProcessVoteExtensions(
	ctx sdk.Context,
	oracleKeeper arkabcitypes.OracleKeeper,
	voteExtensionCodec *abcicodec.VoteExtensionCodec,
	req *cometabci.RequestFinalizeBlock,
) (map[string]math.LegacyDec, error) {
	voteHeight := req.Height - 1
	voteTargets, err := oracleKeeper.GetVoteTargets(ctx, voteHeight)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: get vote targets for height %d: %w",
			arkabcitypes.ErrOracleKeeper,
			voteHeight,
			err,
		)
	}

	// If vote extensions have been enabled, the extended commit info - which
	// contains the vote extensions - must be included in the request.
	expectedVotes := len(req.DecidedLastCommit.Votes)
	votes, err := GetOracleVotes(voteExtensionCodec, req.Txs, voteTargets, expectedVotes)
	if err != nil {
		return nil, fmt.Errorf("get oracle votes for block %d: %w", req.Height, err)
	}

	params, err := oracleKeeper.GetParams(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: get oracle params for block %d: %w",
			arkabcitypes.ErrOracleKeeper,
			req.Height,
			err,
		)
	}
	// Aggregate all oracle vote extensions into a single set of prices.
	result := aggregateOracleVotes(votes, params, voteTargets.Denoms)

	for _, denom := range voteTargets.Denoms {
		price, ok := result.prices[denom]
		if !ok {
			continue
		}
		exchangeRate := oracletypes.ExchangeRate{
			Denom:          denom,
			Rate:           price,
			BlockTimestamp: ctx.BlockHeader().Time,
			BlockHeight:    uint64(ctx.BlockHeight()),
		}
		if err := oracleKeeper.SetExchangeRateWithEvent(ctx, exchangeRate); err != nil {
			return nil, fmt.Errorf(
				"%w: set exchange rate for %s: %w",
				arkabcitypes.ErrOracleKeeper,
				denom,
				err,
			)
		}
	}

	// Update rewards and attendance in oracle.
	for _, score := range result.scores {
		if err := oracleKeeper.RecordVoteAccounting(
			ctx,
			score.recipient,
			score.rewardWeight,
			result.functioningBlock,
			score.participated,
		); err != nil {
			return nil, fmt.Errorf(
				"%w: record vote accounting for %s: %w",
				arkabcitypes.ErrOracleKeeper,
				score.recipient.String(),
				err,
			)
		}
	}

	return result.prices, nil
}
