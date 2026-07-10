package keeper_test

import (
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	chain "ark/pkg/chain"
	"ark/x/treasury/types"
)

func (s *KeeperTestSuite) TestMsgUpdateParams() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	consensusAuthority := authtypes.NewModuleAddress("consensus").String()

	tests := []struct {
		name        string
		setup       func()
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
						Cap:           sdk.NewCoin(chain.MicroSDRDenom, math.NewInt(2000000)),
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
			name: "consensus params authority overrides keeper authority",
			setup: func() {
				s.ctx = sdk.UnwrapSDKContext(s.ctx).WithConsensusParams(cmtproto.ConsensusParams{
					Authority: &cmtproto.AuthorityParams{Authority: consensusAuthority},
				})
			},
			msg: func() *types.MsgUpdateParams {
				p := types.DefaultParams()
				p.WindowShort = 6
				return &types.MsgUpdateParams{Authority: consensusAuthority, Params: p}
			}(),
		},
		{
			name: "invalid authority",
			msg: &types.MsgUpdateParams{
				Authority: "invalid_authority",
				Params:    types.DefaultParams(),
			},
			expectErr:   "invalid authority",
			expectErrIs: errortypes.ErrUnauthorized,
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
			ctx := s.ctx
			defer func() { s.ctx = ctx }()
			if tc.setup != nil {
				tc.setup()
			}

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
