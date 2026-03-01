package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"

	core "noah/types"
	"noah/x/treasury/types"
)

func (s *KeeperTestSuite) TestSettleSeigniorage() {
	// Set up: seigniorage = 1000 uark, rewardWeight = 5%
	s.Require().NoError(s.treasuryKeeper.EpochInitialIssuance.Set(s.ctx, types.EpochInitialIssuance{
		Issuance: sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(10000))),
	}))
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), core.MicroArkDenom).
		Return(sdk.NewCoin(core.MicroArkDenom, math.NewInt(9000)))
	// seigniorage = 10000 - 9000 = 1000

	// Mint seigniorage
	seigniorageCoins := sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000)))
	s.bankKeeper.EXPECT().
		MintCoins(gomock.Any(), types.ModuleName, seigniorageCoins).
		Return(nil)

	// Oracle reward: rewardWeight(5%) * 1000 = 50
	oracleRewardCoins := sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(50)))
	s.bankKeeper.EXPECT().
		SendCoinsFromModuleToModule(gomock.Any(), types.ModuleName, "oracle", oracleRewardCoins).
		Return(nil)

	// Community pool: 1000 - 50 = 950
	communityCoins := sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(950)))
	s.bankKeeper.EXPECT().
		SendCoinsFromModuleToModule(gomock.Any(), types.ModuleName, "distribution", communityCoins).
		Return(nil)

	// Distribution keeper: verify exact community pool amount
	s.distrKeeper.EXPECT().GetFeePool(gomock.Any()).
		Return(distrtypes.FeePool{CommunityPool: sdk.DecCoins{}})
	expectedPool := distrtypes.FeePool{
		CommunityPool: sdk.NewDecCoinsFromCoins(communityCoins...),
	}
	s.distrKeeper.EXPECT().SetFeePool(gomock.Any(), expectedPool)

	err := s.treasuryKeeper.SettleSeigniorage(s.ctx)
	s.Require().NoError(err)
}

func (s *KeeperTestSuite) TestSettleSeigniorage_ZeroSeigniorage() {
	// No seigniorage (supply didn't decrease)
	s.Require().NoError(s.treasuryKeeper.EpochInitialIssuance.Set(s.ctx, types.EpochInitialIssuance{
		Issuance: sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(10000))),
	}))
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), core.MicroArkDenom).
		Return(sdk.NewCoin(core.MicroArkDenom, math.NewInt(10000)))

	err := s.treasuryKeeper.SettleSeigniorage(s.ctx)
	s.Require().NoError(err)
	// No mint/send should be called (gomock would fail if unexpected calls were made)
}

func (s *KeeperTestSuite) TestSettleSeigniorage_FullRewardWeight() {
	// When rewardWeight = 100%, all seigniorage goes to oracle, nothing to community pool
	s.Require().NoError(s.treasuryKeeper.RewardWeight.Set(s.ctx, math.LegacyOneDec()))

	s.Require().NoError(s.treasuryKeeper.EpochInitialIssuance.Set(s.ctx, types.EpochInitialIssuance{
		Issuance: sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(10000))),
	}))
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), core.MicroArkDenom).
		Return(sdk.NewCoin(core.MicroArkDenom, math.NewInt(9000)))

	// Mint seigniorage
	seigniorageCoins := sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000)))
	s.bankKeeper.EXPECT().
		MintCoins(gomock.Any(), types.ModuleName, seigniorageCoins).
		Return(nil)

	// All to oracle: rewardWeight(100%) * 1000 = 1000
	oracleRewardCoins := sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000)))
	s.bankKeeper.EXPECT().
		SendCoinsFromModuleToModule(gomock.Any(), types.ModuleName, "oracle", oracleRewardCoins).
		Return(nil)

	// leftAmt = 0 → distribution path is skipped (no send, no fee pool update)

	err := s.treasuryKeeper.SettleSeigniorage(s.ctx)
	s.Require().NoError(err)
}

func (s *KeeperTestSuite) TestSettleSeigniorage_ZeroRewardWeight() {
	// When rewardWeight = 0%, all seigniorage goes to community pool, nothing to oracle
	s.Require().NoError(s.treasuryKeeper.RewardWeight.Set(s.ctx, math.LegacyZeroDec()))

	s.Require().NoError(s.treasuryKeeper.EpochInitialIssuance.Set(s.ctx, types.EpochInitialIssuance{
		Issuance: sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(10000))),
	}))
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), core.MicroArkDenom).
		Return(sdk.NewCoin(core.MicroArkDenom, math.NewInt(9000)))

	// Mint seigniorage
	seigniorageCoins := sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000)))
	s.bankKeeper.EXPECT().
		MintCoins(gomock.Any(), types.ModuleName, seigniorageCoins).
		Return(nil)

	// oracleRewardAmt = 0 → oracle send is skipped

	// All to community pool: 1000 - 0 = 1000
	communityCoins := sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000)))
	s.bankKeeper.EXPECT().
		SendCoinsFromModuleToModule(gomock.Any(), types.ModuleName, "distribution", communityCoins).
		Return(nil)

	s.distrKeeper.EXPECT().GetFeePool(gomock.Any()).
		Return(distrtypes.FeePool{CommunityPool: sdk.DecCoins{}})
	expectedPool := distrtypes.FeePool{
		CommunityPool: sdk.NewDecCoinsFromCoins(communityCoins...),
	}
	s.distrKeeper.EXPECT().SetFeePool(gomock.Any(), expectedPool)

	err := s.treasuryKeeper.SettleSeigniorage(s.ctx)
	s.Require().NoError(err)
}
