package keeper_test

import (
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	core "noah/types"
	"noah/x/oracle/types"
)

func (s *KeeperTestSuite) TestPrevote() {
	tests := []struct {
		name      string
		setup     func(feeder sdk.AccAddress, validator sdk.ValAddress)
		msg       func(feeder sdk.AccAddress, validator sdk.ValAddress) *types.MsgPrevote
		expectErr error
		errText   string
	}{
		{
			name: "stores prevote on success",
			setup: func(_ sdk.AccAddress, validator sdk.ValAddress) {
				s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(7)
				s.stakingKeeper.EXPECT().Validator(s.ctx, validator).Return(stakingtypes.Validator{
					OperatorAddress: validator.String(),
					Status:          stakingtypes.Bonded,
					Tokens:          math.NewInt(10),
				})
			},
			msg: func(feeder sdk.AccAddress, validator sdk.ValAddress) *types.MsgPrevote {
				hash := types.GetVoteHash("salt", "1.0"+core.MicroUSDDenom, validator)
				return &types.MsgPrevote{
					Hash:      hash.String(),
					Feeder:    feeder.String(),
					Validator: validator.String(),
				}
			},
		},
		{
			name:  "rejects wrong feeder",
			setup: func(_ sdk.AccAddress, _ sdk.ValAddress) {},
			msg: func(_ sdk.AccAddress, validator sdk.ValAddress) *types.MsgPrevote {
				hash := types.GetVoteHash("salt", "1.0"+core.MicroUSDDenom, validator)
				return &types.MsgPrevote{
					Hash:      hash.String(),
					Feeder:    accAddr1.String(),
					Validator: validator.String(),
				}
			},
			expectErr: types.ErrNoVotingPermission,
		},
		{
			name: "rejects invalid hash length",
			setup: func(_ sdk.AccAddress, validator sdk.ValAddress) {
				s.stakingKeeper.EXPECT().Validator(s.ctx, validator).Return(stakingtypes.Validator{
					OperatorAddress: validator.String(),
					Status:          stakingtypes.Bonded,
					Tokens:          math.NewInt(10),
				})
			},
			msg: func(feeder sdk.AccAddress, validator sdk.ValAddress) *types.MsgPrevote {
				return &types.MsgPrevote{
					Hash:      "abcd",
					Feeder:    feeder.String(),
					Validator: validator.String(),
				}
			},
			expectErr: types.ErrInvalidHashLength,
		},
		{
			name:  "rejects invalid validator address",
			setup: func(_ sdk.AccAddress, _ sdk.ValAddress) {},
			msg: func(feeder sdk.AccAddress, validator sdk.ValAddress) *types.MsgPrevote {
				hash := types.GetVoteHash("salt", "1.0"+core.MicroUSDDenom, validator)
				return &types.MsgPrevote{
					Hash:      hash.String(),
					Feeder:    feeder.String(),
					Validator: "invalid",
				}
			},
			errText: "decoding bech32 failed",
		},
		{
			name:  "rejects invalid feeder address",
			setup: func(_ sdk.AccAddress, _ sdk.ValAddress) {},
			msg: func(_ sdk.AccAddress, validator sdk.ValAddress) *types.MsgPrevote {
				hash := types.GetVoteHash("salt", "1.0"+core.MicroUSDDenom, validator)
				return &types.MsgPrevote{
					Hash:      hash.String(),
					Feeder:    "invalid",
					Validator: validator.String(),
				}
			},
			errText: "decoding bech32 failed",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			validator := valAddr1
			feeder := sdk.AccAddress(validator)
			tc.setup(feeder, validator)

			_, err := s.msgServer.Prevote(s.ctx, tc.msg(feeder, validator))
			if tc.expectErr != nil || tc.errText != "" {
				s.Require().Error(err)
				if tc.expectErr != nil {
					s.Require().True(errorsmod.IsOf(err, tc.expectErr))
				}
				if tc.errText != "" {
					s.Require().ErrorContains(err, tc.errText)
				}
				return
			}

			s.Require().NoError(err)
			prevote, err := s.keeper.Prevote.Get(s.ctx, validator)
			s.Require().NoError(err)
			s.Require().Equal(validator.String(), prevote.Voter)
			s.Require().Equal(uint64(7), prevote.SubmitBlock)

			events := sdk.UnwrapSDKContext(s.ctx).EventManager().Events()
			s.Require().Len(events, 2)
			s.Require().Equal(types.EventTypePrevote, events[0].Type)
			s.Require().Equal(types.AttributeKeyVoter, events[0].Attributes[0].Key)
			s.Require().Equal(validator.String(), events[0].Attributes[0].Value)
			s.Require().Equal(sdk.EventTypeMessage, events[1].Type)
			s.Require().Equal(sdk.AttributeKeyModule, events[1].Attributes[0].Key)
			s.Require().Equal(types.AttributeValueCategory, events[1].Attributes[0].Value)
			s.Require().Equal(sdk.AttributeKeySender, events[1].Attributes[1].Key)
			s.Require().Equal(feeder.String(), events[1].Attributes[1].Value)
		})
	}
}

func (s *KeeperTestSuite) TestVote() {
	tests := []struct {
		name      string
		setup     func(feeder sdk.AccAddress, validator sdk.ValAddress, salt, rates string)
		msg       func(feeder sdk.AccAddress, validator sdk.ValAddress, salt, rates string) *types.MsgVote
		expectErr error
		errText   string
	}{
		{
			name: "reveals prevote successfully",
			setup: func(_ sdk.AccAddress, validator sdk.ValAddress, salt, rates string) {
				s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(20)
				params, err := s.keeper.Params.Get(s.ctx)
				s.Require().NoError(err)
				params.VotePeriod = 10
				s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
				s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, core.MicroUSDDenom, math.LegacyNewDecWithPrec(25, 4)))
				s.Require().NoError(s.keeper.Prevote.Set(
					s.ctx,
					validator,
					types.NewPrevote(types.GetVoteHash(salt, rates, validator), validator, 10),
				))
				s.stakingKeeper.EXPECT().Validator(s.ctx, validator).Return(stakingtypes.Validator{
					OperatorAddress: validator.String(),
					Status:          stakingtypes.Bonded,
					Tokens:          math.NewInt(10),
				})
			},
			msg: func(feeder sdk.AccAddress, validator sdk.ValAddress, salt, rates string) *types.MsgVote {
				return &types.MsgVote{
					Salt:          salt,
					ExchangeRates: rates,
					Feeder:        feeder.String(),
					Validator:     validator.String(),
				}
			},
		},
		{
			name: "requires at least one exchange rate",
			setup: func(_ sdk.AccAddress, validator sdk.ValAddress, _, _ string) {
				s.stakingKeeper.EXPECT().Validator(s.ctx, validator).Return(stakingtypes.Validator{
					OperatorAddress: validator.String(),
					Status:          stakingtypes.Bonded,
					Tokens:          math.NewInt(10),
				})
			},
			msg: func(feeder sdk.AccAddress, validator sdk.ValAddress, salt, _ string) *types.MsgVote {
				return &types.MsgVote{
					Salt:          salt,
					ExchangeRates: "",
					Feeder:        feeder.String(),
					Validator:     validator.String(),
				}
			},
			expectErr: errortypes.ErrUnknownRequest,
		},
		{
			name:  "rejects invalid validator address",
			setup: func(_ sdk.AccAddress, _ sdk.ValAddress, _, _ string) {},
			msg: func(feeder sdk.AccAddress, _ sdk.ValAddress, salt, rates string) *types.MsgVote {
				return &types.MsgVote{
					Salt:          salt,
					ExchangeRates: rates,
					Feeder:        feeder.String(),
					Validator:     "invalid",
				}
			},
			errText: "decoding bech32 failed",
		},
		{
			name:  "rejects invalid feeder address",
			setup: func(_ sdk.AccAddress, _ sdk.ValAddress, _, _ string) {},
			msg: func(_ sdk.AccAddress, validator sdk.ValAddress, salt, rates string) *types.MsgVote {
				return &types.MsgVote{
					Salt:          salt,
					ExchangeRates: rates,
					Feeder:        "invalid",
					Validator:     validator.String(),
				}
			},
			errText: "decoding bech32 failed",
		},
		{
			name: "rejects empty salt",
			setup: func(_ sdk.AccAddress, validator sdk.ValAddress, _, _ string) {
				s.stakingKeeper.EXPECT().Validator(s.ctx, validator).Return(stakingtypes.Validator{
					OperatorAddress: validator.String(),
					Status:          stakingtypes.Bonded,
					Tokens:          math.NewInt(10),
				})
			},
			msg: func(feeder sdk.AccAddress, validator sdk.ValAddress, _, rates string) *types.MsgVote {
				return &types.MsgVote{
					Salt:          "",
					ExchangeRates: rates,
					Feeder:        feeder.String(),
					Validator:     validator.String(),
				}
			},
			expectErr: types.ErrInvalidSaltLength,
		},
		{
			name: "rejects salt too long",
			setup: func(_ sdk.AccAddress, validator sdk.ValAddress, _, _ string) {
				s.stakingKeeper.EXPECT().Validator(s.ctx, validator).Return(stakingtypes.Validator{
					OperatorAddress: validator.String(),
					Status:          stakingtypes.Bonded,
					Tokens:          math.NewInt(10),
				})
			},
			msg: func(feeder sdk.AccAddress, validator sdk.ValAddress, _, rates string) *types.MsgVote {
				return &types.MsgVote{
					Salt:          "abcde",
					ExchangeRates: rates,
					Feeder:        feeder.String(),
					Validator:     validator.String(),
				}
			},
			expectErr: types.ErrInvalidSaltLength,
		},
		{
			name: "rejects reveal period mismatch",
			setup: func(_ sdk.AccAddress, validator sdk.ValAddress, salt, rates string) {
				s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(12)
				params, err := s.keeper.Params.Get(s.ctx)
				s.Require().NoError(err)
				params.VotePeriod = 10
				s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
				s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, core.MicroUSDDenom, math.LegacyNewDecWithPrec(25, 4)))
				s.Require().NoError(s.keeper.Prevote.Set(
					s.ctx,
					validator,
					types.NewPrevote(types.GetVoteHash(salt, rates, validator), validator, 10),
				))
				s.stakingKeeper.EXPECT().Validator(s.ctx, validator).Return(stakingtypes.Validator{
					OperatorAddress: validator.String(),
					Status:          stakingtypes.Bonded,
					Tokens:          math.NewInt(10),
				})
			},
			msg: func(feeder sdk.AccAddress, validator sdk.ValAddress, salt, rates string) *types.MsgVote {
				return &types.MsgVote{
					Salt:          salt,
					ExchangeRates: rates,
					Feeder:        feeder.String(),
					Validator:     validator.String(),
				}
			},
			expectErr: types.ErrRevealPeriodMissMatch,
		},
		{
			name: "rejects missing prevote",
			setup: func(_ sdk.AccAddress, validator sdk.ValAddress, _, _ string) {
				s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(20)
				params, err := s.keeper.Params.Get(s.ctx)
				s.Require().NoError(err)
				params.VotePeriod = 10
				s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
				s.stakingKeeper.EXPECT().Validator(s.ctx, validator).Return(stakingtypes.Validator{
					OperatorAddress: validator.String(),
					Status:          stakingtypes.Bonded,
					Tokens:          math.NewInt(10),
				})
			},
			msg: func(feeder sdk.AccAddress, validator sdk.ValAddress, salt, rates string) *types.MsgVote {
				return &types.MsgVote{
					Salt:          salt,
					ExchangeRates: rates,
					Feeder:        feeder.String(),
					Validator:     validator.String(),
				}
			},
			errText: "not found",
		},
		{
			name: "rejects unknown denom",
			setup: func(_ sdk.AccAddress, validator sdk.ValAddress, salt, rates string) {
				s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(20)
				params, err := s.keeper.Params.Get(s.ctx)
				s.Require().NoError(err)
				params.VotePeriod = 10
				s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
				s.Require().NoError(s.keeper.Prevote.Set(
					s.ctx,
					validator,
					types.NewPrevote(types.GetVoteHash(salt, rates, validator), validator, 10),
				))
				s.stakingKeeper.EXPECT().Validator(s.ctx, validator).Return(stakingtypes.Validator{
					OperatorAddress: validator.String(),
					Status:          stakingtypes.Bonded,
					Tokens:          math.NewInt(10),
				})
			},
			msg: func(feeder sdk.AccAddress, validator sdk.ValAddress, salt, _ string) *types.MsgVote {
				return &types.MsgVote{
					Salt:          salt,
					ExchangeRates: "1.0ufoo",
					Feeder:        feeder.String(),
					Validator:     validator.String(),
				}
			},
			expectErr: types.ErrUnknownDenom,
		},
		{
			name: "rejects verification mismatch",
			setup: func(_ sdk.AccAddress, validator sdk.ValAddress, _, rates string) {
				s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(20)
				params, err := s.keeper.Params.Get(s.ctx)
				s.Require().NoError(err)
				params.VotePeriod = 10
				s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
				s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, core.MicroUSDDenom, math.LegacyNewDecWithPrec(25, 4)))
				s.Require().NoError(s.keeper.Prevote.Set(
					s.ctx,
					validator,
					types.NewPrevote(types.GetVoteHash("diff", rates, validator), validator, 10),
				))
				s.stakingKeeper.EXPECT().Validator(s.ctx, validator).Return(stakingtypes.Validator{
					OperatorAddress: validator.String(),
					Status:          stakingtypes.Bonded,
					Tokens:          math.NewInt(10),
				})
			},
			msg: func(feeder sdk.AccAddress, validator sdk.ValAddress, salt, rates string) *types.MsgVote {
				return &types.MsgVote{
					Salt:          salt,
					ExchangeRates: rates,
					Feeder:        feeder.String(),
					Validator:     validator.String(),
				}
			},
			expectErr: types.ErrVerificationFailed,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			validator := valAddr1
			feeder := sdk.AccAddress(validator)
			salt := "salt"
			rates := "1.0" + core.MicroUSDDenom
			tc.setup(feeder, validator, salt, rates)

			_, err := s.msgServer.Vote(s.ctx, tc.msg(feeder, validator, salt, rates))
			if tc.expectErr != nil || tc.errText != "" {
				s.Require().Error(err)
				if tc.expectErr != nil {
					s.Require().True(errorsmod.IsOf(err, tc.expectErr), err.Error())
				}
				if tc.errText != "" {
					s.Require().ErrorContains(err, tc.errText)
				}
				return
			}

			s.Require().NoError(err)
			vote, err := s.keeper.Vote.Get(s.ctx, validator)
			s.Require().NoError(err)
			s.Require().Equal(validator.String(), vote.Voter)

			_, err = s.keeper.Prevote.Get(s.ctx, validator)
			s.Require().Error(err)

			events := sdk.UnwrapSDKContext(s.ctx).EventManager().Events()
			s.Require().Len(events, 2)
			s.Require().Equal(types.EventTypeVote, events[0].Type)
			s.Require().Equal(types.AttributeKeyVoter, events[0].Attributes[0].Key)
			s.Require().Equal(validator.String(), events[0].Attributes[0].Value)
			s.Require().Equal(types.AttributeKeyExchangeRates, events[0].Attributes[1].Key)
			s.Require().Equal(rates, events[0].Attributes[1].Value)
			s.Require().Equal(sdk.EventTypeMessage, events[1].Type)
			s.Require().Equal(sdk.AttributeKeyModule, events[1].Attributes[0].Key)
			s.Require().Equal(types.AttributeValueCategory, events[1].Attributes[0].Value)
			s.Require().Equal(sdk.AttributeKeySender, events[1].Attributes[1].Key)
			s.Require().Equal(feeder.String(), events[1].Attributes[1].Value)
		})
	}
}

func (s *KeeperTestSuite) TestDelegateFeedConsent() {
	tests := []struct {
		name      string
		setup     func()
		msg       *types.MsgDelegateFeedConsent
		expectErr error
		errText   string
	}{
		{
			name: "stores delegation for validator",
			setup: func() {
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(stakingtypes.Validator{
					OperatorAddress: valAddr1.String(),
					Status:          stakingtypes.Bonded,
					Tokens:          math.NewInt(10),
				})
			},
			msg: &types.MsgDelegateFeedConsent{
				Validator: valAddr1.String(),
				Feeder:    accAddr1.String(),
			},
		},
		{
			name: "returns error when validator missing",
			setup: func() {
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(nil)
			},
			msg: &types.MsgDelegateFeedConsent{
				Validator: valAddr1.String(),
				Feeder:    accAddr1.String(),
			},
			expectErr: stakingtypes.ErrNoValidatorFound,
		},
		{
			name:  "rejects invalid operator address",
			setup: func() {},
			msg: &types.MsgDelegateFeedConsent{
				Validator: "invalid",
				Feeder:    accAddr1.String(),
			},
			errText: "decoding bech32 failed",
		},
		{
			name:  "rejects invalid delegate address",
			setup: func() {},
			msg: &types.MsgDelegateFeedConsent{
				Validator: valAddr1.String(),
				Feeder:    "invalid",
			},
			errText: "decoding bech32 failed",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			tc.setup()

			_, err := s.msgServer.DelegateFeedConsent(s.ctx, tc.msg)
			if tc.expectErr != nil || tc.errText != "" {
				s.Require().Error(err)
				if tc.expectErr != nil {
					s.Require().True(errorsmod.IsOf(err, tc.expectErr))
				}
				if tc.errText != "" {
					s.Require().ErrorContains(err, tc.errText)
				}
				return
			}

			s.Require().NoError(err)
			delegate, err := s.keeper.FeederDelegation.Get(s.ctx, valAddr1)
			s.Require().NoError(err)
			s.Require().Equal(accAddr1.String(), delegate.String())

			events := sdk.UnwrapSDKContext(s.ctx).EventManager().Events()
			s.Require().Len(events, 2)
			s.Require().Equal(types.EventTypeFeedDelegate, events[0].Type)
			s.Require().Equal(types.AttributeKeyFeeder, events[0].Attributes[0].Key)
			s.Require().Equal(tc.msg.Feeder, events[0].Attributes[0].Value)
			s.Require().Equal(sdk.EventTypeMessage, events[1].Type)
			s.Require().Equal(sdk.AttributeKeyModule, events[1].Attributes[0].Key)
			s.Require().Equal(types.AttributeValueCategory, events[1].Attributes[0].Value)
			s.Require().Equal(sdk.AttributeKeySender, events[1].Attributes[1].Key)
			s.Require().Equal(tc.msg.Validator, events[1].Attributes[1].Value)
		})
	}
}

func (s *KeeperTestSuite) TestUpdateParams() {
	tests := []struct {
		name      string
		msg       *types.MsgUpdateParams
		expectErr error
		errText   string
	}{
		{
			name: "updates params",
			msg: &types.MsgUpdateParams{
				Authority: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
				Params: func() types.Params {
					params := types.DefaultParams()
					params.VotePeriod = 42
					return params
				}(),
			},
		},
		{
			name: "rejects invalid authority",
			msg: &types.MsgUpdateParams{
				Authority: "cosmos1invalidauthority0000000000000000000000",
				Params:    types.DefaultParams(),
			},
			expectErr: govtypes.ErrInvalidSigner,
		},
		{
			name: "rejects invalid params",
			msg: &types.MsgUpdateParams{
				Authority: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
				Params: func() types.Params {
					params := types.DefaultParams()
					params.VotePeriod = 0
					return params
				}(),
			},
			errText: "VotePeriod must be > 0",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			_, err := s.msgServer.UpdateParams(s.ctx, tc.msg)
			if tc.expectErr != nil || tc.errText != "" {
				s.Require().Error(err)
				if tc.expectErr != nil {
					s.Require().True(errorsmod.IsOf(err, tc.expectErr))
				}
				if tc.errText != "" {
					s.Require().ErrorContains(err, tc.errText)
				}
				return
			}

			s.Require().NoError(err)
			params, err := s.keeper.Params.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().Equal(uint64(42), params.VotePeriod)
		})
	}
}
