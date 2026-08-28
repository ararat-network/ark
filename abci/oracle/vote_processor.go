package oracle

import (
	"fmt"

	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	oraclemetrics "github.com/ararat-network/ark/abci/oracle/metrics"
	arkabcitypes "github.com/ararat-network/ark/abci/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// ProcessVoteExtensions derives aggregate prices from vote extensions.
// If a price exists for an asset, it is written to state. Successfully written
// prices are returned for telemetry.
func ProcessVoteExtensions(
	ctx sdk.Context,
	oracleKeeper arkabcitypes.OracleKeeper,
	req *cometabci.RequestFinalizeBlock,
) (map[string]math.LegacyDec, error) {
	voteHeight := req.Height - 1
	feeds, err := oracleKeeper.GetFeeds(ctx, voteHeight)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: get feeds for height %d: %w",
			arkabcitypes.ErrOracleKeeper,
			voteHeight,
			err,
		)
	}

	// If vote extensions have been enabled, the extended commit info - which
	// contains the vote extensions - must be included in the request.
	expectedVotes := len(req.DecidedLastCommit.Votes)
	votes, err := GetOracleVotes(req.Txs, feeds, expectedVotes)
	if err != nil {
		return nil, fmt.Errorf("get oracle votes for block %d: %w", req.Height, err)
	}
	recordVoteReportTelemetry(ctx, votes)

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
	result := aggregateOracleVotes(votes, params, feeds.Denoms)

	for _, denom := range feeds.Denoms {
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

// recordVoteReportTelemetry classifies decoded reports for operators, keeping
// invalid payloads distinguishable from plain absences. Telemetry only:
// consensus never reads these signals, and only the canonical finalise
// execution records them.
func recordVoteReportTelemetry(ctx sdk.Context, votes []Vote) {
	if ctx.ExecMode() != sdk.ExecModeFinalize {
		return
	}

	logger := ctx.Logger()
	var valid, empty, invalid int64
	for _, vote := range votes {
		switch {
		case vote.Invalid:
			invalid++
			if logger != nil {
				logger.Debug(
					"oracle vote extension payload is invalid",
					"validator", sdk.ConsAddress(vote.Validator.Address).String(),
				)
			}
		case vote.Rates == nil:
			empty++
		default:
			valid++
		}
	}
	oraclemetrics.CountVoteReports(valid, empty, invalid)
}
