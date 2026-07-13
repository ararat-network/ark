package keeper_test

import (
	"context"

	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	oracletypes "ark/x/oracle/types"
	"ark/x/treasury/types"
)

func (s *KeeperTestSuite) TestEndBlocker_NotEpochLastBlock() {
	// Block 100 is not the last block of any epoch
	s.setBlockHeight(100)
	err := s.keeper.EndBlocker(s.ctx)
	s.Require().NoError(err)
	// No mock calls expected — EndBlocker is a no-op
}

func (s *KeeperTestSuite) TestEndBlocker_DuringProbation() {
	// Last block of epoch 0: height = BlocksPerWeek - 1
	// Default WindowProbation=12, so this is well within probation
	s.setBlockHeight(int64(chain.BlocksPerWeek) - 1)

	// UpdateIndicators mocks
	s.stakingKeeper.EXPECT().TotalValidatorPower(gomock.Any()).Return(math.NewInt(1000), nil)
	// No seigniorage (supply unchanged)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.MicroNoahDenom).
		Return(sdk.NewCoin(chain.MicroNoahDenom, math.NewInt(1000000000000))).AnyTimes()
	s.oracleKeeper.EXPECT().
		GetRateSnapshot(gomock.Any(), chain.MicroNoahDenom, chain.MicroSDRDenom).
		Return(oracletypes.RateSnapshot{
			chain.MicroNoahDenom: math.LegacyOneDec(),
			chain.MicroSDRDenom:  math.LegacyOneDec(),
		}, nil)

	// Deferred RecordEpochInitialIssuance mocks
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(oracletypes.TobinTaxes{}, nil)

	err := s.keeper.EndBlocker(s.ctx)
	s.Require().NoError(err)

	// Verify indicators were updated (epoch state stored)
	epochState, err := s.keeper.EpochStates.Get(s.ctx, 0)
	s.Require().NoError(err)
	s.Require().Equal(uint64(0), epochState.Epoch)

	// Verify tax rate unchanged (no policy update during probation)
	taxRate, _ := s.keeper.TaxRate.Get(s.ctx)
	s.Require().True(taxRate.Equal(types.DefaultTaxRate))
}

func (s *KeeperTestSuite) TestEndBlocker_PolicyUpdate() {
	// Use small window params to simplify setup
	params := types.DefaultParams()
	params.WindowProbation = 1
	params.WindowShort = 2
	// No mock calls expected — EndBlocker is a no-op
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

	// Pre-populate epoch 0 state (zero rewards, as if EndBlocker ran at epoch 0)
	s.Require().NoError(s.keeper.EpochStates.Set(s.ctx, 0, types.EpochState{
		Epoch:             0,
		TaxReward:         math.LegacyZeroDec(),
		SeigniorageReward: math.LegacyZeroDec(),
		TotalStakedNoah:   math.NewInt(1000),
	}))

	// Capture old rates before EndBlocker
	oldTaxRate, _ := s.keeper.TaxRate.Get(s.ctx)
	oldRewardWeight, _ := s.keeper.RewardWeight.Get(s.ctx)

	// Last block of epoch 1: height = 2*BlocksPerWeek - 1
	// GetEpoch = (2*BlocksPerWeek - 1) / BlocksPerWeek = 1
	// Probation check: 2*BlocksPerWeek - 1 >= 1*BlocksPerWeek → past probation
	s.setBlockHeight(int64(2*chain.BlocksPerWeek) - 1)

	// --- Mocks for the full EndBlocker cycle ---
	expectedTaxCap := math.NewInt(2_000_000)
	s.stakingKeeper.EXPECT().TotalValidatorPower(gomock.Any()).Return(math.NewInt(1000), nil)
	// GetSupply called by UpdateIndicators, SettleSeigniorage, and RecordEpochInitialIssuance
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, denom string) sdk.Coin {
			return sdk.NewCoin(denom, math.NewInt(1000000000000))
		}).AnyTimes()
	// SettleSeigniorage: no seigniorage (supply unchanged) → early return

	// Tax-cap computation and RecordEpochInitialIssuance both read Tobin taxes.
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).
		Return(oracletypes.TobinTaxes{
			{Denom: chain.MicroSDRDenom},
			{Denom: chain.MicroUSDDenom},
		}, nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetRateSnapshot(
		gomock.Any(),
		chain.MicroNoahDenom,
		chain.MicroSDRDenom,
		params.TaxPolicy.Cap.Denom,
		chain.MicroSDRDenom,
		chain.MicroUSDDenom,
	).Return(oracletypes.RateSnapshot{
		chain.MicroNoahDenom: math.LegacyOneDec(),
		chain.MicroSDRDenom:  math.LegacyOneDec(),
		chain.MicroUSDDenom:  math.LegacyNewDec(2),
	}, nil)

	err := s.keeper.EndBlocker(s.ctx)
	s.Require().NoError(err)

	// Zero tax proceeds → tax rate increases by ChangeRateMax (clamped)
	newTaxRate, _ := s.keeper.TaxRate.Get(s.ctx)
	expectedTaxRate := oldTaxRate.Add(params.TaxPolicy.ChangeRateMax)
	s.Require().True(newTaxRate.Equal(expectedTaxRate),
		"expected tax rate %s, got %s", expectedTaxRate, newTaxRate)

	// Zero revenues → reward weight increases by ChangeRateMax (clamped)
	newRewardWeight, _ := s.keeper.RewardWeight.Get(s.ctx)
	expectedRewardWeight := oldRewardWeight.Add(params.RewardPolicy.ChangeRateMax)
	s.Require().True(newRewardWeight.Equal(expectedRewardWeight),
		"expected reward weight %s, got %s", expectedRewardWeight, newRewardWeight)

	// Verify non-SDR tax caps were converted and stored.
	taxCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.MicroUSDDenom)
	s.Require().NoError(err)
	s.Require().Equal(expectedTaxCap, taxCap)
	expectedTaxCaps := sdk.NewCoins(sdk.NewCoin(chain.MicroUSDDenom, expectedTaxCap))

	// Verify policy update event emitted
	var found bool
	sdkCtx := sdk.UnwrapSDKContext(s.ctx)
	for _, e := range sdkCtx.EventManager().Events() {
		if e.Type == types.EventTypePolicyUpdate {
			found = true
			attrMap := make(map[string]string)
			for _, attr := range e.Attributes {
				attrMap[attr.Key] = attr.Value
			}
			s.Require().Equal(newTaxRate.String(), attrMap[types.AttributeKeyTaxRate])
			s.Require().Equal(newRewardWeight.String(), attrMap[types.AttributeKeyRewardWeight])
			s.Require().Equal(expectedTaxCaps.String(), attrMap[types.AttributeKeyTaxCap])
			break
		}
	}
	s.Require().True(found, "policy_update event not emitted")
}

func (s *KeeperTestSuite) TestEndBlocker_SparseEpochData() {
	// Tests behaviour when the previous epoch's indicators are missing
	// (e.g. EndBlocker was skipped for that epoch, or chain upgrade).
	//
	// Missing epoch states are skipped gracefully — the rolling average
	// and sum indicators only use epochs with actual data. With only
	// epoch 1 stored (zero revenue), tax rate should increase by ChangeRateMax.

	params := types.DefaultParams()
	params.WindowProbation = 1
	params.WindowShort = 2
	params.WindowLong = 4
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

	// Intentionally do NOT populate epoch 0 state.
	// Epoch 1's UpdateIndicators will store epoch 1, and
	// policy updates will skip the missing epoch 0.

	oldTaxRate, _ := s.keeper.TaxRate.Get(s.ctx)

	s.setBlockHeight(int64(2*chain.BlocksPerWeek) - 1)

	// UpdateIndicators mocks (these succeed — epoch 1 gets stored)
	s.stakingKeeper.EXPECT().TotalValidatorPower(gomock.Any()).Return(math.NewInt(1000), nil)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), gomock.Any()).
		Return(sdk.NewCoin(chain.MicroNoahDenom, math.NewInt(1000000000000))).AnyTimes()
	// SettleSeigniorage: no seigniorage → early return

	// Tax-cap computation and RecordEpochInitialIssuance.
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).
		Return(oracletypes.TobinTaxes{}, nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetRateSnapshot(
		gomock.Any(),
		chain.MicroNoahDenom,
		chain.MicroSDRDenom,
		params.TaxPolicy.Cap.Denom,
	).Return(oracletypes.RateSnapshot{
		chain.MicroNoahDenom: math.LegacyOneDec(),
		chain.MicroSDRDenom:  math.LegacyOneDec(),
	}, nil)

	err := s.keeper.EndBlocker(s.ctx)
	s.Require().NoError(err)

	// Zero revenue in epoch 1 (only available data) → tax rate increases by ChangeRateMax
	newTaxRate, _ := s.keeper.TaxRate.Get(s.ctx)
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
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

	// Mocks that apply across all epochs
	s.stakingKeeper.EXPECT().TotalValidatorPower(gomock.Any()).Return(math.NewInt(1000), nil).AnyTimes()
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), gomock.Any()).
		Return(sdk.NewCoin(chain.MicroNoahDenom, math.NewInt(1000000000000))).AnyTimes()
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).
		Return(oracletypes.TobinTaxes{}, nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetRateSnapshot(
		gomock.Any(),
		chain.MicroNoahDenom,
		chain.MicroSDRDenom,
		params.TaxPolicy.Cap.Denom,
	).Return(oracletypes.RateSnapshot{
		chain.MicroNoahDenom: math.LegacyOneDec(),
		chain.MicroSDRDenom:  math.LegacyOneDec(),
	}, nil).AnyTimes()
	s.bankKeeper.EXPECT().MintCoins(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	s.accountKeeper.EXPECT().GetModuleAddress(types.ModuleName).Return(sdk.AccAddress{1}).AnyTimes()
	s.ppoolKeeper.EXPECT().FundCommunityPool(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	initialTaxRate, _ := s.keeper.TaxRate.Get(s.ctx)

	// Run 3 epochs
	for epoch := range 3 {
		s.setBlockHeight(int64((epoch+1)*int(chain.BlocksPerWeek)) - 1)
		err := s.keeper.EndBlocker(s.ctx)
		s.Require().NoError(err, "epoch %d", epoch)
	}

	// After 3 epochs of zero revenue, tax rate should have increased 3x ChangeRateMax
	finalTaxRate, _ := s.keeper.TaxRate.Get(s.ctx)
	expectedIncrease := params.TaxPolicy.ChangeRateMax.MulInt64(3)
	s.Require().True(finalTaxRate.Equal(initialTaxRate.Add(expectedIncrease)),
		"expected tax rate %s, got %s", initialTaxRate.Add(expectedIncrease), finalTaxRate)
}

func (s *KeeperTestSuite) TestEndBlocker_StaleRatesPreservePolicyAndProceeds() {
	s.setBlockHeight(int64(chain.BlocksPerWeek) - 1)
	taxProceeds := sdk.NewCoins(sdk.NewInt64Coin(chain.MicroUSDDenom, 300))
	s.Require().NoError(s.keeper.EpochTaxProceeds.Set(s.ctx, types.EpochTaxProceeds{
		TaxProceeds: taxProceeds,
	}))
	oldTaxRate, err := s.keeper.TaxRate.Get(s.ctx)
	s.Require().NoError(err)

	s.oracleKeeper.EXPECT().GetRateSnapshot(
		gomock.Any(),
		chain.MicroNoahDenom,
		chain.MicroSDRDenom,
		chain.MicroUSDDenom,
	).Return(nil, oracletypes.ErrStaleExchangeRate)
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(oracletypes.TobinTaxes{}, nil)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.MicroNoahDenom).
		Return(sdk.NewInt64Coin(chain.MicroNoahDenom, 1000))

	s.Require().NoError(s.keeper.EndBlocker(s.ctx))

	proceeds, err := s.keeper.EpochTaxProceeds.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(taxProceeds, proceeds.TaxProceeds)
	newTaxRate, err := s.keeper.TaxRate.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(oldTaxRate.Equal(newTaxRate))
	hasEpochState, err := s.keeper.EpochStates.Has(s.ctx, 0)
	s.Require().NoError(err)
	s.Require().False(hasEpochState)
	for _, event := range sdk.UnwrapSDKContext(s.ctx).EventManager().Events() {
		s.Require().NotEqual(types.EventTypePolicyUpdate, event.Type)
	}
}
