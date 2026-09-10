package simulation

import (
	"context"

	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/x/market/keeper"
	"github.com/ararat-network/ark/x/market/types"
)

// activeConversionCommittee requires an active mandate and a signer key owned by the run. Expired
// or externally appointed committees cause a normal simulation skip.
func activeConversionCommittee(
	ctx context.Context,
	k *keeper.Keeper,
	testData *simsx.ChainDataSource,
	reporter simsx.SimulationReporter,
) (types.ConversionMandate, simsx.SimAccount, bool) {
	mandate, err := k.ConversionMandate.Get(ctx)
	if err != nil {
		reporter.Skip("get conversion mandate: " + err.Error())

		return types.ConversionMandate{}, simsx.SimAccount{}, false
	}
	// Authorise gates on the envelope, so the factory checks the same thing.
	if !mandate.Envelope.IsActive(uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())) {
		reporter.Skip("conversion mandate is not active at this height")

		return types.ConversionMandate{}, simsx.SimAccount{}, false
	}
	committee := testData.GetAccount(reporter, mandate.Committee)
	if reporter.IsSkipped() {
		return types.ConversionMandate{}, simsx.SimAccount{}, false
	}

	return mandate, committee, true
}

// MsgCommitteeUpdatePolicyFactory moves conversion policy within the corridor
// the mandate grants. Only the base pool is drawn: the mandate seeded at
// genesis widens that alone, and a field the corridor pins has one admissible
// value anyway.
func MsgCommitteeUpdatePolicyFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgCommitteeUpdatePolicy] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgCommitteeUpdatePolicy) {
		mandate, committee, ok := activeConversionCommittee(ctx, k, testData, reporter)
		if !ok {
			return nil, nil
		}

		minimum, maximum := mandate.MinimumPolicy, mandate.MaximumPolicy
		basePool := minimum.BasePool.Amount
		if maximum.BasePool.Amount.GT(basePool) {
			basePool = basePool.Add(testData.Rand().DecN(maximum.BasePool.Amount.Sub(basePool)))
		}

		return []simsx.SimAccount{committee}, &types.MsgCommitteeUpdatePolicy{
			Committee:    mandate.Committee,
			ExpectedTerm: mandate.Term,
			Policy: types.ConversionPolicy{
				BasePool:           sdk.NewDecCoinFromDec(minimum.BasePool.Denom, basePool),
				PoolRecoveryPeriod: minimum.PoolRecoveryPeriod,
				MinStabilitySpread: minimum.MinStabilitySpread,
			},
		}
	}
}

// MsgCommitteeSetTobinTaxFactory raises one denomination's Tobin tax within the
// cap the mandate grants. The committee may only raise: a rate below the
// default is governance's to set, so the draw starts there.
func MsgCommitteeSetTobinTaxFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgCommitteeSetTobinTax] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgCommitteeSetTobinTax) {
		mandate, committee, ok := activeConversionCommittee(ctx, k, testData, reporter)
		if !ok {
			return nil, nil
		}
		if mandate.MaxTobinTax.IsZero() {
			reporter.Skip("conversion mandate delegates no Tobin power")

			return nil, nil
		}
		params, err := k.Params.Get(ctx)
		if err != nil {
			reporter.Skip("get market params: " + err.Error())

			return nil, nil
		}
		if params.DefaultTobinTax.GT(mandate.MaxTobinTax) {
			reporter.Skip("default Tobin tax already exceeds the mandate cap")

			return nil, nil
		}
		denoms, err := k.GetActiveDenoms(ctx)
		if err != nil {
			reporter.Skip(err.Error())

			return nil, nil
		}
		if len(denoms) == 0 {
			reporter.Skip("no registered denomination to price")

			return nil, nil
		}

		r := testData.Rand()
		tobinTax := params.DefaultTobinTax.Add(r.DecN(mandate.MaxTobinTax.Sub(params.DefaultTobinTax)))

		return []simsx.SimAccount{committee}, &types.MsgCommitteeSetTobinTax{
			Committee:    mandate.Committee,
			ExpectedTerm: mandate.Term,
			Denom:        denoms[r.Intn(len(denoms))],
			TobinTax:     tobinTax,
		}
	}
}
