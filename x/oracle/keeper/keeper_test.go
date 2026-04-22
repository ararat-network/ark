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
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
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

func (s *KeeperTestSuite) SetupSubTest() {
	s.SetupTest()
}

// --- Helpers ---
var (
	valAddr1 = sdk.ValAddress([]byte("validator1___________"))
	valAddr2 = sdk.ValAddress([]byte("validator2___________"))
	accAddr1 = sdk.AccAddress([]byte("feeder1______________"))
	accAddr2 = sdk.AccAddress([]byte("feeder2______________"))
)

func (s *KeeperTestSuite) TestGetFeederDelegation() {
	tests := []struct {
		name     string
		setup    func()
		operator sdk.ValAddress
		expected sdk.AccAddress
	}{
		{
			name:     "no delegation - defaults to validator address",
			setup:    func() {},
			operator: valAddr1,
			expected: sdk.AccAddress(valAddr1),
		},
		{
			name: "delegation set - returns delegate",
			setup: func() {
				s.Require().NoError(s.keeper.FeederDelegation.Set(s.ctx, valAddr1, accAddr1))
			},
			operator: valAddr1,
			expected: accAddr1,
		},
		{
			name: "multiple validators - returns correct delegate",
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
			name:     "ark denom - always returns one",
			setup:    func() {},
			denom:    core.MicroArkDenom,
			expected: math.LegacyOneDec(),
		},
		{
			name: "known denom - returns stored rate",
			setup: func() {
				s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, "uusd", math.LegacyNewDecWithPrec(123, 2)))
			},
			denom:    "uusd",
			expected: math.LegacyNewDecWithPrec(123, 2),
		},
		{
			name:      "unknown denom - returns error",
			setup:     func() {},
			denom:     "ufoo",
			expectErr: true,
		},
		{
			name: "overwritten rate - returns latest",
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

func (s *KeeperTestSuite) TestSetExchangeRate() {
	rate := math.LegacyNewDecWithPrec(123, 2)

	err := s.keeper.SetExchangeRate(s.ctx, "uusd", rate)
	s.Require().NoError(err)

	stored, err := s.keeper.ExchangeRate.Get(s.ctx, "uusd")
	s.Require().NoError(err)
	s.Require().True(rate.Equal(stored), "expected %s, got %s", rate, stored)
}

func (s *KeeperTestSuite) TestIterateExchangeRates() {
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, "uusd", math.LegacyNewDec(1)))
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, "ukrw", math.LegacyNewDec(2)))

	visited := map[string]math.LegacyDec{}
	s.Require().NoError(s.keeper.IterateExchangeRates(s.ctx, func(denom string, rate math.LegacyDec) bool {
		visited[denom] = rate
		return false
	}))

	s.Require().Len(visited, 2)
	s.Require().True(math.LegacyNewDec(1).Equal(visited["uusd"]))
	s.Require().True(math.LegacyNewDec(2).Equal(visited["ukrw"]))
}

func (s *KeeperTestSuite) TestIterateExchangeRatesStops() {
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, "uusd", math.LegacyNewDec(1)))
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, "ukrw", math.LegacyNewDec(2)))

	count := 0
	s.Require().NoError(s.keeper.IterateExchangeRates(s.ctx, func(_ string, _ math.LegacyDec) bool {
		count++
		return true
	}))

	s.Require().Equal(1, count)
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
			err := s.keeper.SetExchangeRateWithEvent(s.ctx, "uusd", tc.rate)
			s.Require().NoError(err)

			stored, err := s.keeper.ExchangeRate.Get(s.ctx, "uusd")
			s.Require().NoError(err)
			s.Require().True(tc.rate.Equal(stored), "expected %s, got %s", tc.rate, stored)
		})
	}
}

func (s *KeeperTestSuite) TestSetExchangeRateWithEventEmitsEvent() {
	rate := math.LegacyNewDecWithPrec(123, 2)

	err := s.keeper.SetExchangeRateWithEvent(s.ctx, "uusd", rate)
	s.Require().NoError(err)

	events := sdk.UnwrapSDKContext(s.ctx).EventManager().Events()
	s.Require().Len(events, 1)
	s.Require().Equal(types.EventTypeExchangeRateUpdate, events[0].Type)

	attrs := events[0].Attributes
	s.Require().Equal(types.AttributeKeyDenom, attrs[0].Key)
	s.Require().Equal("uusd", attrs[0].Value)
	s.Require().Equal(types.AttributeKeyExchangeRate, attrs[1].Key)
	s.Require().Equal(rate.String(), attrs[1].Value)
}

func (s *KeeperTestSuite) TestValidateFeeder() {
	tests := []struct {
		name      string
		setup     func()
		feeder    sdk.AccAddress
		validator sdk.ValAddress
		expectErr string
	}{
		{
			name: "validator is own feeder",
			setup: func() {
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(stakingtypes.Validator{Status: stakingtypes.Bonded})
			},
			feeder:    sdk.AccAddress(valAddr1),
			validator: valAddr1,
		},
		{
			name: "delegated feeder — authorised",
			setup: func() {
				s.Require().NoError(s.keeper.FeederDelegation.Set(s.ctx, valAddr1, accAddr1))
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(stakingtypes.Validator{Status: stakingtypes.Bonded})
			},
			feeder:    accAddr1,
			validator: valAddr1,
		},
		{
			name: "wrong feeder — unauthorised",
			setup: func() {
				s.Require().NoError(s.keeper.FeederDelegation.Set(s.ctx, valAddr1, accAddr1))
			},
			feeder:    accAddr2,
			validator: valAddr1,
			expectErr: types.ErrNoVotingPermission.Error(),
		},
		{
			name:      "no delegation and feeder is not validator — unauthorised",
			setup:     func() {},
			feeder:    accAddr1,
			validator: valAddr1,
			expectErr: types.ErrNoVotingPermission.Error(),
		},
		{
			name: "validator not bonded",
			setup: func() {
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(stakingtypes.Validator{Status: stakingtypes.Unbonded})
			},
			feeder:    sdk.AccAddress(valAddr1),
			validator: valAddr1,
			expectErr: stakingtypes.ErrNoValidatorFound.Error(),
		},
		{
			name: "validator not found",
			setup: func() {
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(nil)
			},
			feeder:    sdk.AccAddress(valAddr1),
			validator: valAddr1,
			expectErr: stakingtypes.ErrNoValidatorFound.Error(),
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			tc.setup()

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

func (s *KeeperTestSuite) TestGetTobinTax() {
	tobinTax := math.LegacyNewDecWithPrec(25, 4)

	tests := []struct {
		name      string
		setup     func()
		denom     string
		expected  math.LegacyDec
		expectErr bool
	}{
		{
			name: "stored denom",
			setup: func() {
				s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, core.MicroUSDDenom, tobinTax))
			},
			denom:    core.MicroUSDDenom,
			expected: tobinTax,
		},
		{
			name:      "unknown denom",
			setup:     func() {},
			denom:     core.MicroUSDDenom,
			expected:  math.LegacyZeroDec(),
			expectErr: true,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			tc.setup()

			tobinTax, err := s.keeper.GetTobinTax(s.ctx, tc.denom)
			if tc.expectErr {
				s.Require().Error(err)
			} else {
				s.Require().NoError(err)
			}
			s.Require().True(tc.expected.Equal(tobinTax), "expected %s, got %s", tc.expected, tobinTax)
		})
	}
}

func (s *KeeperTestSuite) TestSetTobinTax() {
	tobinTax := math.LegacyNewDecWithPrec(25, 4)

	err := s.keeper.SetTobinTax(s.ctx, core.MicroUSDDenom, tobinTax)
	s.Require().NoError(err)

	stored, err := s.keeper.TobinTax.Get(s.ctx, core.MicroUSDDenom)
	s.Require().NoError(err)
	s.Require().True(tobinTax.Equal(stored), "expected %s, got %s", tobinTax, stored)
}

func (s *KeeperTestSuite) TestGetTobinTaxes() {
	params := types.DefaultParams()
	params.TobinTaxes = types.TobinTaxes{
		{Denom: "uparams", TobinTax: math.LegacyNewDecWithPrec(10, 4)},
	}
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, core.MicroUSDDenom, math.LegacyNewDecWithPrec(25, 4)))
	s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, core.MicroKRWDenom, math.LegacyNewDecWithPrec(125, 5)))

	tobinTaxes, err := s.keeper.GetTobinTaxes(s.ctx)
	s.Require().NoError(err)

	actual := map[string]math.LegacyDec{}
	for _, item := range tobinTaxes {
		actual[item.Denom] = item.TobinTax
	}
	s.Require().Len(actual, 2)
	s.Require().True(math.LegacyNewDecWithPrec(25, 4).Equal(actual[core.MicroUSDDenom]))
	s.Require().True(math.LegacyNewDecWithPrec(125, 5).Equal(actual[core.MicroKRWDenom]))
}

func (s *KeeperTestSuite) TestSetTobinTaxes() {
	tests := []struct {
		name     string
		initial  map[string]math.LegacyDec
		set      types.TobinTaxes
		expected map[string]math.LegacyDec
		setup    func()
	}{
		{
			name: "empty set clears existing taxes",
			initial: map[string]math.LegacyDec{
				core.MicroUSDDenom: math.LegacyNewDecWithPrec(25, 4),
				core.MicroKRWDenom: math.LegacyNewDecWithPrec(125, 5),
			},
			set:      types.TobinTaxes{},
			expected: map[string]math.LegacyDec{},
		},
		{
			name: "set replaces stale taxes",
			initial: map[string]math.LegacyDec{
				core.MicroUSDDenom: math.LegacyNewDecWithPrec(25, 4),
				core.MicroKRWDenom: math.LegacyNewDecWithPrec(125, 5),
			},
			set: types.TobinTaxes{
				{Denom: core.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(50, 4)},
			},
			setup: func() {
				s.bankKeeper.EXPECT().GetDenomMetaData(s.ctx, core.MicroUSDDenom).Return(banktypes.Metadata{}, true)
			},
			expected: map[string]math.LegacyDec{
				core.MicroUSDDenom: math.LegacyNewDecWithPrec(50, 4),
			},
		},
		{
			name: "missing metadata is registered",
			set: types.TobinTaxes{
				{Denom: core.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
			},
			setup: func() {
				s.bankKeeper.EXPECT().GetDenomMetaData(s.ctx, core.MicroUSDDenom).Return(banktypes.Metadata{}, false)
				s.bankKeeper.EXPECT().SetDenomMetaData(s.ctx, gomock.Any()).Do(
					func(_ interface{}, meta banktypes.Metadata) {
						s.Require().Equal("uusd", meta.Base)
						s.Require().Equal("usd", meta.Display)
						s.Require().Equal("USD NOAH", meta.Name)
						s.Require().Equal("USN", meta.Symbol)
						s.Require().Equal("The native stable token of Noah Icarus.", meta.Description)
						s.Require().Len(meta.DenomUnits, 3)
						s.Require().Equal("uusd", meta.DenomUnits[0].Denom)
						s.Require().Equal(uint32(0), meta.DenomUnits[0].Exponent)
						s.Require().Equal("musd", meta.DenomUnits[1].Denom)
						s.Require().Equal(uint32(3), meta.DenomUnits[1].Exponent)
						s.Require().Equal("usd", meta.DenomUnits[2].Denom)
						s.Require().Equal(uint32(6), meta.DenomUnits[2].Exponent)
					},
				)
			},
			expected: map[string]math.LegacyDec{
				core.MicroUSDDenom: math.LegacyNewDecWithPrec(25, 4),
			},
		},
		{
			name: "existing metadata is not registered again",
			set: types.TobinTaxes{
				{Denom: core.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
			},
			setup: func() {
				s.bankKeeper.EXPECT().GetDenomMetaData(s.ctx, core.MicroUSDDenom).Return(banktypes.Metadata{}, true)
			},
			expected: map[string]math.LegacyDec{
				core.MicroUSDDenom: math.LegacyNewDecWithPrec(25, 4),
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			for denom, tobinTax := range tc.initial {
				s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, denom, tobinTax))
			}
			if tc.setup != nil {
				tc.setup()
			}

			err := s.keeper.SetTobinTaxes(s.ctx, tc.set)
			s.Require().NoError(err)
			s.Require().Equal(tc.expected, s.tobinTaxMap())
		})
	}
}

func (s *KeeperTestSuite) TestSyncTobinTaxes() {
	tests := []struct {
		name     string
		initial  map[string]math.LegacyDec
		params   types.TobinTaxes
		expected map[string]math.LegacyDec
		setup    func()
	}{
		{
			name: "unchanged taxes skips metadata sync",
			initial: map[string]math.LegacyDec{
				core.MicroUSDDenom: math.LegacyNewDecWithPrec(25, 4),
			},
			params: types.TobinTaxes{
				{Denom: core.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
			},
			expected: map[string]math.LegacyDec{
				core.MicroUSDDenom: math.LegacyNewDecWithPrec(25, 4),
			},
		},
		{
			name: "changed taxes are replaced",
			initial: map[string]math.LegacyDec{
				core.MicroUSDDenom: math.LegacyNewDecWithPrec(25, 4),
			},
			params: types.TobinTaxes{
				{Denom: core.MicroKRWDenom, TobinTax: math.LegacyNewDecWithPrec(50, 4)},
			},
			setup: func() {
				s.bankKeeper.EXPECT().GetDenomMetaData(s.ctx, core.MicroKRWDenom).Return(banktypes.Metadata{}, true)
			},
			expected: map[string]math.LegacyDec{
				core.MicroKRWDenom: math.LegacyNewDecWithPrec(50, 4),
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			for denom, tobinTax := range tc.initial {
				s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, denom, tobinTax))
			}
			if tc.setup != nil {
				tc.setup()
			}

			err := s.keeper.SyncTobinTaxes(s.ctx, tc.initial, tc.params)
			s.Require().NoError(err)
			s.Require().Equal(tc.expected, s.tobinTaxMap())
		})
	}
}

func (s *KeeperTestSuite) tobinTaxMap() map[string]math.LegacyDec {
	tobinTaxes := make(map[string]math.LegacyDec)
	err := s.keeper.TobinTax.Walk(s.ctx, nil, func(denom string, tobinTax math.LegacyDec) (bool, error) {
		tobinTaxes[denom] = tobinTax
		return false, nil
	})
	s.Require().NoError(err)
	return tobinTaxes
}
