package simulation

import (
	"context"

	"github.com/cosmos/cosmos-sdk/testutil/simsx"

	"ark/x/oracle/types"
)

// MsgUpdateParamsFactory creates a gov proposal for param updates
func MsgUpdateParamsFactory() simsx.SimMsgFactoryFn[*types.MsgUpdateParams] {
	return func(_ context.Context, testData *simsx.ChainDataSource, reporter simsx.SimulationReporter) ([]simsx.SimAccount, *types.MsgUpdateParams) {
		r := testData.Rand()
		params := types.Params{
			VoteThreshold:            GenVoteThreshold(r.Rand),
			RewardBand:               GenRewardBand(r.Rand),
			RewardWindow:             GenRewardWindow(r.Rand),
			RewardDistributionWindow: GenRewardDistributionWindow(r.Rand),
			SlashFraction:            GenSlashFraction(r.Rand),
			SlashWindow:              GenSlashWindow(r.Rand),
			MinValidPerWindow:        GenMinValidPerWindow(r.Rand),
		}

		return nil, &types.MsgUpdateParams{
			Authority: testData.ModuleAccountAddress(reporter, "gov"),
			Params:    params,
		}
	}
}
