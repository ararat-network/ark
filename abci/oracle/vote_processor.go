package oracle

import (
	"fmt"

	cmtabci "github.com/cometbft/cometbft/abci/types"

	sdk "github.com/cosmos/cosmos-sdk/types"

	oraclemetrics "github.com/ararat-network/ark/abci/oracle/metrics"
	abcitypes "github.com/ararat-network/ark/abci/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// ProcessVoteExtensions derives aggregate prices from vote extensions, writes
// available prices to state, and records validator rewards and attendance.
func ProcessVoteExtensions(
	ctx sdk.Context,
	oracleKeeper abcitypes.OracleKeeper,
	req *cmtabci.RequestFinalizeBlock,
) error {
	voteHeight := req.Height - 1
	feeds, err := oracleKeeper.GetFeeds(ctx, voteHeight)
	if err != nil {
		return fmt.Errorf(
			"%w: get feeds for height %d: %w",
			abcitypes.ErrOracleKeeper,
			voteHeight,
			err,
		)
	}

	// If vote extensions have been enabled, the extended commit info - which
	// contains the vote extensions - must be included in the request.
	expectedVotes := len(req.DecidedLastCommit.Votes)
	votes, err := GetOracleVotes(req.Txs, feeds, expectedVotes)
	if err != nil {
		return fmt.Errorf("get oracle votes for block %d: %w", req.Height, err)
	}
	recordVoteReportTelemetry(ctx, votes)

	params, err := oracleKeeper.GetParams(ctx)
	if err != nil {
		return fmt.Errorf(
			"%w: get oracle params for block %d: %w",
			abcitypes.ErrOracleKeeper,
			req.Height,
			err,
		)
	}
	// Aggregate all oracle vote extensions into a single set of prices.
	result := aggregateOracleVotes(votes, params, feeds.Denoms)
	recordParticipationTelemetry(ctx, result)

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
			return fmt.Errorf(
				"%w: set exchange rate for %s: %w",
				abcitypes.ErrOracleKeeper,
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
			return fmt.Errorf(
				"%w: record vote accounting for %s: %w",
				abcitypes.ErrOracleKeeper,
				score.recipient.String(),
				err,
			)
		}
	}

	return nil
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

// recordParticipationTelemetry exports the block's attendance verdict for
// operators. Telemetry only, and only from the canonical finalise execution.
func recordParticipationTelemetry(ctx sdk.Context, result aggregationResult) {
	if ctx.ExecMode() != sdk.ExecModeFinalize {
		return
	}
	oraclemetrics.RecordBlockParticipation(result.participatingPower, result.totalPower, result.functioningBlock)
}
