package simulation

import (
	"context"

	"github.com/cosmos/cosmos-sdk/testutil/simsx"

	"noah/x/treasury/types"
)

// MsgUpdateParamsFactory creates a gov proposal for param updates
func MsgUpdateParamsFactory() simsx.SimMsgFactoryFn[*types.MsgUpdateParams] {
	return func(_ context.Context, testData *simsx.ChainDataSource, reporter simsx.SimulationReporter) ([]simsx.SimAccount, *types.MsgUpdateParams) {
		r := testData.Rand()
		params := types.Params{
			TaxPolicy:               GenTaxPolicy(r.Rand),
			RewardPolicy:            GenRewardPolicy(r.Rand),
			SeigniorageBurdenTarget: GenSeigniorageBurdenTarget(r.Rand),
			MiningIncrement:         GenMiningIncrement(r.Rand),
			WindowShort:             GenWindowShort(r.Rand),
			WindowLong:              GenWindowLong(r.Rand),
			WindowProbation:         GenWindowProbation(r.Rand),
		}

		return nil, &types.MsgUpdateParams{
			Authority: testData.ModuleAccountAddress(reporter, "gov"),
			Params:    params,
		}
	}
}
