package keeper_test

import (
	"context"

	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	core "noah/types"
	"noah/x/treasury/types"
)

func (s *KeeperTestSuite) TestGetEpoch() {
	tests := []struct {
		name        string
		blockHeight int64
		expected    uint64
	}{
		{"block 0 — epoch 0", 0, 0},
		{"last block of epoch 0", int64(core.BlocksPerWeek) - 1, 0},
		{"first block of epoch 1", int64(core.BlocksPerWeek), 1},
		{"mid-epoch", int64(core.BlocksPerWeek) + int64(core.BlocksPerWeek/2), 1},
		{"epoch 3", int64(3 * core.BlocksPerWeek), 3},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.setBlockHeight(tc.blockHeight)
			s.Require().Equal(tc.expected, s.keeper.GetEpoch(s.ctx))
		})
	}
}

func (s *KeeperTestSuite) TestUpdateIndicators() {
	s.marketKeeper.EXPECT().
		ComputeOracleRate(gomock.Any(), gomock.Any(), core.MicroSDRDenom).
		DoAndReturn(func(_ context.Context, coin sdk.DecCoin, denom string) (sdk.DecCoin, error) {
			return sdk.NewDecCoinFromDec(denom, coin.Amount), nil
		}).
		AnyTimes()

	tests := []struct {
		name                      string
		totalBonded               math.Int
		taxProceeds               sdk.Coins
		initialArkSupply          math.Int
		currentArkSupply          math.Int
		expectedTaxReward         math.LegacyDec
		expectedSeigniorageReward math.LegacyDec
		expectedTotalStakedArk    math.Int
	}{
		{
			name:                      "tax proceeds and seigniorage",
			totalBonded:               math.NewInt(1000),
			taxProceeds:               sdk.NewCoins(sdk.NewCoin(core.MicroSDRDenom, math.NewInt(500))),
			initialArkSupply:          math.NewInt(1000),
			currentArkSupply:          math.NewInt(900),
			expectedTaxReward:         math.LegacyNewDec(500),
			expectedSeigniorageReward: math.LegacyNewDec(5),
			expectedTotalStakedArk:    math.NewInt(1000),
		},
		{
			name:                      "zero tax proceeds",
			totalBonded:               math.NewInt(1000),
			taxProceeds:               sdk.Coins{},
			initialArkSupply:          math.NewInt(1000),
			currentArkSupply:          math.NewInt(1000),
			expectedTaxReward:         math.LegacyZeroDec(),
			expectedSeigniorageReward: math.LegacyZeroDec(),
			expectedTotalStakedArk:    math.NewInt(1000),
		},
		{
			name:        "multi denom tax proceeds",
			totalBonded: math.NewInt(1000),
			taxProceeds: sdk.NewCoins(
				sdk.NewCoin(core.MicroUSDDenom, math.NewInt(300)),
				sdk.NewCoin(core.MicroSDRDenom, math.NewInt(200)),
			),
			initialArkSupply:          math.NewInt(1000),
			currentArkSupply:          math.NewInt(1000),
			expectedTaxReward:         math.LegacyNewDec(500),
			expectedSeigniorageReward: math.LegacyZeroDec(),
			expectedTotalStakedArk:    math.NewInt(1000),
		},
		{
			name:                      "zero bonded tokens",
			totalBonded:               math.ZeroInt(),
			taxProceeds:               sdk.NewCoins(sdk.NewCoin(core.MicroSDRDenom, math.NewInt(500))),
			initialArkSupply:          math.NewInt(1000),
			currentArkSupply:          math.NewInt(1000),
			expectedTaxReward:         math.LegacyNewDec(500),
			expectedSeigniorageReward: math.LegacyZeroDec(),
			expectedTotalStakedArk:    math.ZeroInt(),
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.setBlockHeight(0)

			s.stakingKeeper.EXPECT().TotalBondedTokens(gomock.Any()).
				Return(tc.totalBonded)

			s.Require().NoError(s.keeper.EpochTaxProceeds.Set(s.ctx, types.EpochTaxProceeds{
				TaxProceeds: tc.taxProceeds,
			}))
			s.Require().NoError(s.keeper.EpochInitialIssuance.Set(s.ctx, types.EpochInitialIssuance{
				Issuance: sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, tc.initialArkSupply)),
			}))
			s.bankKeeper.EXPECT().GetSupply(gomock.Any(), core.MicroArkDenom).
				Return(sdk.NewCoin(core.MicroArkDenom, tc.currentArkSupply))

			err := s.keeper.UpdateIndicators(s.ctx)
			s.Require().NoError(err)

			epochState, err := s.keeper.EpochStates.Get(s.ctx, 0)
			s.Require().NoError(err)
			s.Require().Equal(uint64(0), epochState.Epoch)
			s.Require().True(epochState.TaxReward.Equal(tc.expectedTaxReward),
				"expected tax reward %s, got %s", tc.expectedTaxReward, epochState.TaxReward)
			s.Require().True(epochState.SeigniorageReward.Equal(tc.expectedSeigniorageReward),
				"expected seigniorage reward %s, got %s", tc.expectedSeigniorageReward, epochState.SeigniorageReward)
			s.Require().Equal(tc.expectedTotalStakedArk, epochState.TotalStakedArk)

			proceeds, err := s.keeper.EpochTaxProceeds.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().True(proceeds.TaxProceeds.IsZero())
		})
	}
}
