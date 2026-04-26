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
					BurnWeight:              math.LegacyNewDecWithPrec(20, 2),
					MiningIncrement:         math.LegacyNewDecWithPrec(110, 2),
					WindowShort:             5,
					WindowLong:              53,
					WindowProbation:         14,
				},
			},
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
		{
			name: "negative TaxPolicy.RateMin ensures params are validated",
			msg: func() *types.MsgUpdateParams {
				p := types.DefaultParams()
				p.TaxPolicy.RateMin = math.LegacyNewDec(-1)
				return &types.MsgUpdateParams{Authority: authority, Params: p}
			}(),
			expectErr: "TaxPolicy.RateMin must be zero or positive",
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
