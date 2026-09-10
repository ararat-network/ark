package simulation

import (
	"context"

	"github.com/cosmos/cosmos-sdk/testutil/simsx"

	"github.com/ararat-network/ark/x/treasury/types"
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

// MsgSetEconomicMandateFactory creates a valid appointment with identical policy bounds. Committee
// factories exercise corridor width separately.
func MsgSetEconomicMandateFactory() simsx.SimMsgFactoryFn[*types.MsgSetEconomicMandate] {
	return func(
		_ context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgSetEconomicMandate) {
		authority := testData.ModuleAccountAddress(reporter, "gov")
		committee := testData.AnyAccount(reporter)
		if reporter.IsSkipped() {
			return nil, nil
		}
		// The handler refuses a committee that is the authority itself, and an
		// account drawn at random could be it.
		if committee.AddressBech32 == authority {
			reporter.Skip("drawn committee is the Treasury authority")

			return nil, nil
		}

		r := testData.Rand()
		activation := r.Uint64InRange(1, 1_000)
		bounds := RandomisedEconomicPolicy(r.Rand)

		return nil, &types.MsgSetEconomicMandate{
			Authority:        authority,
			Committee:        committee.AddressBech32,
			ActivationHeight: activation,
			// Validation demands activation strictly precede expiry.
			ExpiryHeight:  activation + r.Uint64InRange(1, 100_000),
			MinimumPolicy: bounds,
			MaximumPolicy: bounds,
		}
	}
}

// MsgUpdatePolicyFactory restates economic policy. Governance sets policy
// outright rather than within the mandate corridor, which binds the committee
// alone, so any valid policy lands.
func MsgUpdatePolicyFactory() simsx.SimMsgFactoryFn[*types.MsgUpdatePolicy] {
	return func(
		_ context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgUpdatePolicy) {
		return nil, &types.MsgUpdatePolicy{
			Authority: testData.ModuleAccountAddress(reporter, "gov"),
			Policy:    RandomisedEconomicPolicy(testData.Rand().Rand),
		}
	}
}
