package simulation

import (
	"context"

	"github.com/cosmos/cosmos-sdk/testutil/simsx"

	"noah/x/oracle/keeper"
	"noah/x/oracle/types"
)

// MsgAggregateExchangeRatePrevote
func MsgAggregateExchangeRatePrevote(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgAggregateExchangeRatePrevote] {
	return func(ctx context.Context, testData *simsx.ChainDataSource, reporter simsx.SimulationReporter) ([]simsx.SimAccount, *types.MsgAggregateExchangeRatePrevote) {
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

		return []simsx.SimAccount{sender}, &types.MsgAggregateExchangeRatePrevote{
			Trader:    sender.AddressBech32,
			OfferCoin: sdk.NewCoin(offerCoin.Denom, offerCoin.Amount),
			AskDenom:  askDenom,
		}
	}
}

// MsgAggregateExchangeRateVote
func MsgAggregateExchangeRateVote(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgAggregateExchangeRateVote] {
	return func(ctx context.Context, testData *simsx.ChainDataSource, reporter simsx.SimulationReporter) ([]simsx.SimAccount, *types.MsgAggregateExchangeRateVote) {
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

		return []simsx.SimAccount{sender}, &types.MsgAggregateExchangeRateVote{
			Trader:    sender.AddressBech32,
			OfferCoin: sdk.NewCoin(offerCoin.Denom, offerCoin.Amount),
			AskDenom:  askDenom,
		}
	}
}

// MsgDelegateFeedConsent
func MsgDelegateFeedConsent(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgDelegateFeedConsent] {
	return func(ctx context.Context, testData *simsx.ChainDataSource, reporter simsx.SimulationReporter) ([]simsx.SimAccount, *types.MsgDelegateFeedConsent) {
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

		return []simsx.SimAccount{sender}, &types.MsgDelegateFeedConsent{
			Trader:    sender.AddressBech32,
			OfferCoin: sdk.NewCoin(offerCoin.Denom, offerCoin.Amount),
			AskDenom:  askDenom,
		}
	}
}

// MsgUpdateParamsFactory creates a gov proposal for param updates
func MsgUpdateParamsFactory() simsx.SimMsgFactoryFn[*types.MsgUpdateParams] {
	return func(_ context.Context, testData *simsx.ChainDataSource, reporter simsx.SimulationReporter) ([]simsx.SimAccount, *types.MsgUpdateParams) {
		r := testData.Rand()
		params := types.Params{
			VotePeriod:               GenVotePeriod(r.Rand),
			VoteThreshold:            GenVoteThreshold(r.Rand),
			RewardBand:               GenRewardBand(r.Rand),
			RewardDistributionWindow: GenRewardDistributionWindow(r.Rand),
			SlashFraction:            GenSlashFraction(r.Rand),
			SlashWindow:              GenSlashWindow(r.Rand),
			MinValidPerWindow:        GenMinValidPerWindow(r.Rand),
		}

		return nil, &types.MsgUpdateParams{
			Authority: testData.ModuleAccountAddress(reporter, "gov"),
			Params:    params,
		}
	}
}
