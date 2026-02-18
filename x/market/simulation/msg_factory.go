package simulation

import (
	"context"
	core "noah/types"
	"noah/x/market/keeper"
	"noah/x/market/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/testutil/simsx"
)

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
			Trader:    sender.AddressBech32,
			OfferCoin: sdk.NewCoin(offerCoin.Denom, offerCoin.Amount),
			AskDenom:  askDenom,
		}
	}
}

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
			FromAddress: sender.AddressBech32,
			ToAddress:   receiver.AddressBech32,
			OfferCoin:   sdk.NewCoin(offerCoin.Denom, offerCoin.Amount),
			AskDenom:    askDenom,
		}
	}
}

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
	var whitelist []string
	k.OracleKeeper.IterateArkExchangeRates(ctx, func(denom string, _ math.LegacyDec) bool {
		whitelist = append(whitelist, denom)
		return false
	})

	if len(whitelist) == 0 {
		reporter.Skip("no available exchange rates")
		return "", ""
	}

	idx := r.Intn(len(whitelist) * 2)
	if idx < len(whitelist) {
		return core.MicroArkDenom, whitelist[idx]
	}
	return whitelist[idx-len(whitelist)], core.MicroArkDenom
}
