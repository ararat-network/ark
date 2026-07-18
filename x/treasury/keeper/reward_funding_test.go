package keeper_test

import (
	"errors"
	"math/big"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"go.uber.org/mock/gomock"

	chain "ark/pkg/chain"
	oracletypes "ark/x/oracle/types"
	"ark/x/treasury/types"
)

func (s *KeeperTestSuite) TestUpdateRewardFundingAccruesBlock() {
	s.setBlockHeight(2)
	policy := types.DefaultMonetaryPolicy()
	policy.ValidatorBlockRewardTarget = math.NewInt(7)
	policy.OracleBlockRewardTarget = math.NewInt(3)
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	s.expectValidatorFees(sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 5)))
	s.expectNoahRewardValuation()

	_, err := s.keeper.UpdateRewardFunding(s.ctx)
	s.Require().NoError(err)
	funding, err := s.keeper.RewardFunding.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(types.DefaultRewardFundingWindow-1, funding.BlocksRemaining)
	s.Require().Equal(math.NewInt(7), funding.ValidatorTarget)
	s.Require().Equal(math.NewInt(3), funding.OracleTarget)
	s.Require().Equal(math.NewInt(5), funding.ValidatorFeeValue)
	s.Require().True(funding.ValuationComplete)
}

func (s *KeeperTestSuite) TestBeginBlockerDefersWindowChangeUntilNextWindow() {
	s.setBlockHeight(2)
	params := types.DefaultParams()
	params.RewardFundingWindow = 2
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.expectTaxCapsMatch()
	s.expectValidatorFees(sdk.NewCoins())

	s.Require().NoError(s.keeper.BeginBlocker(s.ctx))
	funding, err := s.keeper.RewardFunding.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(uint64(1), funding.BlocksRemaining)

	params.RewardFundingWindow = 3
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.setBlockHeight(3)
	s.expectTaxCapsMatch()
	s.expectValidatorFees(sdk.NewCoins())
	s.expectStabilityTaxBalance(sdk.NewCoins())

	s.Require().NoError(s.keeper.BeginBlocker(s.ctx))
	s.requireDefaultRewardFunding()

	s.setBlockHeight(4)
	s.expectTaxCapsMatch()
	s.expectValidatorFees(sdk.NewCoins())
	s.Require().NoError(s.keeper.BeginBlocker(s.ctx))
	funding, err = s.keeper.RewardFunding.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(uint64(2), funding.BlocksRemaining)
}

func (s *KeeperTestSuite) TestBeginBlockerSettlesSingleBlockWindow() {
	s.setBlockHeight(2)
	params := types.DefaultParams()
	params.RewardFundingWindow = 1
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.expectTaxCapsMatch()
	s.expectValidatorFees(sdk.NewCoins())
	s.expectStabilityTaxBalance(sdk.NewCoins())

	s.Require().NoError(s.keeper.BeginBlocker(s.ctx))
	s.requireDefaultRewardFunding()
}

func (s *KeeperTestSuite) TestBeginBlockerNetsFeesAcrossWindow() {
	s.setBlockHeight(2)
	policy := types.DefaultMonetaryPolicy()
	policy.ValidatorBlockRewardTarget = math.NewInt(100)
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	s.setRewardFunding(rewardFunding(2, 0, 0, 0, true))
	s.expectTaxCapsMatch()
	s.expectValidatorFees(sdk.NewCoins())

	s.Require().NoError(s.keeper.BeginBlocker(s.ctx))

	s.setBlockHeight(3)
	s.expectTaxCapsMatch()
	s.expectValidatorFees(sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 200)))
	s.expectNoahRewardValuation()
	s.expectStabilityTaxBalance(sdk.NewCoins())
	s.oracleKeeper.EXPECT().GetRateSnapshot(gomock.Any()).
		Return(oracletypes.RateSnapshot{chain.MicroNoahDenom: math.LegacyOneDec()}, nil)
	s.expectSubsidyBalance(20)

	s.Require().NoError(s.keeper.BeginBlocker(s.ctx))
	s.requireDefaultRewardFunding()

	s.requireEvent(types.EventTypeBlockRewardsToppedUp)
}

func (s *KeeperTestSuite) TestSettleRewardFundingSendsAllTaxToOracleWhenFeesCoverTarget() {
	funding := rewardFunding(0, 7, 3, 7, true)
	stabilityTax := sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 5))
	s.expectStabilityTaxBalance(stabilityTax)
	s.expectNoahRewardValuation()
	s.expectSubsidyBalance(20)
	s.expectTaxAllocation(sdk.NewCoins(), stabilityTax)

	s.Require().NoError(s.keeper.SettleRewardFunding(s.ctx, funding))
	s.requireExactEvent(sdk.NewEvent(
		types.EventTypeBlockRewardsToppedUp,
		sdk.NewAttribute(types.AttributeKeyTarget, "validator=7unoah,oracle=3unoah"),
		sdk.NewAttribute(types.AttributeKeyOrganic, "validator=7unoah,oracle=5unoah"),
		sdk.NewAttribute(types.AttributeKeyPaid, "validator=0unoah,oracle=0unoah"),
	))
}

func (s *KeeperTestSuite) TestSettleRewardFundingProtectsOracleThenFundsValidatorGap() {
	funding := rewardFunding(0, 7, 3, 2, true)
	stabilityTax := sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 8))
	s.expectStabilityTaxBalance(stabilityTax)
	s.expectNoahRewardValuation()
	s.expectSubsidyBalance(20)
	s.expectTaxAllocation(
		sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 5)),
		sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 3)),
	)

	s.Require().NoError(s.keeper.SettleRewardFunding(s.ctx, funding))
}

func (s *KeeperTestSuite) TestSettleRewardFundingReturnsResidualTaxToOracle() {
	funding := rewardFunding(0, 7, 3, 5, true)
	stabilityTax := sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 10))
	s.expectStabilityTaxBalance(stabilityTax)
	s.expectNoahRewardValuation()
	s.expectSubsidyBalance(20)
	s.expectTaxAllocation(
		sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 2)),
		sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 8)),
	)

	s.Require().NoError(s.keeper.SettleRewardFunding(s.ctx, funding))
}

func (s *KeeperTestSuite) TestSettleRewardFundingPaysOnlyRemainingShortfalls() {
	funding := rewardFunding(0, 7, 3, 5, true)
	stabilityTax := sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 1))
	s.expectStabilityTaxBalance(stabilityTax)
	s.expectNoahRewardValuation()
	s.expectSubsidyBalance(20)
	s.expectTaxAllocation(sdk.NewCoins(), stabilityTax)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.SubsidyPoolName, authtypes.FeeCollectorName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 2)),
	).Return(nil)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.SubsidyPoolName, oracletypes.ModuleName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 2)),
	).Return(nil)

	s.Require().NoError(s.keeper.SettleRewardFunding(s.ctx, funding))
}

func (s *KeeperTestSuite) TestSettleRewardFundingAllocatesScarceSubsidyByShortfall() {
	funding := rewardFunding(0, 6, 6, 2, true)
	stabilityTax := sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 4))
	s.expectStabilityTaxBalance(stabilityTax)
	s.expectNoahRewardValuation()
	s.expectSubsidyBalance(3)
	s.expectTaxAllocation(sdk.NewCoins(), stabilityTax)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.SubsidyPoolName, authtypes.FeeCollectorName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 2)),
	).Return(nil)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.SubsidyPoolName, oracletypes.ModuleName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 1)),
	).Return(nil)

	s.Require().NoError(s.keeper.SettleRewardFunding(s.ctx, funding))
}

func (s *KeeperTestSuite) TestSettleRewardFundingConservesMultiDenomTaxAndRoundsToOracle() {
	funding := rewardFunding(0, 2, 3, 0, true)
	stabilityTax := sdk.NewCoins(
		sdk.NewInt64Coin(chain.MicroSDRDenom, 3),
		sdk.NewInt64Coin(chain.MicroKRWDenom, 2),
	)
	s.expectStabilityTaxBalance(stabilityTax)
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(oracletypes.TobinTaxes{
		{Denom: chain.MicroSDRDenom},
		{Denom: chain.MicroKRWDenom},
	}, nil)
	s.oracleKeeper.EXPECT().GetRateSnapshot(
		gomock.Any(),
		chain.MicroKRWDenom,
		chain.MicroSDRDenom,
	).Return(oracletypes.RateSnapshot{
		chain.MicroNoahDenom: math.LegacyOneDec(),
		chain.MicroSDRDenom:  math.LegacyOneDec(),
		chain.MicroKRWDenom:  math.LegacyOneDec(),
	}, nil)
	s.expectSubsidyBalance(10)
	s.expectTaxAllocation(
		sdk.NewCoins(sdk.NewInt64Coin(chain.MicroSDRDenom, 1)),
		sdk.NewCoins(
			sdk.NewInt64Coin(chain.MicroSDRDenom, 2),
			sdk.NewInt64Coin(chain.MicroKRWDenom, 2),
		),
	)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.SubsidyPoolName, authtypes.FeeCollectorName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 1)),
	).Return(nil)

	s.Require().NoError(s.keeper.SettleRewardFunding(s.ctx, funding))
}

func (s *KeeperTestSuite) TestSettleRewardFundingDoesNotCreditUnconfiguredTaxToTargets() {
	funding := rewardFunding(0, 0, 3, 0, true)
	stabilityTax := sdk.NewCoins(sdk.NewInt64Coin(chain.MicroUSDDenom, 4))
	s.expectStabilityTaxBalance(stabilityTax)
	s.expectNoahRewardValuation()
	s.expectSubsidyBalance(10)
	s.expectTaxAllocation(sdk.NewCoins(), stabilityTax)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.SubsidyPoolName, oracletypes.ModuleName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 3)),
	).Return(nil)

	s.Require().NoError(s.keeper.SettleRewardFunding(s.ctx, funding))
}

func (s *KeeperTestSuite) TestUpdateRewardFundingMarksWindowIncompleteWhenFeeValuationUnavailable() {
	s.setBlockHeight(2)
	validatorFees := sdk.NewCoins(sdk.NewInt64Coin(chain.MicroSDRDenom, 4))
	s.expectValidatorFees(validatorFees)
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(oracletypes.TobinTaxes{
		{Denom: chain.MicroSDRDenom},
	}, nil)
	s.oracleKeeper.EXPECT().GetRateSnapshot(
		gomock.Any(),
		chain.MicroSDRDenom,
	).Return(nil, oracletypes.ErrStaleExchangeRate)

	_, err := s.keeper.UpdateRewardFunding(s.ctx)
	s.Require().NoError(err)
	funding, err := s.keeper.RewardFunding.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(types.DefaultRewardFundingWindow-1, funding.BlocksRemaining)
	s.Require().False(funding.ValuationComplete)
}

func (s *KeeperTestSuite) TestUpdateRewardFundingMarksWindowIncompleteWhenFeeValueAggregateOverflows() {
	s.setBlockHeight(2)
	max := maxRepresentableInt()
	validatorFees := sdk.NewCoins(
		sdk.NewCoin(chain.MicroNoahDenom, max),
		sdk.NewCoin(chain.MicroSDRDenom, max),
	)
	s.expectValidatorFees(validatorFees)
	s.expectNoahAndSDRRewardValuation()

	_, err := s.keeper.UpdateRewardFunding(s.ctx)
	s.Require().NoError(err)
	funding, err := s.keeper.RewardFunding.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(types.DefaultRewardFundingWindow-1, funding.BlocksRemaining)
	s.Require().Equal(math.ZeroInt(), funding.ValidatorFeeValue)
	s.Require().False(funding.ValuationComplete)
}

func (s *KeeperTestSuite) TestUpdateRewardFundingRetainsAccruedFeesWhenCrossBlockSumOverflows() {
	s.setBlockHeight(2)
	max := maxRepresentableInt()
	s.setRewardFunding(types.RewardFundingState{
		BlocksRemaining:   2,
		ValidatorTarget:   math.ZeroInt(),
		OracleTarget:      math.ZeroInt(),
		ValidatorFeeValue: max,
		ValuationComplete: true,
	})
	s.expectValidatorFees(sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 1)))
	s.expectNoahRewardValuation()

	_, err := s.keeper.UpdateRewardFunding(s.ctx)
	s.Require().NoError(err)
	funding, err := s.keeper.RewardFunding.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(uint64(1), funding.BlocksRemaining)
	s.Require().Equal(max, funding.ValidatorFeeValue)
	s.Require().False(funding.ValuationComplete)
}

func (s *KeeperTestSuite) TestSettleRewardFundingDefaultsTaxToOracleWhenWindowValuationIncomplete() {
	funding := rewardFunding(0, 1, 1, 0, false)
	stabilityTax := sdk.NewCoins(sdk.NewInt64Coin(chain.MicroSDRDenom, 4))
	s.expectStabilityTaxBalance(stabilityTax)
	s.expectTaxAllocation(sdk.NewCoins(), stabilityTax)

	s.Require().NoError(s.keeper.SettleRewardFunding(s.ctx, funding))
	s.requireExactEvent(sdk.NewEvent(
		types.EventTypeBlockRewardTopUpSkipped,
		sdk.NewAttribute(
			types.AttributeKeySkipReason,
			"validator fee valuation was incomplete during the funding window",
		),
	))
}

func (s *KeeperTestSuite) TestSettleRewardFundingDefaultsTaxToOracleWhenTaxValuationUnavailable() {
	funding := rewardFunding(0, 1, 1, 0, true)
	stabilityTax := sdk.NewCoins(sdk.NewInt64Coin(chain.MicroSDRDenom, 4))
	s.expectStabilityTaxBalance(stabilityTax)
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(oracletypes.TobinTaxes{
		{Denom: chain.MicroSDRDenom},
	}, nil)
	s.oracleKeeper.EXPECT().GetRateSnapshot(
		gomock.Any(),
		chain.MicroSDRDenom,
	).Return(nil, oracletypes.ErrStaleExchangeRate)
	s.expectTaxAllocation(sdk.NewCoins(), stabilityTax)

	s.Require().NoError(s.keeper.SettleRewardFunding(s.ctx, funding))
	s.requireEvent(types.EventTypeBlockRewardTopUpSkipped)
}

func (s *KeeperTestSuite) TestSettleRewardFundingDefaultsTaxToOracleWhenTaxValueAggregateOverflows() {
	max := maxRepresentableInt()
	funding := rewardFunding(0, 1, 1, 0, true)
	stabilityTax := sdk.NewCoins(
		sdk.NewCoin(chain.MicroNoahDenom, max),
		sdk.NewCoin(chain.MicroSDRDenom, max),
	)
	s.expectStabilityTaxBalance(stabilityTax)
	s.expectNoahAndSDRRewardValuation()
	s.expectTaxAllocation(sdk.NewCoins(), stabilityTax)

	s.Require().NoError(s.keeper.SettleRewardFunding(s.ctx, funding))
	s.requireEvent(types.EventTypeBlockRewardTopUpSkipped)
}

func (s *KeeperTestSuite) TestSettleRewardFundingSendsTaxToOracleWhenTargetsAreDisabled() {
	funding := rewardFunding(0, 0, 0, 0, true)
	stabilityTax := sdk.NewCoins(sdk.NewInt64Coin(chain.MicroSDRDenom, 4))
	s.expectStabilityTaxBalance(stabilityTax)
	s.expectTaxAllocation(sdk.NewCoins(), stabilityTax)

	s.Require().NoError(s.keeper.SettleRewardFunding(s.ctx, funding))
}

func (s *KeeperTestSuite) TestBeginBlockerDoesNotClearAfterAllocationFailure() {
	initial := rewardFunding(1, 0, 0, 1, true)
	s.setBlockHeight(2)
	s.setRewardFunding(initial)
	s.expectTaxCapsMatch()
	s.expectValidatorFees(sdk.NewCoins())
	stabilityTax := sdk.NewCoins(sdk.NewInt64Coin(chain.MicroSDRDenom, 4))
	s.expectStabilityTaxBalance(stabilityTax)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.StabilityTaxCollectorName, oracletypes.ModuleName, stabilityTax,
	).Return(errors.New("bank failure"))

	err := s.keeper.BeginBlocker(s.ctx)
	s.Require().ErrorContains(err, "allocating stability tax to Oracle")
	funding, getErr := s.keeper.RewardFunding.Get(s.ctx)
	s.Require().NoError(getErr)
	s.Require().Zero(funding.BlocksRemaining)
	s.Require().Equal(math.OneInt(), funding.ValidatorFeeValue)
}

func (s *KeeperTestSuite) setRewardFunding(funding types.RewardFundingState) {
	s.Require().NoError(s.keeper.RewardFunding.Set(s.ctx, funding))
}

func rewardFunding(blocksRemaining uint64, validatorTarget, oracleTarget, validatorFees int64, complete bool) types.RewardFundingState {
	return types.RewardFundingState{
		BlocksRemaining:   blocksRemaining,
		ValidatorTarget:   math.NewInt(validatorTarget),
		OracleTarget:      math.NewInt(oracleTarget),
		ValidatorFeeValue: math.NewInt(validatorFees),
		ValuationComplete: complete,
	}
}

func (s *KeeperTestSuite) expectValidatorFees(fees sdk.Coins) {
	s.bankKeeper.EXPECT().GetAllBalances(
		gomock.Any(),
		authtypes.NewModuleAddress(authtypes.FeeCollectorName),
	).Return(fees)
}

func (s *KeeperTestSuite) expectStabilityTaxBalance(stabilityTax sdk.Coins) {
	s.bankKeeper.EXPECT().GetAllBalances(
		gomock.Any(),
		authtypes.NewModuleAddress(types.StabilityTaxCollectorName),
	).Return(stabilityTax)
}

func (s *KeeperTestSuite) expectTaxAllocation(validatorTax, oracleTax sdk.Coins) {
	if !validatorTax.IsZero() {
		s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
			gomock.Any(), types.StabilityTaxCollectorName, authtypes.FeeCollectorName, validatorTax,
		).Return(nil)
	}
	if !oracleTax.IsZero() {
		s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
			gomock.Any(), types.StabilityTaxCollectorName, oracletypes.ModuleName, oracleTax,
		).Return(nil)
	}
}

func (s *KeeperTestSuite) expectSubsidyBalance(amount int64) {
	s.bankKeeper.EXPECT().GetBalance(
		gomock.Any(),
		authtypes.NewModuleAddress(types.SubsidyPoolName),
		chain.MicroNoahDenom,
	).Return(sdk.NewInt64Coin(chain.MicroNoahDenom, amount))
}

func (s *KeeperTestSuite) expectNoahRewardValuation() {
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(oracletypes.TobinTaxes{}, nil)
	s.oracleKeeper.EXPECT().GetRateSnapshot(
		gomock.Any(),
	).Return(oracletypes.RateSnapshot{chain.MicroNoahDenom: math.LegacyOneDec()}, nil)
}

func (s *KeeperTestSuite) expectNoahAndSDRRewardValuation() {
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(oracletypes.TobinTaxes{
		{Denom: chain.MicroSDRDenom},
	}, nil)
	s.oracleKeeper.EXPECT().GetRateSnapshot(
		gomock.Any(),
		chain.MicroSDRDenom,
	).Return(oracletypes.RateSnapshot{
		chain.MicroNoahDenom: math.LegacyOneDec(),
		chain.MicroSDRDenom:  math.LegacyOneDec(),
	}, nil)
}

func maxRepresentableInt() math.Int {
	max := new(big.Int).Lsh(big.NewInt(1), math.MaxBitLen)
	return math.NewIntFromBigInt(max.Sub(max, big.NewInt(1)))
}

func (s *KeeperTestSuite) expectTaxCapsMatch() {
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(oracletypes.TobinTaxes{}, nil)
}

func (s *KeeperTestSuite) requireDefaultRewardFunding() {
	funding, err := s.keeper.RewardFunding.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(types.DefaultRewardFundingState(), funding)
}

func (s *KeeperTestSuite) requireEvent(eventType string) {
	for _, event := range sdk.UnwrapSDKContext(s.ctx).EventManager().Events() {
		if event.Type == eventType {
			return
		}
	}
	s.Fail("event not found", eventType)
}

func (s *KeeperTestSuite) requireExactEvent(expected sdk.Event) {
	for _, event := range sdk.UnwrapSDKContext(s.ctx).EventManager().Events() {
		if event.Type == expected.Type {
			s.Require().Equal(expected, event)
			return
		}
	}
	s.Fail("event not found", expected.Type)
}

func (s *KeeperTestSuite) requireEventAttribute(eventType, key, value string) {
	for _, event := range sdk.UnwrapSDKContext(s.ctx).EventManager().Events() {
		if event.Type != eventType {
			continue
		}
		for _, attribute := range event.Attributes {
			if attribute.Key == key && attribute.Value == value {
				return
			}
		}
	}
	s.Fail("event attribute not found", "%s %s=%s", eventType, key, value)
}
