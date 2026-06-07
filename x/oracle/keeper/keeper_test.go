package keeper_test

import (
	"context"
	"testing"

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
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

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

	s.Require().NoError(s.keeper.Params.Set(s.ctx, types.DefaultParams()))

	queryHelper := baseapp.NewQueryServerTestHelper(testCtx.Ctx, interfaceRegistry)
	types.RegisterQueryServer(queryHelper, keeper.NewQueryServerImpl(s.keeper))
	s.queryClient = types.NewQueryClient(queryHelper)

	s.msgServer = keeper.NewMsgServerImpl(s.keeper)
}

func (s *KeeperTestSuite) SetupSubTest() {
	s.SetupTest()
}

var (
	valAddr1 = sdk.ValAddress([]byte("validator1___________"))
	valAddr2 = sdk.ValAddress([]byte("validator2___________"))
)

func newStoredExchangeRate(denom string, rate math.LegacyDec) types.ExchangeRate {
	return types.ExchangeRate{Denom: denom, Rate: rate}
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
			name:     "ark denom returns one",
			denom:    core.MicroArkDenom,
			expected: math.LegacyOneDec(),
		},
		{
			name: "known denom returns stored rate",
			setup: func() {
				s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, core.MicroUSDDenom, newStoredExchangeRate(core.MicroUSDDenom, math.LegacyNewDecWithPrec(123, 2))))
			},
			denom:    core.MicroUSDDenom,
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

func (s *KeeperTestSuite) TestSetExchangeRateWithEvent() {
	rate := math.LegacyNewDecWithPrec(123, 2)

	err := s.keeper.SetExchangeRateWithEvent(s.ctx, newStoredExchangeRate(core.MicroUSDDenom, rate))
	s.Require().NoError(err)

	stored, err := s.keeper.ExchangeRate.Get(s.ctx, core.MicroUSDDenom)
	s.Require().NoError(err)
	s.Require().Equal(core.MicroUSDDenom, stored.Denom)
	s.Require().True(rate.Equal(stored.Rate))

	events := sdk.UnwrapSDKContext(s.ctx).EventManager().Events()
	s.Require().Len(events, 1)
	s.Require().Equal(types.EventTypeExchangeRateUpdate, events[0].Type)
}

func (s *KeeperTestSuite) TestGetActives() {
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, core.MicroUSDDenom, types.ExchangeRate{
		Denom:       core.MicroUSDDenom,
		Rate:        math.LegacyOneDec(),
		BlockHeight: 10,
	}))
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, core.MicroKRWDenom, types.ExchangeRate{
		Denom:       core.MicroKRWDenom,
		Rate:        math.LegacyOneDec(),
		BlockHeight: 1,
	}))

	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.MaxExchangeRateAge = 5
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(12)

	actives, err := s.keeper.GetActives(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal([]string{core.MicroUSDDenom}, actives)
}

func (s *KeeperTestSuite) TestGetTobinTaxes() {
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.TobinTaxes = types.TobinTaxes{
		{Denom: core.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
	}
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

	tobinTaxes, err := s.keeper.GetTobinTaxes(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(params.TobinTaxes, tobinTaxes)
}

func (s *KeeperTestSuite) TestGetMaxTobinTax() {
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.TobinTaxes = types.TobinTaxes{
		{Denom: core.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
		{Denom: core.MicroKRWDenom, TobinTax: math.LegacyNewDecWithPrec(50, 4)},
	}
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

	tobinTax, err := s.keeper.GetMaxTobinTax(s.ctx, core.MicroUSDDenom, core.MicroKRWDenom)
	s.Require().NoError(err)
	s.Require().True(math.LegacyNewDecWithPrec(50, 4).Equal(tobinTax))

	_, err = s.keeper.GetMaxTobinTax(s.ctx, core.MicroUSDDenom, "ufoo")
	s.Require().ErrorIs(err, types.ErrUnknownDenom)
}

func (s *KeeperTestSuite) TestApplyTobinTaxChanges() {
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, core.MicroUSDDenom, newStoredExchangeRate(core.MicroUSDDenom, math.LegacyOneDec())))
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, core.MicroKRWDenom, newStoredExchangeRate(core.MicroKRWDenom, math.LegacyOneDec())))

	s.bankKeeper.EXPECT().GetDenomMetaData(s.ctx, core.MicroUSDDenom).Return(banktypes.Metadata{}, false)
	s.bankKeeper.EXPECT().SetDenomMetaData(s.ctx, gomock.Any())

	err := s.keeper.ApplyTobinTaxChanges(s.ctx, types.TobinTaxes{
		{Denom: core.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
	})
	s.Require().NoError(err)

	hasUSD, err := s.keeper.ExchangeRate.Has(s.ctx, core.MicroUSDDenom)
	s.Require().NoError(err)
	s.Require().True(hasUSD)
	hasKRW, err := s.keeper.ExchangeRate.Has(s.ctx, core.MicroKRWDenom)
	s.Require().NoError(err)
	s.Require().False(hasKRW)
}

func (s *KeeperTestSuite) TestAccountingCounters() {
	s.Require().NoError(s.keeper.IncrementMissCount(s.ctx, valAddr1))
	s.Require().NoError(s.keeper.IncrementMissCount(s.ctx, valAddr1))
	missCount, err := s.keeper.MissCount.Get(s.ctx, valAddr1)
	s.Require().NoError(err)
	s.Require().Equal(uint64(2), missCount)

	s.Require().NoError(s.keeper.AddScoreWeight(s.ctx, valAddr1, 3))
	s.Require().NoError(s.keeper.AddScoreWeight(s.ctx, valAddr1, 4))
	scoreWeight, err := s.keeper.ScoreWeight.Get(s.ctx, valAddr1)
	s.Require().NoError(err)
	s.Require().Equal(uint64(7), scoreWeight)
}
