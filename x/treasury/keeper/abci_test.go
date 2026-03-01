package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"

	core "noah/types"
	oracletypes "noah/x/oracle/types"
	"noah/x/treasury/types"
)

func (s *KeeperTestSuite) TestEndBlocker_NotEpochLastBlock() {
	// Block 100 is not the last block of any epoch
	s.setBlockHeight(100)
	err := s.treasuryKeeper.EndBlocker(s.ctx)
	s.Require().NoError(err)
	// No mock calls expected — EndBlocker is a no-op
}

func (s *KeeperTestSuite) TestEndBlocker_DuringProbation() {
	// Last block of epoch 0: height = BlocksPerWeek - 1
	// Default WindowProbation=12, so this is well within probation
	s.setBlockHeight(int64(core.BlocksPerWeek) - 1)

	// UpdateIndicators mocks
	s.stakingKeeper.EXPECT().TotalBondedTokens(gomock.Any()).Return(math.NewInt(1000))
	// No seigniorage (supply unchanged)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), core.MicroArkDenom).
		Return(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000000000000))).AnyTimes()
	// alignCoins for seigniorage reward (0 uark → usdr)
	s.marketKeeper.EXPECT().
		ComputeOracleRate(gomock.Any(), gomock.Any(), core.MicroSDRDenom).
		Return(sdk.NewDecCoinFromDec(core.MicroSDRDenom, math.LegacyZeroDec()), nil).AnyTimes()

	// Deferred RecordEpochInitialIssuance mocks
	s.oracleKeeper.EXPECT().Whitelist(gomock.Any()).Return(oracletypes.DenomList{})

	err := s.treasuryKeeper.EndBlocker(s.ctx)
	s.Require().NoError(err)

	// Verify indicators were updated (epoch state stored)
	epochState, err := s.treasuryKeeper.EpochStates.Get(s.ctx, 0)
	s.Require().NoError(err)
	s.Require().Equal(uint64(0), epochState.Epoch)

	// Verify tax rate unchanged (no policy update during probation)
	taxRate, _ := s.treasuryKeeper.TaxRate.Get(s.ctx)
	s.Require().True(taxRate.Equal(types.DefaultTaxRate))
}

func (s *KeeperTestSuite) TestEndBlocker_PolicyUpdate() {
	// Use small window params to simplify setup
	params := types.DefaultParams()
	params.WindowProbation = 1
	params.WindowShort = 2
	params.WindowLong = 4
	s.Require().NoError(s.treasuryKeeper.Params.Set(s.ctx, params))

	// Pre-populate epoch 0 state (zero rewards, as if EndBlocker ran at epoch 0)
	s.Require().NoError(s.treasuryKeeper.EpochStates.Set(s.ctx, 0, types.EpochState{
		Epoch:             0,
		TaxReward:         math.LegacyZeroDec(),
		SeigniorageReward: math.LegacyZeroDec(),
		TotalStakedArk:    math.NewInt(1000),
	}))

	// Capture old rates before EndBlocker
	oldTaxRate, _ := s.treasuryKeeper.TaxRate.Get(s.ctx)
	oldRewardWeight, _ := s.treasuryKeeper.RewardWeight.Get(s.ctx)

	// Last block of epoch 1: height = 2*BlocksPerWeek - 1
	// GetEpoch = (2*BlocksPerWeek - 1) / BlocksPerWeek = 1
	// Probation check: 2*BlocksPerWeek - 1 >= 1*BlocksPerWeek → past probation
	s.setBlockHeight(int64(2*core.BlocksPerWeek) - 1)

	// --- Mocks for the full EndBlocker cycle ---
	s.stakingKeeper.EXPECT().TotalBondedTokens(gomock.Any()).Return(math.NewInt(1000))
	// GetSupply called by UpdateIndicators, SettleSeigniorage, and RecordEpochInitialIssuance
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), gomock.Any()).
		Return(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000000000000))).AnyTimes()
	s.marketKeeper.EXPECT().
		ComputeOracleRate(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(sdk.NewDecCoinFromDec(core.MicroSDRDenom, math.LegacyZeroDec()), nil).AnyTimes()

	// SettleSeigniorage: no seigniorage (supply unchanged) → early return

	// UpdateTaxCap + RecordEpochInitialIssuance both call Whitelist
	s.oracleKeeper.EXPECT().Whitelist(gomock.Any()).
		Return(oracletypes.DenomList{}).AnyTimes()

	err := s.treasuryKeeper.EndBlocker(s.ctx)
	s.Require().NoError(err)

	// Zero tax proceeds → tax rate increases by ChangeRateMax (clamped)
	newTaxRate, _ := s.treasuryKeeper.TaxRate.Get(s.ctx)
	expectedTaxRate := oldTaxRate.Add(params.TaxPolicy.ChangeRateMax)
	s.Require().True(newTaxRate.Equal(expectedTaxRate),
		"expected tax rate %s, got %s", expectedTaxRate, newTaxRate)

	// Zero revenues → reward weight increases by ChangeRateMax (clamped)
	newRewardWeight, _ := s.treasuryKeeper.RewardWeight.Get(s.ctx)
	expectedRewardWeight := oldRewardWeight.Add(params.RewardPolicy.ChangeRateMax)
	s.Require().True(newRewardWeight.Equal(expectedRewardWeight),
		"expected reward weight %s, got %s", expectedRewardWeight, newRewardWeight)

	// Verify policy update event emitted
	var found bool
	for _, e := range s.ctx.EventManager().Events() {
		if e.Type == types.EventTypePolicyUpdate {
			found = true
			attrMap := make(map[string]string)
			for _, attr := range e.Attributes {
				attrMap[attr.Key] = attr.Value
			}
			s.Require().Equal(newTaxRate.String(), attrMap[types.AttributeKeyTaxRate])
			s.Require().Equal(newRewardWeight.String(), attrMap[types.AttributeKeyRewardWeight])
			break
		}
	}
	s.Require().True(found, "policy_update event not emitted")
}

func (s *KeeperTestSuite) TestEndBlocker_SparseEpochData() {
	// Tests behavior when the previous epoch's indicators are missing
	// (e.g. EndBlocker was skipped for that epoch, or chain upgrade).
	// This is the Noah equivalent of Terra's TestEmptyIndicator.
	//
	// Missing epoch states are skipped gracefully — the rolling average
	// and sum indicators only use epochs with actual data. With only
	// epoch 1 stored (zero revenue), tax rate should increase by ChangeRateMax.

	params := types.DefaultParams()
	params.WindowProbation = 1
	params.WindowShort = 2
	params.WindowLong = 4
	s.Require().NoError(s.treasuryKeeper.Params.Set(s.ctx, params))

	// Intentionally do NOT populate epoch 0 state.
	// Epoch 1's UpdateIndicators will store epoch 1, and
	// policy updates will skip the missing epoch 0.

	oldTaxRate, _ := s.treasuryKeeper.TaxRate.Get(s.ctx)

	s.setBlockHeight(int64(2*core.BlocksPerWeek) - 1)

	// UpdateIndicators mocks (these succeed — epoch 1 gets stored)
	s.stakingKeeper.EXPECT().TotalBondedTokens(gomock.Any()).Return(math.NewInt(1000))
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), gomock.Any()).
		Return(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000000000000))).AnyTimes()
	s.marketKeeper.EXPECT().
		ComputeOracleRate(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(sdk.NewDecCoinFromDec(core.MicroSDRDenom, math.LegacyZeroDec()), nil).AnyTimes()

	// SettleSeigniorage: no seigniorage → early return

	// UpdateTaxCap + RecordEpochInitialIssuance
	s.oracleKeeper.EXPECT().Whitelist(gomock.Any()).
		Return(oracletypes.DenomList{}).AnyTimes()

	err := s.treasuryKeeper.EndBlocker(s.ctx)
	s.Require().NoError(err)

	// Zero revenue in epoch 1 (only available data) → tax rate increases by ChangeRateMax
	newTaxRate, _ := s.treasuryKeeper.TaxRate.Get(s.ctx)
	expectedTaxRate := oldTaxRate.Add(params.TaxPolicy.ChangeRateMax)
	s.Require().True(newTaxRate.Equal(expectedTaxRate),
		"expected tax rate %s, got %s", expectedTaxRate, newTaxRate)
}

func (s *KeeperTestSuite) TestEndBlocker_MultipleEpochs() {
	// Run EndBlocker across multiple epochs and verify cumulative policy drift.
	// With zero revenue each epoch, tax rate should increase by ChangeRateMax each time.
	params := types.DefaultParams()
	params.WindowProbation = 0
	params.WindowShort = 2
	params.WindowLong = 4
	s.Require().NoError(s.treasuryKeeper.Params.Set(s.ctx, params))

	// Mocks that apply across all epochs
	s.stakingKeeper.EXPECT().TotalBondedTokens(gomock.Any()).Return(math.NewInt(1000)).AnyTimes()
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), gomock.Any()).
		Return(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000000000000))).AnyTimes()
	s.marketKeeper.EXPECT().
		ComputeOracleRate(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(sdk.NewDecCoinFromDec(core.MicroSDRDenom, math.LegacyZeroDec()), nil).AnyTimes()
	s.oracleKeeper.EXPECT().Whitelist(gomock.Any()).
		Return(oracletypes.DenomList{}).AnyTimes()
	s.bankKeeper.EXPECT().MintCoins(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	s.distrKeeper.EXPECT().GetFeePool(gomock.Any()).Return(distrtypes.FeePool{CommunityPool: sdk.DecCoins{}}).AnyTimes()
	s.distrKeeper.EXPECT().SetFeePool(gomock.Any(), gomock.Any()).AnyTimes()

	initialTaxRate, _ := s.treasuryKeeper.TaxRate.Get(s.ctx)

	// Run 3 epochs
	for epoch := 0; epoch < 3; epoch++ {
		s.setBlockHeight(int64((epoch+1)*int(core.BlocksPerWeek)) - 1)
		err := s.treasuryKeeper.EndBlocker(s.ctx)
		s.Require().NoError(err, "epoch %d", epoch)
	}

	// After 3 epochs of zero revenue, tax rate should have increased 3x ChangeRateMax
	finalTaxRate, _ := s.treasuryKeeper.TaxRate.Get(s.ctx)
	expectedIncrease := params.TaxPolicy.ChangeRateMax.MulInt64(3)
	s.Require().True(finalTaxRate.Equal(initialTaxRate.Add(expectedIncrease)),
		"expected tax rate %s, got %s", initialTaxRate.Add(expectedIncrease), finalTaxRate)
}
