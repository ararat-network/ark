package keeper_test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	codectestutil "github.com/cosmos/cosmos-sdk/codec/testutil"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/std"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/gogoproto/proto"

	chain "ark/pkg/chain"
	"ark/x/oracle/keeper"
	"ark/x/oracle/testutil"
	"ark/x/oracle/types"
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

func TestNewKeeperRequiresDistributionModuleAccount(t *testing.T) {
	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	types.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)
	storeService := runtime.NewKVStoreService(storetypes.NewKVStoreKey(types.StoreKey))

	ctrl := gomock.NewController(t)
	accountKeeper := testutil.NewMockAccountKeeper(ctrl)
	accountKeeper.EXPECT().GetModuleAddress(types.ModuleName).Return(sdk.AccAddress{1})
	accountKeeper.EXPECT().GetModuleAddress("distribution").Return(nil)

	requirePanic := func() {
		keeper.NewKeeper(
			cdc,
			storeService,
			authtypes.NewModuleAddress(govtypes.ModuleName).String(),
			"distribution",
			accountKeeper,
			nil,
			nil,
			nil,
		)
	}

	require.PanicsWithValue(t, "distribution module account has not been set", requirePanic)
}

func (s *KeeperTestSuite) SetupTest() {
	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	types.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)

	key := storetypes.NewKVStoreKey(types.StoreKey)
	storeService := runtime.NewKVStoreService(key)
	testCtx := sdktestutil.DefaultContextWithDB(s.T(), key, storetypes.NewTransientStoreKey("transient_test"))
	s.ctx = sdk.UnwrapSDKContext(testCtx.Ctx).WithBlockTime(oracleTestBlockTime)

	ctrl := gomock.NewController(s.T())

	s.accountKeeper = testutil.NewMockAccountKeeper(ctrl)
	s.bankKeeper = testutil.NewMockBankKeeper(ctrl)
	s.distrKeeper = testutil.NewMockDistributionKeeper(ctrl)
	s.stakingKeeper = testutil.NewMockStakingKeeper(ctrl)

	s.accountKeeper.EXPECT().GetModuleAddress(types.ModuleName).Return(sdk.AccAddress{1})
	s.accountKeeper.EXPECT().GetModuleAddress("distribution").Return(sdk.AccAddress{2})

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

	params := types.DefaultParams()
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.Require().NoError(s.keeper.Accounting.Set(s.ctx, types.NewAccounting(params)))

	queryHelper := baseapp.NewQueryServerTestHelper(sdk.UnwrapSDKContext(s.ctx), interfaceRegistry)
	types.RegisterQueryServer(queryHelper, keeper.NewQueryServerImpl(s.keeper))
	s.queryClient = types.NewQueryClient(queryHelper)

	s.msgServer = keeper.NewMsgServerImpl(s.keeper)
}

func (s *KeeperTestSuite) SetupSubTest() {
	s.SetupTest()
}

var (
	valAddr1            = sdk.ValAddress([]byte("validator1___________"))
	valAddr2            = sdk.ValAddress([]byte("validator2___________"))
	valAddr3            = sdk.ValAddress([]byte("validator3___________"))
	oracleTestBlockTime = time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
)

// newBondedValidator builds a bonded validator fixture with operator address
// valAddr and returns it alongside its derived consensus address. Callers
// that need a different bond status or jailed flag mutate the returned
// (local, by-value) validator before arming a mock expectation with it.
func (s *KeeperTestSuite) newBondedValidator(valAddr sdk.ValAddress) (sdk.ConsAddress, stakingtypes.Validator) {
	s.T().Helper()

	pubKey := ed25519.GenPrivKey().PubKey()
	validator, err := stakingtypes.NewValidator(valAddr.String(), pubKey, stakingtypes.Description{})
	s.Require().NoError(err)
	validator.Status = stakingtypes.Bonded
	consAddr, err := validator.GetConsAddr()
	s.Require().NoError(err)

	return consAddr, validator
}

func newStoredExchangeRate(denom string, rate math.LegacyDec) types.ExchangeRate {
	return types.ExchangeRate{Denom: denom, Rate: rate, BlockTimestamp: oracleTestBlockTime}
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
			name:     "noah denom returns one",
			denom:    chain.NoahBaseDenom,
			expected: math.LegacyOneDec(),
		},
		{
			name: "known denom returns stored rate",
			setup: func() {
				s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.USDBaseDenom, newStoredExchangeRate(chain.USDBaseDenom, math.LegacyNewDecWithPrec(123, 2))))
			},
			denom:    chain.USDBaseDenom,
			expected: math.LegacyNewDecWithPrec(123, 2),
		},
		{
			name:      "unknown denom returns error",
			denom:     "afoo",
			expectErr: true,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			if tc.setup != nil {
				tc.setup()
			}

			rate, err := s.keeper.GetExchangeRate(s.ctx, tc.denom)
			if tc.expectErr {
				s.Require().Error(err)
				s.Require().ErrorContains(err, types.ErrUnknownDenom.Error())
				return
			}

			s.Require().NoError(err)
			s.Require().True(tc.expected.Equal(rate), "expected %s, got %s", tc.expected, rate)
		})
	}
}

func (s *KeeperTestSuite) TestGetRateSet() {
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.USDBaseDenom, types.ExchangeRate{
		Denom:          chain.USDBaseDenom,
		Rate:           math.LegacyOneDec(),
		BlockTimestamp: oracleTestBlockTime.Add(-30 * time.Second),
	}))
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.SDRBaseDenom, types.ExchangeRate{
		Denom:          chain.SDRBaseDenom,
		Rate:           math.LegacyMustNewDecFromStr("1.7"),
		BlockTimestamp: oracleTestBlockTime.Add(-30 * time.Second),
	}))

	rates, err := s.keeper.GetRateSet(
		s.ctx,
		chain.USDBaseDenom,
		chain.SDRBaseDenom,
		chain.USDBaseDenom,
	)
	s.Require().NoError(err)
	s.Require().Len(rates, 3)
	s.Require().True(rates[chain.USDBaseDenom].Equal(math.LegacyOneDec()))
	s.Require().True(rates[chain.SDRBaseDenom].Equal(math.LegacyMustNewDecFromStr("1.7")))
	s.Require().True(rates[chain.NoahBaseDenom].Equal(math.LegacyOneDec()))
}

func (s *KeeperTestSuite) TestGetRateSetReturnsNoahIdentityByDefault() {
	rates, err := s.keeper.GetRateSet(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(types.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
	}, rates)
}

func (s *KeeperTestSuite) TestGetRateSetRejectsElapsedTimeStaleness() {
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.MaxExchangeRateAge = time.Minute
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.USDBaseDenom, types.ExchangeRate{
		Denom:          chain.USDBaseDenom,
		Rate:           math.LegacyOneDec(),
		BlockTimestamp: oracleTestBlockTime.Add(-time.Minute - time.Second),
	}))

	_, err = s.keeper.GetRateSet(s.ctx, chain.USDBaseDenom)
	s.Require().ErrorIs(err, types.ErrStaleExchangeRate)
}

func (s *KeeperTestSuite) TestSetExchangeRateWithEventRejectsInvalidRate() {
	tests := []struct {
		name         string
		exchangeRate types.ExchangeRate
	}{
		{
			name: "rate is unset",
			exchangeRate: types.ExchangeRate{
				Denom:          chain.USDBaseDenom,
				Rate:           math.LegacyDec{},
				BlockTimestamp: oracleTestBlockTime,
			},
		},
		{
			name: "rate is not positive",
			exchangeRate: types.ExchangeRate{
				Denom:          chain.USDBaseDenom,
				Rate:           math.LegacyZeroDec(),
				BlockTimestamp: oracleTestBlockTime,
			},
		},
		{
			name: "rate is out of range",
			exchangeRate: types.ExchangeRate{
				Denom: chain.USDBaseDenom,
				Rate: math.LegacyNewDecFromBigInt(
					new(big.Int).Lsh(big.NewInt(1), 256),
				),
				BlockTimestamp: oracleTestBlockTime,
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			err := s.keeper.SetExchangeRateWithEvent(s.ctx, tc.exchangeRate)
			s.Require().ErrorIs(err, types.ErrInvalidExchangeRate)

			has, getErr := s.keeper.ExchangeRate.Has(s.ctx, tc.exchangeRate.Denom)
			s.Require().NoError(getErr)
			s.Require().False(has)
			s.Require().Empty(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())
		})
	}
}

func (s *KeeperTestSuite) TestSetExchangeRateWithEventRejectsFutureTimestamp() {
	err := s.keeper.SetExchangeRateWithEvent(s.ctx, types.ExchangeRate{
		Denom:          chain.USDBaseDenom,
		Rate:           math.LegacyOneDec(),
		BlockTimestamp: oracleTestBlockTime.Add(time.Second),
	})
	s.Require().ErrorIs(err, types.ErrInvalidExchangeRate)

	has, getErr := s.keeper.ExchangeRate.Has(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(getErr)
	s.Require().False(has)
	s.Require().Empty(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())
}

func (s *KeeperTestSuite) TestSetExchangeRateWithEvent() {
	rate := math.LegacyNewDecWithPrec(123, 2)

	err := s.keeper.SetExchangeRateWithEvent(s.ctx, newStoredExchangeRate(chain.USDBaseDenom, rate))
	s.Require().NoError(err)

	stored, err := s.keeper.ExchangeRate.Get(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(chain.USDBaseDenom, stored.Denom)
	s.Require().True(rate.Equal(stored.Rate))

	events := sdk.UnwrapSDKContext(s.ctx).EventManager().Events()
	s.requireTypedEvents(events, &types.EventExchangeRateUpdate{
		Denom:        chain.USDBaseDenom,
		ExchangeRate: rate,
	})
}

func (s *KeeperTestSuite) TestSetExchangeRateWithEventRejectsInvalidDenom() {
	err := s.keeper.SetExchangeRateWithEvent(s.ctx, newStoredExchangeRate("aUSD", math.LegacyOneDec()))
	s.Require().ErrorContains(err, "invalid exchange rate denom")

	has, getErr := s.keeper.ExchangeRate.Has(s.ctx, "aUSD")
	s.Require().NoError(getErr)
	s.Require().False(has)
	s.Require().Empty(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())
}

func (s *KeeperTestSuite) TestGetExchangeRates() {
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.USDBaseDenom, types.ExchangeRate{
		Denom:          chain.USDBaseDenom,
		Rate:           math.LegacyOneDec(),
		BlockTimestamp: oracleTestBlockTime.Add(-30 * time.Second),
	}))
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.KRWBaseDenom, types.ExchangeRate{
		Denom:          chain.KRWBaseDenom,
		Rate:           math.LegacyOneDec(),
		BlockTimestamp: oracleTestBlockTime.Add(-2 * time.Minute),
	}))

	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.MaxExchangeRateAge = time.Minute
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

	exchangeRates, err := s.keeper.GetExchangeRates(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(
		sdk.DecCoins{sdk.NewDecCoinFromDec(chain.USDBaseDenom, math.LegacyOneDec())},
		exchangeRates,
	)
}

func (s *KeeperTestSuite) TestGetTobinTaxes() {
	expected := []types.TobinTax{
		{Denom: chain.KRWBaseDenom, TobinTax: math.LegacyNewDecWithPrec(50, 4)},
		{Denom: chain.USDBaseDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
	}
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.TobinTaxes = expected
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

	tobinTaxes, err := s.keeper.GetTobinTaxes(s.ctx)
	s.Require().NoError(err)
	s.Require().ElementsMatch(expected, tobinTaxes)
}

func (s *KeeperTestSuite) TestGetTobinTax() {
	expected := math.LegacyNewDecWithPrec(25, 4)
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.TobinTaxes = []types.TobinTax{{Denom: chain.USDBaseDenom, TobinTax: expected}}
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

	tobinTax, err := s.keeper.GetTobinTax(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().True(expected.Equal(tobinTax))

	_, err = s.keeper.GetTobinTax(s.ctx, "afoo")
	s.Require().Error(err)
	s.Require().ErrorContains(err, types.ErrUnknownDenom.Error())
}

func (s *KeeperTestSuite) requireTypedEvents(actual sdk.Events, expected ...proto.Message) {
	s.Require().Len(actual, len(expected))
	for i, expectedMessage := range expected {
		expectedEvent, err := sdk.TypedEventToEvent(expectedMessage)
		s.Require().NoError(err)
		s.Require().Equal(expectedEvent, actual[i])
		parsed, err := sdk.ParseTypedEvent(sdk.Events{actual[i]}.ToABCIEvents()[0])
		s.Require().NoError(err)
		roundTripEvent, err := sdk.TypedEventToEvent(parsed)
		s.Require().NoError(err)
		s.Require().Equal(expectedEvent, roundTripEvent)
	}
}
