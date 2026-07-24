package simulation

import (
	"context"

	"github.com/cosmos/cosmos-sdk/testutil/simsx"

	"ark/x/oracle/keeper"
	"ark/x/oracle/types"
)

// MsgUpdateParamsFactory creates a gov proposal for param updates
func MsgUpdateParamsFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgUpdateParams] {
	return func(ctx context.Context, testData *simsx.ChainDataSource, reporter simsx.SimulationReporter) ([]simsx.SimAccount, *types.MsgUpdateParams) {
		params, err := k.Params.Get(ctx)
		if err != nil {
			reporter.Skip("get oracle params: " + err.Error())
			return nil, nil
		}

		r := testData.Rand()
		params.VoteThreshold = GenVoteThreshold(r.Rand)
		params.RewardBand = GenRewardBand(r.Rand)
		params.RewardWindow = GenRewardWindow(r.Rand)
		params.RewardDistributionWindow = GenRewardDistributionWindow(r.Rand)
		params.SlashFraction = GenSlashFraction(r.Rand)
		params.SlashWindow = GenSlashWindow(r.Rand)
		params.MinValidPerWindow = GenMinValidPerWindow(r.Rand)

		return nil, &types.MsgUpdateParams{
			Authority: testData.ModuleAccountAddress(reporter, "gov"),
			Params:    params,
		}
	}
}
