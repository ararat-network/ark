package simulation

import (
	"context"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/market/keeper"
	"github.com/ararat-network/ark/x/market/types"
)

// MsgSwapFactory generates random MsgSwap transactions by picking a denom pair and a funded sender.
func MsgSwapFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgSwap] {
	querier := keeper.NewQueryServerImpl(k)

	return func(ctx context.Context, testData *simsx.ChainDataSource, reporter simsx.SimulationReporter) ([]simsx.SimAccount, *types.MsgSwap) {
		r := testData.Rand()

		offerDenom, askDenom := randomDenomPairX(ctx, r, reporter, k)
		if reporter.IsSkipped() {
			return nil, nil
		}

		sender := testData.AnyAccount(reporter, simsx.WithDenomBalance(offerDenom))
		if reporter.IsSkipped() {
			return nil, nil
		}

		offerCoin := sender.LiquidBalance().RandSubsetCoin(reporter, offerDenom)
		if reporter.IsSkipped() {
			return nil, nil
		}

		quotable(ctx, querier, reporter, offerCoin, askDenom)
		if reporter.IsSkipped() {
			return nil, nil
		}

		return []simsx.SimAccount{sender}, &types.MsgSwap{
			Trader:         sender.AddressBech32,
			OfferCoin:      sdk.NewCoin(offerCoin.Denom, offerCoin.Amount),
			AskDenom:       askDenom,
			MinimumReceive: sdk.NewInt64Coin(askDenom, 1),
		}
	}
}

// MsgSwapSendFactory generates random MsgSwapSend transactions, picking a denom pair, funded sender, and distinct receiver.
func MsgSwapSendFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgSwapSend] {
	querier := keeper.NewQueryServerImpl(k)

	return func(ctx context.Context, testData *simsx.ChainDataSource, reporter simsx.SimulationReporter) ([]simsx.SimAccount, *types.MsgSwapSend) {
		r := testData.Rand()

		offerDenom, askDenom := randomDenomPairX(ctx, r, reporter, k)
		if reporter.IsSkipped() {
			return nil, nil
		}

		if !testData.IsSendEnabledDenom(offerDenom) {
			reporter.Skip("offer denom send not enabled")
			return nil, nil
		}

		sender := testData.AnyAccount(reporter, simsx.WithDenomBalance(offerDenom))
		if reporter.IsSkipped() {
			return nil, nil
		}

		receiver := testData.AnyAccount(reporter, simsx.ExcludeAccounts(sender))
		if reporter.IsSkipped() {
			return nil, nil
		}

		offerCoin := sender.LiquidBalance().RandSubsetCoin(reporter, offerDenom)
		if reporter.IsSkipped() {
			return nil, nil
		}

		quotable(ctx, querier, reporter, offerCoin, askDenom)
		if reporter.IsSkipped() {
			return nil, nil
		}

		return []simsx.SimAccount{sender}, &types.MsgSwapSend{
			FromAddress:    sender.AddressBech32,
			ToAddress:      receiver.AddressBech32,
			OfferCoin:      sdk.NewCoin(offerCoin.Denom, offerCoin.Amount),
			AskDenom:       askDenom,
			MinimumReceive: sdk.NewInt64Coin(askDenom, 1),
		}
	}
}

// MsgUpdateParamsFactory generates random MsgUpdateParams transactions with randomised market parameters.
func MsgUpdateParamsFactory() simsx.SimMsgFactoryFn[*types.MsgUpdateParams] {
	return func(_ context.Context, testData *simsx.ChainDataSource, reporter simsx.SimulationReporter) ([]simsx.SimAccount, *types.MsgUpdateParams) {
		params := types.Params{
			DefaultTobinTax: types.DefaultTobinTax,
		}

		return nil, &types.MsgUpdateParams{
			Authority: testData.ModuleAccountAddress(reporter, "gov"),
			Params:    params,
		}
	}
}

// MsgUpdatePolicyFactory generates random MsgUpdatePolicy
// transactions. Depth, recovery period, and the spread floor are randomised:
// the denomination must stay the live pool unit, which only MsgSetReferenceDenom
// moves.
func MsgUpdatePolicyFactory() simsx.SimMsgFactoryFn[*types.MsgUpdatePolicy] {
	return func(_ context.Context, testData *simsx.ChainDataSource, reporter simsx.SimulationReporter) ([]simsx.SimAccount, *types.MsgUpdatePolicy) {
		r := testData.Rand()

		return nil, &types.MsgUpdatePolicy{
			Authority: testData.ModuleAccountAddress(reporter, "gov"),
			Policy: types.ConversionPolicy{
				BasePool:           GenBasePool(r.Rand),
				PoolRecoveryPeriod: GenPoolRecoveryPeriod(r.Rand),
				MinStabilitySpread: GenMinSpread(r.Rand),
			},
		}
	}
}

// randomDenomPairX picks a random offer/ask denom pair from available exchange rates.
// quotable skips the operation unless the chain would price this swap now. The
// registry says a denomination is priceable; it does not say the Oracle holds a
// rate fresh enough to price it this block. A simulation casts no vote
// extensions, so its genesis rates never refresh and every one of them ages out
// of a run long enough to reach the staleness window — past which a generated
// swap only ever fails.
func quotable(
	ctx context.Context,
	querier types.QueryServer,
	reporter simsx.SimulationReporter,
	offerCoin sdk.Coin,
	askDenom string,
) {
	_, err := querier.Swap(ctx, &types.QuerySwapRequest{
		OfferCoin: offerCoin.String(),
		AskDenom:  askDenom,
	})
	if err != nil {
		reporter.Skip(err.Error())
	}
}

func randomDenomPairX(ctx context.Context, r *simsx.XRand, reporter simsx.SimulationReporter, k *keeper.Keeper) (offerDenom, askDenom string) {
	activeDenoms, err := k.GetActiveDenoms(ctx)
	if err != nil {
		reporter.Skip(err.Error())
		return "", ""
	}

	if len(activeDenoms) == 0 {
		reporter.Skip("no available exchange rates")
		return "", ""
	}

	idx := r.Intn(len(activeDenoms) * 2)
	if idx < len(activeDenoms) {
		return chain.NoahBaseDenom, activeDenoms[idx]
	}
	return activeDenoms[idx-len(activeDenoms)], chain.NoahBaseDenom
}

// MsgSetTobinTaxOverrideFactory prices one denomination's conversion spread
// away from the policy default. The denomination comes from the registry
// because the handler resolves it there.
func MsgSetTobinTaxOverrideFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgSetTobinTaxOverride] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgSetTobinTaxOverride) {
		denoms, err := k.GetActiveDenoms(ctx)
		if err != nil {
			reporter.Skip(err.Error())

			return nil, nil
		}
		if len(denoms) == 0 {
			reporter.Skip("no registered denomination to override")

			return nil, nil
		}

		r := testData.Rand()

		return nil, &types.MsgSetTobinTaxOverride{
			Authority: testData.ModuleAccountAddress(reporter, "gov"),
			Denom:     denoms[r.Intn(len(denoms))],
			// A Tobin tax sits in [0, 1); a hundredth keeps it well inside.
			TobinTax: math.LegacyNewDecWithPrec(int64(r.IntInRange(0, 100)), 2),
		}
	}
}

// MsgRemoveTobinTaxOverrideFactory withdraws a standing override. Only a
// denomination carrying one qualifies: the handler refuses a removal that
// names nothing.
func MsgRemoveTobinTaxOverrideFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgRemoveTobinTaxOverride] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgRemoveTobinTaxOverride) {
		var denoms []string
		if err := k.TobinTaxOverrides.Walk(ctx, nil, func(denom string, _ math.LegacyDec) (bool, error) {
			denoms = append(denoms, denom)

			return false, nil
		}); err != nil {
			reporter.Skip("iterating tobin tax overrides: " + err.Error())

			return nil, nil
		}
		if len(denoms) == 0 {
			reporter.Skip("no standing tobin tax override to remove")

			return nil, nil
		}

		return nil, &types.MsgRemoveTobinTaxOverride{
			Authority: testData.ModuleAccountAddress(reporter, "gov"),
			Denom:     denoms[testData.Rand().Intn(len(denoms))],
		}
	}
}

// MsgSetConversionMandateFactory appoints the conversion committee and states
// the corridor it may move policy within. The bounds are set identical for the
// reason the economic mandate gives: a corridor's width is the committee's
// freedom, which only the committee messages exercise.
func MsgSetConversionMandateFactory() simsx.SimMsgFactoryFn[*types.MsgSetConversionMandate] {
	return func(
		_ context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgSetConversionMandate) {
		authority := testData.ModuleAccountAddress(reporter, "gov")
		committee := testData.AnyAccount(reporter)
		if reporter.IsSkipped() {
			return nil, nil
		}
		// The handler refuses a committee that is the authority itself, and an
		// account drawn at random could be it.
		if committee.AddressBech32 == authority {
			reporter.Skip("drawn committee is the Market authority")

			return nil, nil
		}

		r := testData.Rand()
		activation := r.Uint64InRange(1, 1_000)
		bounds := types.ConversionPolicy{
			BasePool:           GenBasePool(r.Rand),
			PoolRecoveryPeriod: GenPoolRecoveryPeriod(r.Rand),
			MinStabilitySpread: GenMinSpread(r.Rand),
		}

		return nil, &types.MsgSetConversionMandate{
			Authority:        authority,
			Committee:        committee.AddressBech32,
			ActivationHeight: activation,
			// Validation demands activation strictly precede expiry.
			ExpiryHeight:  activation + r.Uint64InRange(1, 100_000),
			MinimumPolicy: bounds,
			MaximumPolicy: bounds,
			MaxTobinTax:   math.LegacyNewDecWithPrec(int64(r.IntInRange(0, 100)), 2),
		}
	}
}
