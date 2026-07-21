package keeper_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	codectestutil "github.com/cosmos/cosmos-sdk/codec/testutil"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/std"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
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
	oracleTestBlockTime = time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
)

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
			denom:    chain.MicroNoahDenom,
			expected: math.LegacyOneDec(),
		},
		{
			name: "known denom returns stored rate",
			setup: func() {
				s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.MicroUSDDenom, newStoredExchangeRate(chain.MicroUSDDenom, math.LegacyNewDecWithPrec(123, 2))))
			},
			denom:    chain.MicroUSDDenom,
			expected: math.LegacyNewDecWithPrec(123, 2),
		},
		{
			name:      "unknown denom returns error",
			denom:     "ufoo",
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

func (s *KeeperTestSuite) TestGetRateSnapshot() {
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.MicroUSDDenom, types.ExchangeRate{
		Denom:          chain.MicroUSDDenom,
		Rate:           math.LegacyOneDec(),
		BlockTimestamp: oracleTestBlockTime.Add(-30 * time.Second),
	}))
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.MicroSDRDenom, types.ExchangeRate{
		Denom:          chain.MicroSDRDenom,
		Rate:           math.LegacyMustNewDecFromStr("1.7"),
		BlockTimestamp: oracleTestBlockTime.Add(-30 * time.Second),
	}))

	rates, err := s.keeper.GetRateSnapshot(
		s.ctx,
		chain.MicroUSDDenom,
		chain.MicroSDRDenom,
		chain.MicroUSDDenom,
	)
	s.Require().NoError(err)
	s.Require().Len(rates, 3)
	s.Require().True(rates[chain.MicroUSDDenom].Equal(math.LegacyOneDec()))
	s.Require().True(rates[chain.MicroSDRDenom].Equal(math.LegacyMustNewDecFromStr("1.7")))
	s.Require().True(rates[chain.MicroNoahDenom].Equal(math.LegacyOneDec()))
}

func (s *KeeperTestSuite) TestGetRateSnapshotReturnsNoahIdentityByDefault() {
	rates, err := s.keeper.GetRateSnapshot(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(types.RateSnapshot{
		chain.MicroNoahDenom: math.LegacyOneDec(),
	}, rates)
}

func (s *KeeperTestSuite) TestGetRateSnapshotRejectsElapsedTimeStaleness() {
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.MaxExchangeRateAge = time.Minute
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.MicroUSDDenom, types.ExchangeRate{
		Denom:          chain.MicroUSDDenom,
		Rate:           math.LegacyOneDec(),
		BlockTimestamp: oracleTestBlockTime.Add(-time.Minute - time.Second),
	}))

	_, err = s.keeper.GetRateSnapshot(s.ctx, chain.MicroUSDDenom)
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
				Denom:          chain.MicroUSDDenom,
				Rate:           math.LegacyDec{},
				BlockTimestamp: oracleTestBlockTime,
			},
		},
		{
			name: "rate is not positive",
			exchangeRate: types.ExchangeRate{
				Denom:          chain.MicroUSDDenom,
				Rate:           math.LegacyZeroDec(),
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
		Denom:          chain.MicroUSDDenom,
		Rate:           math.LegacyOneDec(),
		BlockTimestamp: oracleTestBlockTime.Add(time.Second),
	})
	s.Require().ErrorIs(err, types.ErrInvalidExchangeRate)

	has, getErr := s.keeper.ExchangeRate.Has(s.ctx, chain.MicroUSDDenom)
	s.Require().NoError(getErr)
	s.Require().False(has)
	s.Require().Empty(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())
}

func (s *KeeperTestSuite) TestSetExchangeRateWithEvent() {
	rate := math.LegacyNewDecWithPrec(123, 2)

	err := s.keeper.SetExchangeRateWithEvent(s.ctx, newStoredExchangeRate(chain.MicroUSDDenom, rate))
	s.Require().NoError(err)

	stored, err := s.keeper.ExchangeRate.Get(s.ctx, chain.MicroUSDDenom)
	s.Require().NoError(err)
	s.Require().Equal(chain.MicroUSDDenom, stored.Denom)
	s.Require().True(rate.Equal(stored.Rate))

	events := sdk.UnwrapSDKContext(s.ctx).EventManager().Events()
	s.requireTypedEvents(events, &types.EventExchangeRateUpdate{
		Denom:        chain.MicroUSDDenom,
		ExchangeRate: rate,
	})
}

func (s *KeeperTestSuite) TestSetExchangeRateWithEventRejectsInvalidDenom() {
	err := s.keeper.SetExchangeRateWithEvent(s.ctx, newStoredExchangeRate("uUSD", math.LegacyOneDec()))
	s.Require().ErrorContains(err, "invalid exchange rate denom")

	has, getErr := s.keeper.ExchangeRate.Has(s.ctx, "uUSD")
	s.Require().NoError(getErr)
	s.Require().False(has)
	s.Require().Empty(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())
}

func (s *KeeperTestSuite) TestGetExchangeRates() {
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.MicroUSDDenom, types.ExchangeRate{
		Denom:          chain.MicroUSDDenom,
		Rate:           math.LegacyOneDec(),
		BlockTimestamp: oracleTestBlockTime.Add(-30 * time.Second),
	}))
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.MicroKRWDenom, types.ExchangeRate{
		Denom:          chain.MicroKRWDenom,
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
		sdk.DecCoins{sdk.NewDecCoinFromDec(chain.MicroUSDDenom, math.LegacyOneDec())},
		exchangeRates,
	)
}

func (s *KeeperTestSuite) TestGetTobinTaxes() {
	expected := types.TobinTaxes{
		{Denom: chain.MicroKRWDenom, TobinTax: math.LegacyNewDecWithPrec(50, 4)},
		{Denom: chain.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
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
	params.TobinTaxes = types.TobinTaxes{{Denom: chain.MicroUSDDenom, TobinTax: expected}}
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

	tobinTax, err := s.keeper.GetTobinTax(s.ctx, chain.MicroUSDDenom)
	s.Require().NoError(err)
	s.Require().True(expected.Equal(tobinTax))

	_, err = s.keeper.GetTobinTax(s.ctx, "ufoo")
	s.Require().Error(err)
	s.Require().ErrorContains(err, types.ErrUnknownDenom.Error())
}

func (s *KeeperTestSuite) TestGetVoteTargets() {
	expected := []string{chain.MicroKRWDenom, chain.MicroUSDDenom}
	s.Require().NoError(s.keeper.VoteTargets.Set(s.ctx, types.VoteTargets{Denoms: expected}))

	voteTargets, err := s.keeper.GetVoteTargets(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(expected, voteTargets)
}

func (s *KeeperTestSuite) TestSyncVoteTargets() {
	oldVoteTargets := []string{chain.MicroKRWDenom, chain.MicroUSDDenom}
	s.Require().NoError(s.keeper.VoteTargets.Set(s.ctx, types.VoteTargets{Denoms: oldVoteTargets}))
	for _, denom := range oldVoteTargets {
		s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, denom, newStoredExchangeRate(denom, math.LegacyOneDec())))
	}

	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.TobinTaxes = types.TobinTaxes{
		{Denom: chain.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(5, 2)},
		{Denom: chain.MicroSDRDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
	}
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

	s.Require().NoError(s.keeper.SyncVoteTargets(s.ctx, oldVoteTargets))

	// SyncVoteTargets does not mutate the caller's old target slice.
	s.Require().Equal([]string{chain.MicroKRWDenom, chain.MicroUSDDenom}, oldVoteTargets)
	voteTargets, err := s.keeper.GetVoteTargets(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal([]string{chain.MicroSDRDenom, chain.MicroUSDDenom}, voteTargets)

	updatedTobinTax, err := s.keeper.GetTobinTax(s.ctx, chain.MicroUSDDenom)
	s.Require().NoError(err)
	s.Require().True(math.LegacyNewDecWithPrec(5, 2).Equal(updatedTobinTax))

	addedTobinTax, err := s.keeper.GetTobinTax(s.ctx, chain.MicroSDRDenom)
	s.Require().NoError(err)
	s.Require().True(math.LegacyNewDecWithPrec(25, 4).Equal(addedTobinTax))

	_, err = s.keeper.GetTobinTax(s.ctx, chain.MicroKRWDenom)
	s.Require().Error(err)
	s.Require().ErrorContains(err, types.ErrUnknownDenom.Error())

	hasUSDExchangeRate, err := s.keeper.ExchangeRate.Has(s.ctx, chain.MicroUSDDenom)
	s.Require().NoError(err)
	s.Require().True(hasUSDExchangeRate)

	hasKRWExchangeRate, err := s.keeper.ExchangeRate.Has(s.ctx, chain.MicroKRWDenom)
	s.Require().NoError(err)
	s.Require().False(hasKRWExchangeRate)
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
