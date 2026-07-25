package simulation

import (
	"context"

	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	"ark/x/market/keeper"
	"ark/x/market/types"
)

// MsgSwapFactory generates random MsgSwap transactions by picking a denom pair and a funded sender.
func MsgSwapFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgSwap] {
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
		r := testData.Rand()
		params := types.Params{
			BasePool:           GenBasePool(r.Rand),
			PoolRecoveryPeriod: GenPoolRecoveryPeriod(r.Rand),
			MinStabilitySpread: GenMinSpread(r.Rand),
		}

		return nil, &types.MsgUpdateParams{
			Authority: testData.ModuleAccountAddress(reporter, "gov"),
			Params:    params,
		}
	}
}

// randomDenomPairX picks a random offer/ask denom pair from available exchange rates.
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
