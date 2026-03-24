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

	tests := []struct {
		name        string
		msg         *types.MsgUpdateParams
		expectErr   string
		expectErrIs error
	}{
		{
			name: "valid custom params",
			msg: &types.MsgUpdateParams{
				Authority: authority,
				Params: types.Params{
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
				},
			},
		},
		{
			name: "boundary: RateMax equals RateMin",
			msg: func() *types.MsgUpdateParams {
				p := types.DefaultParams()
				p.TaxPolicy.RateMax = p.TaxPolicy.RateMin
				return &types.MsgUpdateParams{Authority: authority, Params: p}
			}(),
		},
		{
			name: "boundary: zero SeigniorageBurdenTarget",
			msg: func() *types.MsgUpdateParams {
				p := types.DefaultParams()
				p.SeigniorageBurdenTarget = math.LegacyZeroDec()
				return &types.MsgUpdateParams{Authority: authority, Params: p}
			}(),
		},
		{
			name: "boundary: WindowLong = WindowShort + 1",
			msg: func() *types.MsgUpdateParams {
				p := types.DefaultParams()
				p.WindowShort = 4
				p.WindowLong = 5
				return &types.MsgUpdateParams{Authority: authority, Params: p}
			}(),
		},
		{
			name: "invalid authority",
			msg: &types.MsgUpdateParams{
				Authority: "invalid_authority",
				Params:    types.DefaultParams(),
			},
			expectErr:   "invalid authority",
			expectErrIs: govtypes.ErrInvalidSigner,
		},
		// TaxPolicy validations
		{
			name: "negative TaxPolicy.RateMin",
			msg: func() *types.MsgUpdateParams {
				p := types.DefaultParams()
				p.TaxPolicy.RateMin = math.LegacyNewDec(-1)
				return &types.MsgUpdateParams{Authority: authority, Params: p}
			}(),
			expectErr: "TaxPolicy.RateMin must be zero or positive",
		},
		{
			name: "TaxPolicy.RateMax less than RateMin",
			msg: func() *types.MsgUpdateParams {
				p := types.DefaultParams()
				p.TaxPolicy.RateMax = math.LegacyNewDecWithPrec(1, 4) // 0.01%
				p.TaxPolicy.RateMin = math.LegacyNewDecWithPrec(5, 4) // 0.05%
				return &types.MsgUpdateParams{Authority: authority, Params: p}
			}(),
			expectErr: "TaxPolicy.RateMax",
		},
		{
			name: "invalid TaxPolicy.Cap",
			msg: func() *types.MsgUpdateParams {
				p := types.DefaultParams()
				p.TaxPolicy.Cap = sdk.Coin{Denom: core.MicroSDRDenom, Amount: math.NewInt(-1)}
				return &types.MsgUpdateParams{Authority: authority, Params: p}
			}(),
			expectErr: "TaxPolicy.Cap is invalid",
		},
		{
			name: "negative TaxPolicy.ChangeRateMax",
			msg: func() *types.MsgUpdateParams {
				p := types.DefaultParams()
				p.TaxPolicy.ChangeRateMax = math.LegacyNewDec(-1)
				return &types.MsgUpdateParams{Authority: authority, Params: p}
			}(),
			expectErr: "TaxPolicy.ChangeRateMax must be positive",
		},
		// RewardPolicy validations
		{
			name: "RewardPolicy.RateMax less than RateMin",
			msg: func() *types.MsgUpdateParams {
				p := types.DefaultParams()
				p.RewardPolicy.RateMax = math.LegacyNewDecWithPrec(1, 2) // 1%
				p.RewardPolicy.RateMin = math.LegacyNewDecWithPrec(5, 2) // 5%
				return &types.MsgUpdateParams{Authority: authority, Params: p}
			}(),
			expectErr: "RewardPolicy.RateMax",
		},
		{
			name: "negative RewardPolicy.RateMin",
			msg: func() *types.MsgUpdateParams {
				p := types.DefaultParams()
				p.RewardPolicy.RateMin = math.LegacyNewDec(-1)
				return &types.MsgUpdateParams{Authority: authority, Params: p}
			}(),
			expectErr: "RewardPolicy.RateMin must be positive",
		},
		{
			name: "negative RewardPolicy.ChangeRateMax",
			msg: func() *types.MsgUpdateParams {
				p := types.DefaultParams()
				p.RewardPolicy.ChangeRateMax = math.LegacyNewDec(-1)
				return &types.MsgUpdateParams{Authority: authority, Params: p}
			}(),
			expectErr: "RewardPolicy.ChangeRateMax must be positive",
		},
		// Scalar validations
		{
			name: "negative SeigniorageBurdenTarget",
			msg: func() *types.MsgUpdateParams {
				p := types.DefaultParams()
				p.SeigniorageBurdenTarget = math.LegacyNewDec(-1)
				return &types.MsgUpdateParams{Authority: authority, Params: p}
			}(),
			expectErr: "SeigniorageBurdenTarget must be positive",
		},
		{
			name: "negative MiningIncrement",
			msg: func() *types.MsgUpdateParams {
				p := types.DefaultParams()
				p.MiningIncrement = math.LegacyNewDec(-1)
				return &types.MsgUpdateParams{Authority: authority, Params: p}
			}(),
			expectErr: "MiningIncrement must be positive",
		},
		// Window validations
		{
			name: "WindowLong equals WindowShort",
			msg: func() *types.MsgUpdateParams {
				p := types.DefaultParams()
				p.WindowLong = 4
				p.WindowShort = 4
				return &types.MsgUpdateParams{Authority: authority, Params: p}
			}(),
			expectErr: "WindowLong must be bigger than WindowShort",
		},
		{
			name: "WindowLong less than WindowShort",
			msg: func() *types.MsgUpdateParams {
				p := types.DefaultParams()
				p.WindowLong = 3
				p.WindowShort = 4
				return &types.MsgUpdateParams{Authority: authority, Params: p}
			}(),
			expectErr: "WindowLong must be bigger than WindowShort",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			_, err := s.msgServer.UpdateParams(s.ctx, tc.msg)
			if tc.expectErr != "" {
				s.Require().Error(err)
				s.Require().ErrorContains(err, tc.expectErr)
				if tc.expectErrIs != nil {
					s.Require().ErrorIs(err, tc.expectErrIs)
				}
			} else {
				s.Require().NoError(err)
				params, err := s.keeper.Params.Get(s.ctx)
				s.Require().NoError(err)
				s.Require().Equal(tc.msg.Params, params)
			}
		})
	}
}
