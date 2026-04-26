package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	core "noah/types"
	"noah/x/treasury/types"
)

func (s *KeeperTestSuite) TestSettleSeigniorage() {
	treasuryAddr := sdk.AccAddress{1}

	coin := func(amount int64) sdk.Coins {
		return sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(amount)))
	}

	tests := []struct {
		name                  string
		rewardWeight          math.LegacyDec
		burnWeight            *math.LegacyDec
		initialSupply         math.Int
		currentSupply         math.Int
		expectedMint          sdk.Coins
		expectedOracleReward  sdk.Coins
		expectedCommunityPool sdk.Coins
	}{
		{
			name:                  "default reward weight splits seigniorage",
			rewardWeight:          types.DefaultRewardWeight,
			initialSupply:         math.NewInt(10000),
			currentSupply:         math.NewInt(9000),
			expectedMint:          coin(900),
			expectedOracleReward:  coin(50),
			expectedCommunityPool: coin(850),
		},
		{
			name:          "zero seigniorage skips settlement",
			rewardWeight:  types.DefaultRewardWeight,
			initialSupply: math.NewInt(10000),
			currentSupply: math.NewInt(10000),
		},
		{
			name:                 "full reward weight sends all seigniorage to oracle",
			rewardWeight:         math.LegacyOneDec(),
			burnWeight:           func() *math.LegacyDec { w := math.LegacyZeroDec(); return &w }(),
			initialSupply:        math.NewInt(10000),
			currentSupply:        math.NewInt(9000),
			expectedMint:         coin(1000),
			expectedOracleReward: coin(1000),
		},
		{
			name:                  "zero reward weight sends all seigniorage to community pool",
			rewardWeight:          math.LegacyZeroDec(),
			initialSupply:         math.NewInt(10000),
			currentSupply:         math.NewInt(9000),
			expectedMint:          coin(900),
			expectedCommunityPool: coin(900),
		},
		{
			name:                  "small seigniorage truncates oracle reward to zero",
			rewardWeight:          types.DefaultRewardWeight,
			initialSupply:         math.NewInt(10000),
			currentSupply:         math.NewInt(9999),
			expectedMint:          coin(1),
			expectedCommunityPool: coin(1),
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.Require().NoError(s.keeper.RewardWeight.Set(s.ctx, tc.rewardWeight))
			params := types.DefaultParams()
			if tc.burnWeight != nil {
				params.BurnWeight = *tc.burnWeight
			}
			s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
			s.Require().NoError(s.keeper.EpochInitialIssuance.Set(s.ctx, types.EpochInitialIssuance{
				Issuance: sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, tc.initialSupply)),
			}))

			s.bankKeeper.EXPECT().GetSupply(gomock.Any(), core.MicroArkDenom).
				Return(sdk.NewCoin(core.MicroArkDenom, tc.currentSupply))

			if tc.expectedMint != nil {
				s.bankKeeper.EXPECT().
					MintCoins(gomock.Any(), types.ModuleName, tc.expectedMint).
					Return(nil)
			}

			if tc.expectedOracleReward != nil {
				s.bankKeeper.EXPECT().
					SendCoinsFromModuleToModule(gomock.Any(), types.ModuleName, "oracle", tc.expectedOracleReward).
					Return(nil)
			}

			if tc.expectedCommunityPool != nil {
				s.accountKeeper.EXPECT().GetModuleAddress(types.ModuleName).Return(treasuryAddr)
				s.distrKeeper.EXPECT().
					FundCommunityPool(gomock.Any(), tc.expectedCommunityPool, treasuryAddr).
					Return(nil)
			}

			err := s.keeper.SettleSeigniorage(s.ctx)
			s.Require().NoError(err)
		})
	}
}
