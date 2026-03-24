package keeper_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	codectestutil "github.com/cosmos/cosmos-sdk/codec/testutil"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/std"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	core "noah/types"
	"noah/x/oracle/keeper"
	"noah/x/oracle/testutil"
	"noah/x/oracle/types"
)

type KeeperTestSuite struct {
	suite.Suite

	ctx           context.Context
	keeper        *keeper.Keeper
	msgServer     types.MsgServer
	queryClient   types.QueryClient
	accountKeeper *testutil.MockAccountKeeper
	bankKeeper    *testutil.MockBankKeeper
	distrKeeper   *testutil.MockDistributionKeeper
	stakingKeeper *testutil.MockStakingKeeper
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}

func (s *KeeperTestSuite) SetupTest() {
	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	types.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)

	key := storetypes.NewKVStoreKey(types.StoreKey)
	storeService := runtime.NewKVStoreService(key)
	testCtx := sdktestutil.DefaultContextWithDB(s.T(), key, storetypes.NewTransientStoreKey("transient_test"))
	s.ctx = testCtx.Ctx

	ctrl := gomock.NewController(s.T())

	s.accountKeeper = testutil.NewMockAccountKeeper(ctrl)
	s.bankKeeper = testutil.NewMockBankKeeper(ctrl)
	s.distrKeeper = testutil.NewMockDistributionKeeper(ctrl)
	s.stakingKeeper = testutil.NewMockStakingKeeper(ctrl)

	// Required by NewKeeper's panic guard
	s.accountKeeper.EXPECT().GetModuleAddress(types.ModuleName).Return(sdk.AccAddress{1})

	s.keeper = keeper.NewKeeper(
		cdc,
		storeService,
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		"distribution",
		s.accountKeeper,
		s.bankKeeper,
		s.distrKeeper,
		s.stakingKeeper,
	)

	// Set default state
	s.Require().NoError(s.keeper.Params.Set(s.ctx, types.DefaultParams()))

	// Wire gRPC query client
	queryHelper := baseapp.NewQueryServerTestHelper(testCtx.Ctx, interfaceRegistry)
	types.RegisterQueryServer(queryHelper, keeper.NewQueryServerImpl(s.keeper))
	s.queryClient = types.NewQueryClient(queryHelper)

	// Create message server
	s.msgServer = keeper.NewMsgServerImpl(s.keeper)
}

var (
	valAddr1 = sdk.ValAddress([]byte("validator1___________"))
	valAddr2 = sdk.ValAddress([]byte("validator2___________"))
	accAddr1 = sdk.AccAddress([]byte("feeder1______________"))
	accAddr2 = sdk.AccAddress([]byte("feeder2______________"))
)

func bondedValidator() stakingtypes.ValidatorI {
	return stakingtypes.Validator{Status: stakingtypes.Bonded}
}

func unbondedValidator() stakingtypes.ValidatorI {
	return stakingtypes.Validator{Status: stakingtypes.Unbonded}
}

func (s *KeeperTestSuite) TestGetFeederDelegation() {
	tests := []struct {
		name     string
		setup    func()
		operator sdk.ValAddress
		expected sdk.AccAddress
	}{
		{
			name:     "no delegation — defaults to validator address",
			setup:    func() {},
			operator: valAddr1,
			expected: sdk.AccAddress(valAddr1),
		},
		{
			name: "delegation set — returns delegate",
			setup: func() {
				s.Require().NoError(s.keeper.FeederDelegation.Set(s.ctx, valAddr1, accAddr1))
			},
			operator: valAddr1,
			expected: accAddr1,
		},
		{
			name: "multiple validators — returns correct delegate",
			setup: func() {
				s.Require().NoError(s.keeper.FeederDelegation.Set(s.ctx, valAddr1, accAddr1))
				s.Require().NoError(s.keeper.FeederDelegation.Set(s.ctx, valAddr2, accAddr2))
			},
			operator: valAddr2,
			expected: accAddr2,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			// Clear any existing delegations
			_ = s.keeper.FeederDelegation.Remove(s.ctx, valAddr1)
			_ = s.keeper.FeederDelegation.Remove(s.ctx, valAddr2)

			tc.setup()

			result, err := s.keeper.GetFeederDelegation(s.ctx, tc.operator)
			s.Require().NoError(err)
			s.Require().Equal(tc.expected, result)
		})
	}
}

func (s *KeeperTestSuite) TestGetExchangeRate() {
	tests := []struct {
		name      string
		setup     func()
		denom     string
		expected  math.LegacyDec
		expectErr bool
	}{
		{
			name:     "ark denom — always returns one",
			setup:    func() {},
			denom:    core.MicroArkDenom,
			expected: math.LegacyOneDec(),
		},
		{
			name: "known denom — returns stored rate",
			setup: func() {
				s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, "uusd", math.LegacyNewDecWithPrec(123, 2)))
			},
			denom:    "uusd",
			expected: math.LegacyNewDecWithPrec(123, 2),
		},
		{
			name:      "unknown denom — returns error",
			setup:     func() {},
			denom:     "ufoo",
			expectErr: true,
		},
		{
			name: "overwritten rate — returns latest",
			setup: func() {
				s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, "uusd", math.LegacyNewDec(1)))
				s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, "uusd", math.LegacyNewDec(2)))
			},
			denom:    "uusd",
			expected: math.LegacyNewDec(2),
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			// Clear exchange rates
			_ = s.keeper.ExchangeRate.Remove(s.ctx, "uusd")
			_ = s.keeper.ExchangeRate.Remove(s.ctx, "ufoo")

			tc.setup()

			rate, err := s.keeper.GetExchangeRate(s.ctx, tc.denom)
			if tc.expectErr {
				s.Require().Error(err)
				s.Require().ErrorContains(err, types.ErrUnknownDenom.Error())
			} else {
				s.Require().NoError(err)
				s.Require().True(tc.expected.Equal(rate), "expected %s, got %s", tc.expected, rate)
			}
		})
	}
}

func (s *KeeperTestSuite) TestSetExchangeRateWithEvent() {
	tests := []struct {
		name string
		rate math.LegacyDec
	}{
		{
			name: "set rate",
			rate: math.LegacyNewDecWithPrec(123, 2),
		},
		{
			name: "overwrite rate",
			rate: math.LegacyNewDec(5),
		},
		{
			name: "zero rate",
			rate: math.LegacyZeroDec(),
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			// Reset event manager for each sub-test
			sdkCtx := sdk.UnwrapSDKContext(s.ctx).WithEventManager(sdk.NewEventManager())
			s.ctx = sdkCtx

			err := s.keeper.SetExchangeRateWithEvent(s.ctx, "uusd", tc.rate)
			s.Require().NoError(err)

			// Verify stored rate
			stored, err := s.keeper.ExchangeRate.Get(s.ctx, "uusd")
			s.Require().NoError(err)
			s.Require().True(tc.rate.Equal(stored), "expected %s, got %s", tc.rate, stored)

			// Verify event emitted
			events := sdkCtx.EventManager().Events()
			s.Require().Len(events, 1)
			s.Require().Equal(types.EventTypeExchangeRateUpdate, events[0].Type)

			attrs := events[0].Attributes
			s.Require().Equal(types.AttributeKeyDenom, attrs[0].Key)
			s.Require().Equal("uusd", attrs[0].Value)
			s.Require().Equal(types.AttributeKeyExchangeRate, attrs[1].Key)
			s.Require().Equal(tc.rate.String(), attrs[1].Value)
		})
	}
}

func (s *KeeperTestSuite) TestValidateFeeder() {
	tests := []struct {
		name      string
		setup     func()
		feeder    sdk.AccAddress
		validator sdk.ValAddress
		mockSetup func()
		expectErr string
	}{
		{
			name:      "validator is own feeder",
			setup:     func() {},
			feeder:    sdk.AccAddress(valAddr1),
			validator: valAddr1,
			mockSetup: func() {
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(bondedValidator())
			},
		},
		{
			name: "delegated feeder — authorised",
			setup: func() {
				s.Require().NoError(s.keeper.FeederDelegation.Set(s.ctx, valAddr1, accAddr1))
			},
			feeder:    accAddr1,
			validator: valAddr1,
			mockSetup: func() {
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(bondedValidator())
			},
		},
		{
			name: "wrong feeder — unauthorised",
			setup: func() {
				s.Require().NoError(s.keeper.FeederDelegation.Set(s.ctx, valAddr1, accAddr1))
			},
			feeder:    accAddr2,
			validator: valAddr1,
			mockSetup: func() {},
			expectErr: types.ErrNoVotingPermission.Error(),
		},
		{
			name:      "no delegation and feeder is not validator — unauthorised",
			setup:     func() {},
			feeder:    accAddr1,
			validator: valAddr1,
			mockSetup: func() {},
			expectErr: types.ErrNoVotingPermission.Error(),
		},
		{
			name:      "validator not bonded",
			setup:     func() {},
			feeder:    sdk.AccAddress(valAddr1),
			validator: valAddr1,
			mockSetup: func() {
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(unbondedValidator())
			},
			expectErr: stakingtypes.ErrNoValidatorFound.Error(),
		},
		{
			name:      "validator not found",
			setup:     func() {},
			feeder:    sdk.AccAddress(valAddr1),
			validator: valAddr1,
			mockSetup: func() {
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(nil)
			},
			expectErr: stakingtypes.ErrNoValidatorFound.Error(),
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			// Clear delegations
			_ = s.keeper.FeederDelegation.Remove(s.ctx, valAddr1)
			_ = s.keeper.FeederDelegation.Remove(s.ctx, valAddr2)

			tc.setup()
			tc.mockSetup()

			err := s.keeper.ValidateFeeder(s.ctx, tc.feeder, tc.validator)
			if tc.expectErr != "" {
				s.Require().Error(err)
				s.Require().ErrorContains(err, tc.expectErr)
			} else {
				s.Require().NoError(err)
			}
		})
	}
}
