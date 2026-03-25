package keeper_test

import (
	"cosmossdk.io/math"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	core "noah/types"
	"noah/x/oracle/types"
)

func (s *KeeperTestSuite) TestAggregateExchangeRatePrevote() {
	tests := []struct {
		name      string
		setup     func(feeder sdk.AccAddress, validator sdk.ValAddress)
		msg       func(feeder sdk.AccAddress, validator sdk.ValAddress) *types.MsgAggregateExchangeRatePrevote
		expectErr error
		errText   string
	}{
		{
			name: "stores prevote on success",
			setup: func(_ sdk.AccAddress, validator sdk.ValAddress) {
				s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(7)
				s.stakingKeeper.EXPECT().Validator(s.ctx, validator).Return(makeValidator(validator, stakingtypes.Bonded, 10))
			},
			msg: func(feeder sdk.AccAddress, validator sdk.ValAddress) *types.MsgAggregateExchangeRatePrevote {
				hash := types.GetAggregateVoteHash("salt", "1.0"+core.MicroUSDDenom, validator)
				return &types.MsgAggregateExchangeRatePrevote{
					Hash:      hash.String(),
					Feeder:    feeder.String(),
					Validator: validator.String(),
				}
			},
		},
		{
			name:  "rejects wrong feeder",
			setup: func(_ sdk.AccAddress, _ sdk.ValAddress) {},
			msg: func(_ sdk.AccAddress, validator sdk.ValAddress) *types.MsgAggregateExchangeRatePrevote {
				hash := types.GetAggregateVoteHash("salt", "1.0"+core.MicroUSDDenom, validator)
				return &types.MsgAggregateExchangeRatePrevote{
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
				s.stakingKeeper.EXPECT().Validator(s.ctx, validator).Return(makeValidator(validator, stakingtypes.Bonded, 10))
			},
			msg: func(feeder sdk.AccAddress, validator sdk.ValAddress) *types.MsgAggregateExchangeRatePrevote {
				return &types.MsgAggregateExchangeRatePrevote{
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
			msg: func(feeder sdk.AccAddress, validator sdk.ValAddress) *types.MsgAggregateExchangeRatePrevote {
				hash := types.GetAggregateVoteHash("salt", "1.0"+core.MicroUSDDenom, validator)
				return &types.MsgAggregateExchangeRatePrevote{
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
			msg: func(_ sdk.AccAddress, validator sdk.ValAddress) *types.MsgAggregateExchangeRatePrevote {
				hash := types.GetAggregateVoteHash("salt", "1.0"+core.MicroUSDDenom, validator)
				return &types.MsgAggregateExchangeRatePrevote{
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

			_, err := s.msgServer.AggregateExchangeRatePrevote(s.ctx, tc.msg(feeder, validator))
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
			prevote, err := s.keeper.AggregateExchangeRatePrevote.Get(s.ctx, validator)
			s.Require().NoError(err)
			s.Require().Equal(validator.String(), prevote.Voter)
			s.Require().Equal(uint64(7), prevote.SubmitBlock)
		})
	}
}

func (s *KeeperTestSuite) TestAggregateExchangeRateVote() {
	tests := []struct {
		name      string
		setup     func(feeder sdk.AccAddress, validator sdk.ValAddress, salt, rates string)
		msg       func(feeder sdk.AccAddress, validator sdk.ValAddress, salt, rates string) *types.MsgAggregateExchangeRateVote
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
				s.Require().NoError(s.keeper.AggregateExchangeRatePrevote.Set(
					s.ctx,
					validator,
					types.NewAggregateExchangeRatePrevote(types.GetAggregateVoteHash(salt, rates, validator), validator, 10),
				))
				s.stakingKeeper.EXPECT().Validator(s.ctx, validator).Return(makeValidator(validator, stakingtypes.Bonded, 10))
			},
			msg: func(feeder sdk.AccAddress, validator sdk.ValAddress, salt, rates string) *types.MsgAggregateExchangeRateVote {
				return &types.MsgAggregateExchangeRateVote{
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
				s.stakingKeeper.EXPECT().Validator(s.ctx, validator).Return(makeValidator(validator, stakingtypes.Bonded, 10))
			},
			msg: func(feeder sdk.AccAddress, validator sdk.ValAddress, salt, _ string) *types.MsgAggregateExchangeRateVote {
				return &types.MsgAggregateExchangeRateVote{
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
			msg: func(feeder sdk.AccAddress, _ sdk.ValAddress, salt, rates string) *types.MsgAggregateExchangeRateVote {
				return &types.MsgAggregateExchangeRateVote{
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
			msg: func(_ sdk.AccAddress, validator sdk.ValAddress, salt, rates string) *types.MsgAggregateExchangeRateVote {
				return &types.MsgAggregateExchangeRateVote{
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
				s.stakingKeeper.EXPECT().Validator(s.ctx, validator).Return(makeValidator(validator, stakingtypes.Bonded, 10))
			},
			msg: func(feeder sdk.AccAddress, validator sdk.ValAddress, _, rates string) *types.MsgAggregateExchangeRateVote {
				return &types.MsgAggregateExchangeRateVote{
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
				s.stakingKeeper.EXPECT().Validator(s.ctx, validator).Return(makeValidator(validator, stakingtypes.Bonded, 10))
			},
			msg: func(feeder sdk.AccAddress, validator sdk.ValAddress, _, rates string) *types.MsgAggregateExchangeRateVote {
				return &types.MsgAggregateExchangeRateVote{
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
				s.Require().NoError(s.keeper.AggregateExchangeRatePrevote.Set(
					s.ctx,
					validator,
					types.NewAggregateExchangeRatePrevote(types.GetAggregateVoteHash(salt, rates, validator), validator, 10),
				))
				s.stakingKeeper.EXPECT().Validator(s.ctx, validator).Return(makeValidator(validator, stakingtypes.Bonded, 10))
			},
			msg: func(feeder sdk.AccAddress, validator sdk.ValAddress, salt, rates string) *types.MsgAggregateExchangeRateVote {
				return &types.MsgAggregateExchangeRateVote{
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
				s.stakingKeeper.EXPECT().Validator(s.ctx, validator).Return(makeValidator(validator, stakingtypes.Bonded, 10))
			},
			msg: func(feeder sdk.AccAddress, validator sdk.ValAddress, salt, rates string) *types.MsgAggregateExchangeRateVote {
				return &types.MsgAggregateExchangeRateVote{
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
				s.Require().NoError(s.keeper.AggregateExchangeRatePrevote.Set(
					s.ctx,
					validator,
					types.NewAggregateExchangeRatePrevote(types.GetAggregateVoteHash(salt, rates, validator), validator, 10),
				))
				s.stakingKeeper.EXPECT().Validator(s.ctx, validator).Return(makeValidator(validator, stakingtypes.Bonded, 10))
			},
			msg: func(feeder sdk.AccAddress, validator sdk.ValAddress, salt, _ string) *types.MsgAggregateExchangeRateVote {
				return &types.MsgAggregateExchangeRateVote{
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
				s.Require().NoError(s.keeper.AggregateExchangeRatePrevote.Set(
					s.ctx,
					validator,
					types.NewAggregateExchangeRatePrevote(types.GetAggregateVoteHash("diff", rates, validator), validator, 10),
				))
				s.stakingKeeper.EXPECT().Validator(s.ctx, validator).Return(makeValidator(validator, stakingtypes.Bonded, 10))
			},
			msg: func(feeder sdk.AccAddress, validator sdk.ValAddress, salt, rates string) *types.MsgAggregateExchangeRateVote {
				return &types.MsgAggregateExchangeRateVote{
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

			_, err := s.msgServer.AggregateExchangeRateVote(s.ctx, tc.msg(feeder, validator, salt, rates))
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
			vote, err := s.keeper.AggregateExchangeRateVote.Get(s.ctx, validator)
			s.Require().NoError(err)
			s.Require().Equal(validator.String(), vote.Voter)

			_, err = s.keeper.AggregateExchangeRatePrevote.Get(s.ctx, validator)
			s.Require().Error(err)
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
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(makeValidator(valAddr1, stakingtypes.Bonded, 10))
			},
			msg: &types.MsgDelegateFeedConsent{
				Operator: valAddr1.String(),
				Delegate: accAddr1.String(),
			},
		},
		{
			name: "returns error when validator missing",
			setup: func() {
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(nil)
			},
			msg: &types.MsgDelegateFeedConsent{
				Operator: valAddr1.String(),
				Delegate: accAddr1.String(),
			},
			expectErr: stakingtypes.ErrNoValidatorFound,
		},
		{
			name:  "rejects invalid operator address",
			setup: func() {},
			msg: &types.MsgDelegateFeedConsent{
				Operator: "invalid",
				Delegate: accAddr1.String(),
			},
			errText: "decoding bech32 failed",
		},
		{
			name:  "rejects invalid delegate address",
			setup: func() {},
			msg: &types.MsgDelegateFeedConsent{
				Operator: valAddr1.String(),
				Delegate: "invalid",
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
