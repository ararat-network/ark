package simulation

import (
	"context"
	marketv1 "noah/api/noah/market/v1"
	core "noah/types"
	"noah/x/market/keeper"
	"noah/x/market/types"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/testutil/simsx"
)

func MsgSwapFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*marketv1.MsgSwap] {
	return func(ctx context.Context, testData *simsx.ChainDataSource, reporter simsx.SimulationReporter) ([]simsx.SimAccount, *marketv1.MsgSwap) {
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

		return []simsx.SimAccount{sender}, &marketv1.MsgSwap{
			Trader: sender.AddressBech32,
			OfferCoin: &basev1beta1.Coin{
				Denom:  offerCoin.Denom,
				Amount: offerCoin.Amount.String(),
			},
			AskDenom: askDenom,
		}
	}
}

func MsgSwapSendFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*marketv1.MsgSwapSend] {
	return func(ctx context.Context, testData *simsx.ChainDataSource, reporter simsx.SimulationReporter) ([]simsx.SimAccount, *marketv1.MsgSwapSend) {
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

		return []simsx.SimAccount{sender}, &marketv1.MsgSwapSend{
			FromAddress: sender.AddressBech32,
			ToAddress:   receiver.AddressBech32,
			OfferCoin: &basev1beta1.Coin{
				Denom:  offerCoin.Denom,
				Amount: offerCoin.Amount.String(),
			},
			AskDenom: askDenom,
		}
	}
}

func MsgUpdateParamsFactory() simsx.SimMsgFactoryFn[*marketv1.MsgUpdateParams] {
	return func(_ context.Context, testData *simsx.ChainDataSource, reporter simsx.SimulationReporter) ([]simsx.SimAccount, *marketv1.MsgUpdateParams) {
		r := testData.Rand()
		params := types.DefaultParams()
		params.BasePool = GenBasePool(r.Rand).String()
		params.PoolRecoveryPeriod = GenPoolRecoveryPeriod(r.Rand)
		params.MinStabilitySpread = GenMinSpread(r.Rand).String()

		return nil, &marketv1.MsgUpdateParams{
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
