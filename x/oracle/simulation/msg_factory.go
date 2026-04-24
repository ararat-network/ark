package simulation

import (
	"context"
	"strings"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	sdk "github.com/cosmos/cosmos-sdk/types"

	core "noah/types"
	"noah/x/oracle/keeper"
	"noah/x/oracle/types"
)

const salt = "1234"

var (
	whitelist   = []string{core.MicroKRWDenom, core.MicroUSDDenom, core.MicroSDRDenom}
	voteHashMap = make(map[string]string)
)

// MsgAggregateExchangeRatePrevoteFactory submits a hashed exchange rate prevote for a random bonded validator.
func MsgAggregateExchangeRatePrevoteFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgPrevote] {
	return func(ctx context.Context, testData *simsx.ChainDataSource, reporter simsx.SimulationReporter) ([]simsx.SimAccount, *types.MsgPrevote) {
		r := testData.Rand()
		validators, err := k.GetAllValidators(ctx)
		if err != nil {
			reporter.Skip(err.Error())
			return nil, nil
		}

		val := simsx.OneOf(r, validators)
		if !val.IsBonded() {
			reporter.Skip("validator is not bonded")
			return nil, nil
		}
		addrBytes, err := k.ValidatorAddressCodec().StringToBytes(val.GetOperator())
		if err != nil {
			reporter.Skip(err.Error())
			return nil, nil
		}
		valAddress := sdk.ValAddress(addrBytes)

		exchangeRatesStr := ""
		for _, denom := range whitelist {
			price := math.LegacyNewDecWithPrec(int64(r.IntInRange(1, 10000)), 1)
			exchangeRatesStr += price.String() + denom + ","
		}
		exchangeRatesStr = strings.TrimRight(exchangeRatesStr, ",")
		voteHash := types.GetVoteHash(salt, exchangeRatesStr, valAddress)

		feederAddr, err := k.GetFeederDelegation(ctx, valAddress)
		if err != nil {
			reporter.Skip(err.Error())
			return nil, nil
		}
		feederAccount := testData.GetAccountbyAccAddr(reporter, feederAddr)
		if reporter.IsSkipped() {
			return nil, nil
		}
		voteHashMap[val.GetOperator()] = exchangeRatesStr

		return []simsx.SimAccount{feederAccount}, types.NewMsgPrevote(voteHash, feederAddr, valAddress)
	}
}

// MsgAggregateExchangeRateVoteFactory reveals exchange rates for a validator that previously submitted a prevote.
func MsgAggregateExchangeRateVoteFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgVote] {
	return func(ctx context.Context, testData *simsx.ChainDataSource, reporter simsx.SimulationReporter) ([]simsx.SimAccount, *types.MsgVote) {
		r := testData.Rand()
		validators, err := k.GetAllValidators(ctx)
		if err != nil {
			reporter.Skip(err.Error())
			return nil, nil
		}

		val := simsx.OneOf(r, validators)
		if !val.IsBonded() {
			reporter.Skip("validator is not bonded")
			return nil, nil
		}
		addrBytes, err := k.ValidatorAddressCodec().StringToBytes(val.GetOperator())
		if err != nil {
			reporter.Skip(err.Error())
			return nil, nil
		}
		valAddress := sdk.ValAddress(addrBytes)

		// ensure vote hash exists
		exchangeRatesStr, ok := voteHashMap[val.GetOperator()]
		if !ok {
			reporter.Skip("vote hash does not exist")
			return nil, nil
		}

		// get prevote
		prevote, err := k.Prevote.Get(ctx, valAddress)
		if err != nil {
			reporter.Skip(err.Error())
			return nil, nil
		}

		params, err := k.Params.Get(ctx)
		if err != nil {
			reporter.Skip(err.Error())
			return nil, nil
		}
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		if (uint64(sdkCtx.BlockHeight())/params.VotePeriod)-(prevote.SubmitBlock/params.VotePeriod) != 1 {
			reporter.Skip("reveal period of submitted vote do not match with registered prevote")
			return nil, nil
		}

		feederAddr, err := k.GetFeederDelegation(ctx, valAddress)
		if err != nil {
			reporter.Skip(err.Error())
			return nil, nil
		}
		feederAccount := testData.GetAccountbyAccAddr(reporter, feederAddr)
		if reporter.IsSkipped() {
			return nil, nil
		}

		return []simsx.SimAccount{feederAccount}, types.NewMsgVote(salt, exchangeRatesStr, feederAddr, valAddress)
	}
}

// MsgDelegateFeedConsentFactory delegates oracle feeder authority from a validator to a non-validator account.
func MsgDelegateFeedConsentFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgDelegateFeedConsent] {
	return func(ctx context.Context, testData *simsx.ChainDataSource, reporter simsx.SimulationReporter) ([]simsx.SimAccount, *types.MsgDelegateFeedConsent) {
		r := testData.Rand()
		validators, err := k.GetAllValidators(ctx)
		if err != nil {
			reporter.Skip(err.Error())
			return nil, nil
		}

		val := simsx.OneOf(r, validators)
		if !val.IsBonded() {
			reporter.Skip("validator is not bonded")
			return nil, nil
		}
		addrBytes, err := k.ValidatorAddressCodec().StringToBytes(val.GetOperator())
		if err != nil {
			reporter.Skip(err.Error())
			return nil, nil
		}
		valAccount := testData.GetAccountbyAccAddr(reporter, addrBytes)
		if reporter.IsSkipped() {
			return nil, nil
		}
		valAddress := sdk.ValAddress(addrBytes)
		notValidator := simsx.SimAccountFilterFn(func(a simsx.SimAccount) bool {
			val, err := k.Validator(ctx, sdk.ValAddress(a.Address))
			return err != nil || val == nil
		})
		delegateAccount := testData.AnyAccount(reporter, notValidator)
		if reporter.IsSkipped() {
			return nil, nil
		}

		return []simsx.SimAccount{valAccount}, types.NewMsgDelegateFeedConsent(valAddress, delegateAccount.Address)
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
