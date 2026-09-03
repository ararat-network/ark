package simulation

import (
	"context"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/x/treasury/keeper"
	"github.com/ararat-network/ark/x/treasury/types"
)

// betweenDec draws a decimal in [minimum, maximum]. An empty corridor returns
// its single admissible value rather than failing: a mandate may pin a field.
func betweenDec(r *simsx.XRand, minimum, maximum math.LegacyDec) math.LegacyDec {
	if !maximum.GT(minimum) {
		return minimum
	}

	return minimum.Add(r.DecN(maximum.Sub(minimum)))
}

// betweenInt draws an integer in [minimum, maximum], with the same treatment of
// a pinned field.
func betweenInt(r *simsx.XRand, minimum, maximum math.Int) math.Int {
	if !maximum.GT(minimum) {
		return minimum
	}
	drawn, err := r.PositiveSDKIntn(maximum.Sub(minimum))
	if err != nil {
		return minimum
	}

	return minimum.Add(drawn)
}

// policyWithin returns an economic policy inside the mandate's corridor, which
// is what the committee is permitted to move to. Every field the corridor
// governs is drawn between its own bounds, so a mandate that pins a field
// yields that field's only admissible value.
func policyWithin(r *simsx.XRand, mandate types.EconomicMandate) types.EconomicPolicy {
	minimum, maximum := mandate.MinimumPolicy, mandate.MaximumPolicy

	return types.EconomicPolicy{
		TransferTaxRate:             betweenDec(r, minimum.TransferTaxRate, maximum.TransferTaxRate),
		ValidatorBlockRewardTarget:  betweenInt(r, minimum.ValidatorBlockRewardTarget, maximum.ValidatorBlockRewardTarget),
		OracleBlockRewardTarget:     betweenInt(r, minimum.OracleBlockRewardTarget, maximum.OracleBlockRewardTarget),
		RedemptionBufferTargetRatio: betweenDec(r, minimum.RedemptionBufferTargetRatio, maximum.RedemptionBufferTargetRatio),
		StrategicReserveTargetRatio: betweenDec(r, minimum.StrategicReserveTargetRatio, maximum.StrategicReserveTargetRatio),
		InsuranceTargetRatio:        betweenDec(r, minimum.InsuranceTargetRatio, maximum.InsuranceTargetRatio),
		LiabilityRatioWeight:        betweenDec(r, minimum.LiabilityRatioWeight, maximum.LiabilityRatioWeight),
		VolatilityWeight:            betweenDec(r, minimum.VolatilityWeight, maximum.VolatilityWeight),
		FlowWeight:                  betweenDec(r, minimum.FlowWeight, maximum.FlowWeight),
	}
}

// MsgCommitteeUpdatePolicyFactory moves economic policy within the corridor the
// mandate grants. Unlike a governance proposal, this is delivered as a
// transaction the appointed committee signs, so the run must hold that key and
// the mandate must be active at this height.
func MsgCommitteeUpdatePolicyFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgCommitteeUpdatePolicy] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgCommitteeUpdatePolicy) {
		mandate, err := k.EconomicMandate.Get(ctx)
		if err != nil {
			reporter.Skip("get economic mandate: " + err.Error())

			return nil, nil
		}
		if !mandate.IsActive(uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())) {
			reporter.Skip("economic mandate is not active at this height")

			return nil, nil
		}
		// The appointee must be an account this run can sign for. A mandate
		// appointed by governance names one; one restored from an export may
		// name an address the run never held.
		committee := testData.GetAccount(reporter, mandate.Committee)
		if reporter.IsSkipped() {
			return nil, nil
		}

		return []simsx.SimAccount{committee}, &types.MsgCommitteeUpdatePolicy{
			Committee:    mandate.Committee,
			ExpectedTerm: mandate.Term,
			Policy:       policyWithin(testData.Rand(), mandate),
		}
	}
}
