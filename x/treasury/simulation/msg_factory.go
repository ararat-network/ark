package simulation

import (
	"context"

	"github.com/cosmos/cosmos-sdk/testutil/simsx"

	"ark/x/treasury/types"
)

// MsgUpdateParamsFactory creates a governance proposal for a valid parameter
// update without activating cross-module tax-cap derivation.
func MsgUpdateParamsFactory() simsx.SimMsgFactoryFn[*types.MsgUpdateParams] {
	return func(
		_ context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgUpdateParams) {
		return nil, &types.MsgUpdateParams{
			Authority: testData.ModuleAccountAddress(reporter, "gov"),
			Params:    RandomisedParams(testData.Rand().Rand),
		}
	}
}
