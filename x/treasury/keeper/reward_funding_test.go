package keeper_test

import (
	"errors"
	"math/big"

	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	chain "github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/decimal"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	reservetypes "github.com/ararat-network/ark/x/reserve/types"
	"github.com/ararat-network/ark/x/treasury/types"
)

func (s *KeeperTestSuite) TestUpdateRewardFundingAccruesBlock() {
	s.setBlockHeight(2)
	policy := types.DefaultMonetaryPolicy()
	policy.ValidatorBlockRewardTarget = math.NewInt(7)
	policy.OracleBlockRewardTarget = math.NewInt(3)
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	s.expectValidatorFees(sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 5)))

	s.Require().NoError(s.advanceRewardFunding())
	funding, err := s.keeper.RewardFunding.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(types.DefaultRewardFundingWindow-1, funding.BlocksRemaining)
	s.Require().Equal(math.NewInt(7), funding.ValidatorTarget)
	s.Require().Equal(math.NewInt(3), funding.OracleTarget)
	s.Require().Equal(math.NewInt(5), funding.ValidatorFeeValue)
}

// TestUpdateRewardFundingSkipsParamsReadDuringActiveWindow pins D34: a window
// change applies only once the active countdown settles. The live window is
// seeded far away from the configured one, so a countdown that re-read params
// mid-window would jump to the configured value instead of ticking down.
func (s *KeeperTestSuite) TestUpdateRewardFundingSkipsParamsReadDuringActiveWindow() {
	s.setBlockHeight(2)
	params := types.DefaultParams()
	params.RewardFundingWindow = 999
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.setRewardFunding(rewardFunding(2, 0, 0, 0))
	s.expectValidatorFees(sdk.NewCoins())

	s.Require().NoError(s.advanceRewardFunding())
	funding, err := s.keeper.RewardFunding.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(uint64(1), funding.BlocksRemaining)
}

func (s *KeeperTestSuite) TestBeginBlockerDefersWindowChangeUntilNextWindow() {
	s.setBlockHeight(2)
	params := types.DefaultParams()
	params.ReferenceTaxCap = math.ZeroInt()
	params.RewardFundingWindow = 2
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.expectValidatorFees(sdk.NewCoins())

	s.Require().NoError(s.endBlock())
	funding, err := s.keeper.RewardFunding.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(uint64(1), funding.BlocksRemaining)

	params.RewardFundingWindow = 3
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.setBlockHeight(3)
	s.expectValidatorFees(sdk.NewCoins())
	s.expectStabilityTaxBalance(sdk.NewCoins())

	s.Require().NoError(s.endBlock())
	s.requireDefaultRewardFunding()

	s.setBlockHeight(4)
	s.expectValidatorFees(sdk.NewCoins())
	s.Require().NoError(s.endBlock())
	funding, err = s.keeper.RewardFunding.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(uint64(2), funding.BlocksRemaining)
}

func (s *KeeperTestSuite) TestBeginBlockerSettlesSingleBlockWindow() {
	s.setBlockHeight(2)
	params := types.DefaultParams()
	params.ReferenceTaxCap = math.ZeroInt()
	params.RewardFundingWindow = 1
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.expectValidatorFees(sdk.NewCoins())
	s.expectStabilityTaxBalance(sdk.NewCoins())

	s.Require().NoError(s.endBlock())
	s.requireDefaultRewardFunding()
}

func (s *KeeperTestSuite) TestBeginBlockerNetsFeesAcrossWindow() {
	s.setBlockHeight(2)
	policy := types.DefaultMonetaryPolicy()
	policy.ValidatorBlockRewardTarget = math.NewInt(100)
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	s.setRewardFunding(rewardFunding(2, 0, 0, 0))
	s.expectValidatorFees(sdk.NewCoins())

	s.Require().NoError(s.endBlock())

	s.setBlockHeight(3)
	s.expectValidatorFees(sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 200)))
	s.expectStabilityTaxBalance(sdk.NewCoins())
	s.expectSubsidyBalance(20)

	s.Require().NoError(s.endBlock())
	s.requireDefaultRewardFunding()
	s.requireTypedEvent(&types.EventBlockRewardsToppedUp{
		Denom:            chain.NoahBaseDenom,
		ValidatorTarget:  math.NewInt(200),
		OracleTarget:     math.ZeroInt(),
		ValidatorOrganic: math.NewInt(200),
		OracleOrganic:    math.ZeroInt(),
		ValidatorPaid:    math.ZeroInt(),
		OraclePaid:       math.ZeroInt(),
	})
}

func (s *KeeperTestSuite) TestSettleRewardFundingSendsAllTaxToOracleWhenFeesCoverTarget() {
	funding := rewardFunding(0, 7, 3, 7)
	stabilityTax := sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 5))
	s.expectStabilityTaxBalance(stabilityTax)
	s.expectSubsidyBalance(20)
	s.expectTaxAllocation(sdk.NewCoins(), stabilityTax)

	s.Require().NoError(s.runRewardFundingSettlement(funding))
	s.requireTypedEvent(&types.EventBlockRewardsToppedUp{
		Denom:            chain.NoahBaseDenom,
		ValidatorTarget:  math.NewInt(7),
		OracleTarget:     math.NewInt(3),
		ValidatorOrganic: math.NewInt(7),
		OracleOrganic:    math.NewInt(5),
		ValidatorPaid:    math.ZeroInt(),
		OraclePaid:       math.ZeroInt(),
	})
}

func (s *KeeperTestSuite) TestSettleRewardFundingProtectsOracleThenFundsValidatorGap() {
	funding := rewardFunding(0, 7, 3, 2)
	stabilityTax := sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 8))
	s.expectStabilityTaxBalance(stabilityTax)
	s.expectSubsidyBalance(20)
	s.expectTaxAllocation(
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 5)),
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 3)),
	)

	s.Require().NoError(s.runRewardFundingSettlement(funding))
}

func (s *KeeperTestSuite) TestSettleRewardFundingReturnsResidualTaxToOracle() {
	funding := rewardFunding(0, 7, 3, 5)
	stabilityTax := sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 10))
	s.expectStabilityTaxBalance(stabilityTax)
	s.expectSubsidyBalance(20)
	s.expectTaxAllocation(
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 2)),
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 8)),
	)

	s.Require().NoError(s.runRewardFundingSettlement(funding))
}

func (s *KeeperTestSuite) TestSettleRewardFundingPaysOnlyRemainingShortfalls() {
	funding := rewardFunding(0, 7, 3, 5)
	stabilityTax := sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1))
	s.expectStabilityTaxBalance(stabilityTax)
	s.expectSubsidyBalance(20)
	s.expectTaxAllocation(sdk.NewCoins(), stabilityTax)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.SubsidyPoolName, authtypes.FeeCollectorName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 2)),
	).Return(nil)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.SubsidyPoolName, oracletypes.ModuleName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 2)),
	).Return(nil)

	s.Require().NoError(s.runRewardFundingSettlement(funding))
}

func (s *KeeperTestSuite) TestSettleRewardFundingAllocatesScarceSubsidyByShortfall() {
	funding := rewardFunding(0, 6, 6, 2)
	stabilityTax := sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 4))
	s.expectStabilityTaxBalance(stabilityTax)
	s.expectSubsidyBalance(3)
	s.expectTaxAllocation(sdk.NewCoins(), stabilityTax)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.SubsidyPoolName, authtypes.FeeCollectorName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 2)),
	).Return(nil)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.SubsidyPoolName, oracletypes.ModuleName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1)),
	).Return(nil)

	s.Require().NoError(s.runRewardFundingSettlement(funding))
}

func (s *KeeperTestSuite) TestSettleRewardFundingConservesMultiDenomTaxAndRoundsToOracle() {
	funding := rewardFunding(0, 2, 3, 0)
	stabilityTax := sdk.NewCoins(
		sdk.NewInt64Coin(chain.XDRBaseDenom, 3),
		sdk.NewInt64Coin(chain.KRWBaseDenom, 2),
	)
	s.expectStabilityTaxBalance(stabilityTax)
	s.setRates(oracletypes.RateSet{
		chain.XDRBaseDenom: math.LegacyOneDec(),
		chain.KRWBaseDenom: math.LegacyOneDec(),
	})
	s.expectSubsidyBalance(10)
	s.expectTaxAllocation(
		sdk.NewCoins(sdk.NewInt64Coin(chain.XDRBaseDenom, 1)),
		sdk.NewCoins(
			sdk.NewInt64Coin(chain.XDRBaseDenom, 2),
			sdk.NewInt64Coin(chain.KRWBaseDenom, 2),
		),
	)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.SubsidyPoolName, authtypes.FeeCollectorName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1)),
	).Return(nil)

	s.Require().NoError(s.runRewardFundingSettlement(
		funding,
		chain.XDRBaseDenom,
		chain.KRWBaseDenom,
	))
}

// TestSettleRewardFundingAllocatesPricedTaxAndDefersStaleMember pins the mixed
// case: the priced member still funds the validator gap by value, while the
// stale-feed member's coins stay in the collector for a window that can price
// them instead of reaching the Oracle unvalued.
func (s *KeeperTestSuite) TestSettleRewardFundingAllocatesPricedTaxAndDefersStaleMember() {
	funding := rewardFunding(0, 2, 1, 0)
	stabilityTax := sdk.NewCoins(
		sdk.NewInt64Coin(chain.XDRBaseDenom, 3),
		sdk.NewInt64Coin(chain.KRWBaseDenom, 2),
	)
	s.expectStabilityTaxBalance(stabilityTax)
	// akrw is a member the Oracle cannot price this block, so it is omitted
	// from the available set and its tax defers.
	s.setRates(oracletypes.RateSet{chain.XDRBaseDenom: math.LegacyOneDec()})
	s.expectSubsidyBalance(10)
	s.expectTaxAllocation(
		sdk.NewCoins(sdk.NewInt64Coin(chain.XDRBaseDenom, 2)),
		sdk.NewCoins(sdk.NewInt64Coin(chain.XDRBaseDenom, 1)),
	)

	s.Require().NoError(s.runRewardFundingSettlement(
		funding,
		chain.XDRBaseDenom,
		chain.KRWBaseDenom,
	))
	s.requireTypedEvent(&types.EventUnpricedStabilityTaxRouted{
		Deferred: sdk.NewCoins(sdk.NewInt64Coin(chain.KRWBaseDenom, 2)),
	})
	s.requireTypedEvent(&types.EventBlockRewardsToppedUp{
		Denom:            chain.NoahBaseDenom,
		ValidatorTarget:  math.NewInt(2),
		OracleTarget:     math.OneInt(),
		ValidatorOrganic: math.NewInt(2),
		OracleOrganic:    math.OneInt(),
		ValidatorPaid:    math.ZeroInt(),
		OraclePaid:       math.ZeroInt(),
	})
}

// TestSettleRewardFundingMovesWrittenOffTaxToReserve pins the dead branch:
// written-off supply has no feed to wait for, so its tax moves to the
// strategic reserve, credits no target, and the window still settles with the
// shortfall paid from the subsidy pool.
func (s *KeeperTestSuite) TestSettleRewardFundingMovesWrittenOffTaxToReserve() {
	s.setBlockHeight(2)
	funding := rewardFunding(1, 0, 3, 0)
	s.setRewardFunding(funding)
	s.setAssets()
	s.seedAsset(chain.USDBaseDenom, assettypes.AssetStatus_ASSET_STATUS_WRITTEN_OFF)
	s.expectValidatorFees(sdk.NewCoins())
	stabilityTax := sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 4))
	s.expectStabilityTaxBalance(stabilityTax)
	s.expectUnconfiguredRewardValuation()
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.StabilityTaxCollectorName, reservetypes.StrategicReserveName, stabilityTax,
	).Return(nil)
	s.expectSubsidyBalance(10)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.SubsidyPoolName, oracletypes.ModuleName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 3)),
	).Return(nil)

	s.Require().NoError(s.endBlock())
	s.requireTypedEvent(&types.EventUnpricedStabilityTaxRouted{
		Moved: stabilityTax,
	})
	s.requireTypedEvent(&types.EventBlockRewardsToppedUp{
		Denom:            chain.NoahBaseDenom,
		ValidatorTarget:  math.ZeroInt(),
		OracleTarget:     math.NewInt(3),
		ValidatorOrganic: math.ZeroInt(),
		OracleOrganic:    math.ZeroInt(),
		ValidatorPaid:    math.ZeroInt(),
		OraclePaid:       math.NewInt(3),
	})
}

// TestSettleRewardFundingDefersSuspendedTax pins the recoverable branch: a
// suspended asset keeps its recovery path, so its tax waits in the collector
// rather than moving to the reserve or reaching the Oracle unvalued.
func (s *KeeperTestSuite) TestSettleRewardFundingDefersSuspendedTax() {
	s.setBlockHeight(2)
	funding := rewardFunding(1, 0, 0, 0)
	s.setRewardFunding(funding)
	s.setAssets()
	s.seedAsset(chain.XDRBaseDenom, assettypes.AssetStatus_ASSET_STATUS_SUSPENDED)
	s.expectValidatorFees(sdk.NewCoins())
	stabilityTax := sdk.NewCoins(sdk.NewInt64Coin(chain.XDRBaseDenom, 4))
	s.expectStabilityTaxBalance(stabilityTax)
	s.expectUnconfiguredRewardValuation()

	s.Require().NoError(s.endBlock())
	s.requireTypedEvent(&types.EventUnpricedStabilityTaxRouted{
		Deferred: stabilityTax,
	})
}

// TestSettleRewardFundingPricesSettlingTaxAtPlanRate pins the settlement rate:
// a suspended asset carrying a governance-committed redemption rate
// is priced by that rate, so its tax funds targets and splits between
// validators and the Oracle like any priced denomination rather than
// deferring. The plan is deliberately unactivated, pinning the decision to
// read the commitment rather than its activation height — the same reading the
// liability partition takes.
func (s *KeeperTestSuite) TestSettleRewardFundingPricesSettlingTaxAtPlanRate() {
	s.setBlockHeight(2)
	s.setRewardFunding(rewardFunding(1, 5, 3, 0))
	s.setAssets()
	s.seedAsset(chain.USDBaseDenom, assettypes.AssetStatus_ASSET_STATUS_SUSPENDED)
	s.plans[chain.USDBaseDenom] = usdSettlementPlan()
	s.expectValidatorFees(sdk.NewCoins())
	stabilityTax := sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 4))
	s.expectStabilityTaxBalance(stabilityTax)
	s.expectUnconfiguredRewardValuation()
	s.expectSubsidyBalance(10)
	// The plan redeems two NOAH per unit, so four units value at eight NOAH:
	// enough to cover the Oracle target and five of the validator target, which
	// splits the balance evenly and leaves one NOAH of validator shortfall.
	s.expectTaxAllocation(
		sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 2)),
		sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 2)),
	)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.SubsidyPoolName, authtypes.FeeCollectorName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1)),
	).Return(nil)

	s.Require().NoError(s.endBlock())
	s.requireNoTypedEvent(&types.EventUnpricedStabilityTaxRouted{})
	s.requireTypedEvent(&types.EventBlockRewardsToppedUp{
		Denom:            chain.NoahBaseDenom,
		ValidatorTarget:  math.NewInt(5),
		OracleTarget:     math.NewInt(3),
		ValidatorOrganic: math.NewInt(4),
		OracleOrganic:    math.NewInt(4),
		ValidatorPaid:    math.OneInt(),
		OraclePaid:       math.ZeroInt(),
	})
}

// TestUpdateRewardFundingValuesSettlingFeesAtPlanRate pins the settlement rate
// on the fee side: gas paid in a settling denomination counts toward organic
// validator rewards at the committed redemption rate instead of as zero, so
// the subsidy is not asked to fund value validators already received.
func (s *KeeperTestSuite) TestUpdateRewardFundingValuesSettlingFeesAtPlanRate() {
	s.setBlockHeight(2)
	s.setAssets()
	s.seedAsset(chain.USDBaseDenom, assettypes.AssetStatus_ASSET_STATUS_SUSPENDED)
	s.plans[chain.USDBaseDenom] = usdSettlementPlan()
	s.expectValidatorFees(sdk.NewCoins(
		sdk.NewInt64Coin(chain.NoahBaseDenom, 5),
		sdk.NewInt64Coin(chain.USDBaseDenom, 4),
	))
	s.expectUnconfiguredRewardValuation()

	s.Require().NoError(s.endBlock())
	funding, err := s.keeper.RewardFunding.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(types.DefaultRewardFundingWindow-1, funding.BlocksRemaining)
	s.Require().Equal(math.NewInt(13), funding.ValidatorFeeValue)
}

// TestUpdateRewardFundingCountsUnpricedFeeDenomsAsZero pins the per-denom
// skip: an oracle-priced member whose feed is stale is omitted from the available
// rate set, so its fees count as zero while the priced remainder still
// accrues. Dust of a stale-feed member in the fee collector must not suppress
// a whole window's top-ups.
func (s *KeeperTestSuite) TestUpdateRewardFundingCountsUnpricedFeeDenomsAsZero() {
	s.setBlockHeight(2)
	validatorFees := sdk.NewCoins(
		sdk.NewInt64Coin(chain.NoahBaseDenom, 5),
		sdk.NewInt64Coin(chain.XDRBaseDenom, 4),
	)
	s.expectValidatorFees(validatorFees)
	s.setRates(oracletypes.RateSet{})

	s.Require().NoError(s.advanceRewardFunding(
		chain.XDRBaseDenom,
	))
	funding, err := s.keeper.RewardFunding.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(types.DefaultRewardFundingWindow-1, funding.BlocksRemaining)
	s.Require().Equal(math.NewInt(5), funding.ValidatorFeeValue)
}

// TestUpdateRewardFundingCountsSubUnitFeeDustAsZero pins measurement
// semantics for a priced member: dust whose NOAH value truncates below one
// base unit counts as zero, exactly like an unpriced member, rather than
// failing the valuation of every other denomination carried with it.
func (s *KeeperTestSuite) TestUpdateRewardFundingCountsSubUnitFeeDustAsZero() {
	s.setBlockHeight(2)
	validatorFees := sdk.NewCoins(
		sdk.NewInt64Coin(chain.NoahBaseDenom, 5),
		sdk.NewInt64Coin(chain.XDRBaseDenom, 1),
	)
	s.expectValidatorFees(validatorFees)
	s.setRates(oracletypes.RateSet{chain.XDRBaseDenom: math.LegacySmallestDec()})

	s.Require().NoError(s.advanceRewardFunding(
		chain.XDRBaseDenom,
	))
	funding, err := s.keeper.RewardFunding.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(types.DefaultRewardFundingWindow-1, funding.BlocksRemaining)
	s.Require().Equal(math.NewInt(5), funding.ValidatorFeeValue)
}

// TestUpdateRewardFundingFailsBlockWhenFeeValueAggregateOverflows pins the
// remaining valuation failure as a bug rather than weather. Availability is
// settled before valuation, so only arithmetic beyond any reachable supply is
// left, and the block fails instead of degrading the window's accounting.
func (s *KeeperTestSuite) TestUpdateRewardFundingFailsBlockWhenFeeValueAggregateOverflows() {
	s.setBlockHeight(2)
	max := maxRepresentableInt()
	validatorFees := sdk.NewCoins(
		sdk.NewCoin(chain.NoahBaseDenom, max),
		sdk.NewCoin(chain.XDRBaseDenom, max),
	)
	s.expectValidatorFees(validatorFees)
	s.expectNoahAndXDRRewardValuation()

	err := s.advanceRewardFunding(chain.XDRBaseDenom)
	s.Require().ErrorContains(err, "valuing validator fees")
	s.Require().ErrorIs(err, decimal.ErrOutOfRange)
}

func (s *KeeperTestSuite) TestUpdateRewardFundingFailsBlockWhenCrossBlockSumOverflows() {
	s.setBlockHeight(2)
	max := maxRepresentableInt()
	s.setRewardFunding(types.RewardFundingState{
		BlocksRemaining:   2,
		ValidatorTarget:   math.ZeroInt(),
		OracleTarget:      math.ZeroInt(),
		ValidatorFeeValue: max,
	})
	s.expectValidatorFees(sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1)))

	err := s.advanceRewardFunding()
	s.Require().ErrorContains(err, "adding validator fee value")
}

// TestSettleRewardFundingDefersStaleMemberTaxAndStillTopsUp pins settlement
// when the whole tax balance belongs to a stale-feed member: the coins wait in
// the collector for a window that can price them, and the window still
// settles — both shortfalls are paid from the subsidy pool instead of being
// skipped.
func (s *KeeperTestSuite) TestSettleRewardFundingDefersStaleMemberTaxAndStillTopsUp() {
	funding := rewardFunding(0, 1, 1, 0)
	stabilityTax := sdk.NewCoins(sdk.NewInt64Coin(chain.XDRBaseDenom, 4))
	s.expectStabilityTaxBalance(stabilityTax)
	s.setRates(oracletypes.RateSet{})
	s.expectSubsidyBalance(20)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.SubsidyPoolName, authtypes.FeeCollectorName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1)),
	).Return(nil)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.SubsidyPoolName, oracletypes.ModuleName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1)),
	).Return(nil)

	s.Require().NoError(s.runRewardFundingSettlement(
		funding,
		chain.XDRBaseDenom,
	))
	s.requireTypedEvent(&types.EventUnpricedStabilityTaxRouted{
		Deferred: stabilityTax,
	})
	s.requireTypedEvent(&types.EventBlockRewardsToppedUp{
		Denom:            chain.NoahBaseDenom,
		ValidatorTarget:  math.OneInt(),
		OracleTarget:     math.OneInt(),
		ValidatorOrganic: math.ZeroInt(),
		OracleOrganic:    math.ZeroInt(),
		ValidatorPaid:    math.OneInt(),
		OraclePaid:       math.OneInt(),
	})
}

func (s *KeeperTestSuite) TestSettleRewardFundingFailsBlockWhenTaxValueAggregateOverflows() {
	max := maxRepresentableInt()
	funding := rewardFunding(0, 1, 1, 0)
	stabilityTax := sdk.NewCoins(
		sdk.NewCoin(chain.NoahBaseDenom, max),
		sdk.NewCoin(chain.XDRBaseDenom, max),
	)
	s.expectStabilityTaxBalance(stabilityTax)
	s.expectNoahAndXDRRewardValuation()

	err := s.runRewardFundingSettlement(funding, chain.XDRBaseDenom)
	s.Require().ErrorContains(err, "valuing stability tax")
	s.Require().ErrorIs(err, decimal.ErrOutOfRange)
}

func (s *KeeperTestSuite) TestSettleRewardFundingSendsTaxToOracleWhenTargetsAreDisabled() {
	funding := rewardFunding(0, 0, 0, 0)
	stabilityTax := sdk.NewCoins(sdk.NewInt64Coin(chain.XDRBaseDenom, 4))
	s.expectStabilityTaxBalance(stabilityTax)
	s.expectNoahAndXDRRewardValuation()
	s.expectTaxAllocation(sdk.NewCoins(), stabilityTax)

	s.Require().NoError(s.runRewardFundingSettlement(
		funding,
		chain.XDRBaseDenom,
	))
}

func (s *KeeperTestSuite) TestBeginBlockerDoesNotClearAfterAllocationFailure() {
	initial := rewardFunding(1, 0, 0, 1)
	s.setBlockHeight(2)
	s.setRewardFunding(initial)
	s.expectValidatorFees(sdk.NewCoins())
	stabilityTax := sdk.NewCoins(sdk.NewInt64Coin(chain.XDRBaseDenom, 4))
	s.expectStabilityTaxBalance(stabilityTax)
	s.expectNoahAndXDRRewardValuation()
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.StabilityTaxCollectorName, oracletypes.ModuleName, stabilityTax,
	).Return(errors.New("bank failure"))

	err := s.endBlock()
	s.Require().ErrorContains(err, "allocating stability tax to Oracle")
	funding, getErr := s.keeper.RewardFunding.Get(s.ctx)
	s.Require().NoError(getErr)
	s.Require().Zero(funding.BlocksRemaining)
	s.Require().Equal(math.OneInt(), funding.ValidatorFeeValue)
}

func (s *KeeperTestSuite) advanceRewardFunding(denoms ...string) error {
	s.expectRewardFundingConfiguration(denoms)
	return s.endBlock()
}

func (s *KeeperTestSuite) runRewardFundingSettlement(
	funding types.RewardFundingState,
	denoms ...string,
) error {
	s.setBlockHeight(2)
	funding.BlocksRemaining = 1
	s.setRewardFunding(funding)
	s.expectValidatorFees(sdk.NewCoins())
	return s.advanceRewardFunding(denoms...)
}

// expectRewardFundingConfiguration pins the oracle-priced membership — the set
// reward valuation admits — to exactly the given denominations and gives each
// a held conversion factor, so the per-block pass has nothing to seed.
func (s *KeeperTestSuite) expectRewardFundingConfiguration(denoms []string) {
	s.setAssets(denoms...)
	for _, denom := range denoms {
		s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, denom, types.ConversionFactor{
			Denom:  denom,
			Factor: math.LegacyOneDec(),
		}))
	}
}

func (s *KeeperTestSuite) setRewardFunding(funding types.RewardFundingState) {
	s.Require().NoError(s.keeper.RewardFunding.Set(s.ctx, funding))
}

func rewardFunding(blocksRemaining uint64, validatorTarget, oracleTarget, validatorFees int64) types.RewardFundingState {
	return types.RewardFundingState{
		BlocksRemaining:   blocksRemaining,
		ValidatorTarget:   math.NewInt(validatorTarget),
		OracleTarget:      math.NewInt(oracleTarget),
		ValidatorFeeValue: math.NewInt(validatorFees),
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
		chain.NoahBaseDenom,
	).Return(sdk.NewInt64Coin(chain.NoahBaseDenom, amount))
}

func (s *KeeperTestSuite) expectUnconfiguredRewardValuation() {
	s.setRates(oracletypes.RateSet{})
}

func (s *KeeperTestSuite) expectNoahAndXDRRewardValuation() {
	s.setRates(oracletypes.RateSet{chain.XDRBaseDenom: math.LegacyOneDec()})
}

func maxRepresentableInt() math.Int {
	max := new(big.Int).Lsh(big.NewInt(1), math.MaxBitLen)
	return math.NewIntFromBigInt(max.Sub(max, big.NewInt(1)))
}

func (s *KeeperTestSuite) requireDefaultRewardFunding() {
	funding, err := s.keeper.RewardFunding.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(types.DefaultRewardFundingState(), funding)
}
