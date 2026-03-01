package keeper_test

import (
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	core "noah/types"
	"noah/x/treasury/types"
)

func (s *KeeperTestSuite) TestMsgUpdateParams() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	newParams := types.Params{
		TaxPolicy: types.PolicyConstraints{
			RateMin:       math.LegacyNewDecWithPrec(1, 3),
			RateMax:       math.LegacyNewDecWithPrec(2, 2),
			Cap:           sdk.NewCoin(core.MicroSDRDenom, math.NewInt(2000000)),
			ChangeRateMax: math.LegacyNewDecWithPrec(5, 4),
		},
		RewardPolicy: types.PolicyConstraints{
			RateMin:       math.LegacyNewDecWithPrec(10, 2),
			RateMax:       math.LegacyNewDecWithPrec(60, 2),
			Cap:           sdk.NewCoin("unused", math.ZeroInt()),
			ChangeRateMax: math.LegacyNewDecWithPrec(5, 2),
		},
		SeigniorageBurdenTarget: math.LegacyNewDecWithPrec(70, 2),
		MiningIncrement:         math.LegacyNewDecWithPrec(110, 2),
		WindowShort:             5,
		WindowLong:              53,
		WindowProbation:         14,
	}

	msg := &types.MsgUpdateParams{
		Authority: authority,
		Params:    newParams,
	}

	_, err := s.msgServer.UpdateParams(s.ctx, msg)
	s.Require().NoError(err)

	// Verify params were updated
	params, err := s.treasuryKeeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(newParams.TaxPolicy.RateMax.Equal(params.TaxPolicy.RateMax))
	s.Require().True(newParams.RewardPolicy.RateMax.Equal(params.RewardPolicy.RateMax))
	s.Require().True(newParams.SeigniorageBurdenTarget.Equal(params.SeigniorageBurdenTarget))
	s.Require().True(newParams.MiningIncrement.Equal(params.MiningIncrement))
	s.Require().Equal(newParams.WindowShort, params.WindowShort)
	s.Require().Equal(newParams.WindowLong, params.WindowLong)
	s.Require().Equal(newParams.WindowProbation, params.WindowProbation)
}

func (s *KeeperTestSuite) TestMsgUpdateParams_InvalidAuthority() {
	msg := &types.MsgUpdateParams{
		Authority: "invalid_authority",
		Params:    types.DefaultParams(),
	}

	_, err := s.msgServer.UpdateParams(s.ctx, msg)
	s.Require().Error(err)
	s.Require().ErrorIs(err, govtypes.ErrInvalidSigner)
	s.Require().ErrorContains(err, "invalid authority")
}

func (s *KeeperTestSuite) TestMsgUpdateParams_InvalidParams_NegativeTaxRateMin() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	params := types.DefaultParams()
	params.TaxPolicy.RateMin = math.LegacyNewDec(-1)

	msg := &types.MsgUpdateParams{
		Authority: authority,
		Params:    params,
	}

	_, err := s.msgServer.UpdateParams(s.ctx, msg)
	s.Require().Error(err)
	s.Require().ErrorContains(err, "TaxPolicy.RateMin must be zero or positive")
}

func (s *KeeperTestSuite) TestMsgUpdateParams_InvalidParams_TaxRateMaxLTMin() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	params := types.DefaultParams()
	params.TaxPolicy.RateMax = math.LegacyNewDecWithPrec(1, 4) // 0.01% < default RateMin 0.05%
	params.TaxPolicy.RateMin = math.LegacyNewDecWithPrec(5, 4) // 0.05%

	msg := &types.MsgUpdateParams{
		Authority: authority,
		Params:    params,
	}

	_, err := s.msgServer.UpdateParams(s.ctx, msg)
	s.Require().Error(err)
	s.Require().ErrorContains(err, "TaxPolicy.RateMax")
	s.Require().ErrorContains(err, "must be greater than")
}

func (s *KeeperTestSuite) TestMsgUpdateParams_InvalidParams_WindowLongLTEShort() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	params := types.DefaultParams()
	params.WindowLong = 4
	params.WindowShort = 4

	msg := &types.MsgUpdateParams{
		Authority: authority,
		Params:    params,
	}

	_, err := s.msgServer.UpdateParams(s.ctx, msg)
	s.Require().Error(err)
	s.Require().ErrorContains(err, "WindowLong must be bigger than WindowShort")
}

func (s *KeeperTestSuite) TestMsgUpdateParams_InvalidParams_NegativeSeigniorageBurdenTarget() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	params := types.DefaultParams()
	params.SeigniorageBurdenTarget = math.LegacyNewDec(-1)

	msg := &types.MsgUpdateParams{
		Authority: authority,
		Params:    params,
	}

	_, err := s.msgServer.UpdateParams(s.ctx, msg)
	s.Require().Error(err)
	s.Require().ErrorContains(err, "SeigniorageBurdenTarget must be positive")
}

func (s *KeeperTestSuite) TestMsgUpdateParams_InvalidParams_NegativeMiningIncrement() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	params := types.DefaultParams()
	params.MiningIncrement = math.LegacyNewDec(-1)

	msg := &types.MsgUpdateParams{
		Authority: authority,
		Params:    params,
	}

	_, err := s.msgServer.UpdateParams(s.ctx, msg)
	s.Require().Error(err)
	s.Require().ErrorContains(err, "MiningIncrement must be positive")
}
