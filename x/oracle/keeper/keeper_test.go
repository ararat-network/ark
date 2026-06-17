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
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/std"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	core "noah/pkg/types"
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
	expected := types.TobinTaxes{
		{Denom: core.MicroKRWDenom, TobinTax: math.LegacyNewDecWithPrec(50, 4)},
		{Denom: core.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
	}
	for _, tt := range expected {
		s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, tt.Denom, tt.TobinTax))
	}

	tobinTaxes, err := s.keeper.GetTobinTaxes(s.ctx)
	s.Require().NoError(err)
	s.Require().ElementsMatch(expected, tobinTaxes)
}

func (s *KeeperTestSuite) TestGetTobinTax() {
	expected := math.LegacyNewDecWithPrec(25, 4)
	s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, core.MicroUSDDenom, expected))

	tobinTax, err := s.keeper.GetTobinTax(s.ctx, core.MicroUSDDenom)
	s.Require().NoError(err)
	s.Require().True(expected.Equal(tobinTax))

	_, err = s.keeper.GetTobinTax(s.ctx, "ufoo")
	s.Require().Error(err)
	s.Require().ErrorContains(err, types.ErrUnknownDenom.Error())
}

func (s *KeeperTestSuite) TestGetVoteTargets() {
	expected := map[string]math.LegacyDec{
		core.MicroKRWDenom: math.LegacyNewDecWithPrec(50, 4),
		core.MicroUSDDenom: math.LegacyNewDecWithPrec(25, 4),
	}
	for denom, tobinTax := range expected {
		s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, denom, tobinTax))
	}

	voteTargets, err := s.keeper.GetVoteTargets(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(voteTargets, len(expected))
	for denom, expectedTobinTax := range expected {
		s.Require().True(expectedTobinTax.Equal(voteTargets[denom]))
	}
}

func (s *KeeperTestSuite) TestSyncTobinTax() {
	oldTobinTaxes := map[string]math.LegacyDec{
		core.MicroKRWDenom: math.LegacyNewDecWithPrec(25, 4),
		core.MicroUSDDenom: math.LegacyNewDecWithPrec(25, 4),
	}
	for denom, tobinTax := range oldTobinTaxes {
		s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, denom, tobinTax))
		s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, denom, newStoredExchangeRate(denom, math.LegacyOneDec())))
	}

	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.TobinTaxes = types.TobinTaxes{
		{Denom: core.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(5, 2)},
		{Denom: core.MicroSDRDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
	}
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

	s.bankKeeper.EXPECT().
		GetDenomMetaData(s.ctx, core.MicroSDRDenom).
		Return(banktypes.Metadata{}, false)
	s.bankKeeper.EXPECT().
		SetDenomMetaData(s.ctx, gomock.Any()).
		Do(func(_ context.Context, metadata banktypes.Metadata) {
			s.Require().Equal(core.MicroSDRDenom, metadata.Base)
			s.Require().Equal("sdr", metadata.Display)
		})

	s.Require().NoError(s.keeper.SyncTobinTax(s.ctx, oldTobinTaxes))

	// SyncTobinTax copies the caller's old target map before diffing.
	s.Require().Len(oldTobinTaxes, 2)
	s.Require().True(oldTobinTaxes[core.MicroKRWDenom].Equal(math.LegacyNewDecWithPrec(25, 4)))

	updatedTobinTax, err := s.keeper.GetTobinTax(s.ctx, core.MicroUSDDenom)
	s.Require().NoError(err)
	s.Require().True(math.LegacyNewDecWithPrec(5, 2).Equal(updatedTobinTax))

	addedTobinTax, err := s.keeper.GetTobinTax(s.ctx, core.MicroSDRDenom)
	s.Require().NoError(err)
	s.Require().True(math.LegacyNewDecWithPrec(25, 4).Equal(addedTobinTax))

	_, err = s.keeper.GetTobinTax(s.ctx, core.MicroKRWDenom)
	s.Require().Error(err)
	s.Require().ErrorContains(err, types.ErrUnknownDenom.Error())

	hasUSDExchangeRate, err := s.keeper.ExchangeRate.Has(s.ctx, core.MicroUSDDenom)
	s.Require().NoError(err)
	s.Require().True(hasUSDExchangeRate)

	hasKRWExchangeRate, err := s.keeper.ExchangeRate.Has(s.ctx, core.MicroKRWDenom)
	s.Require().NoError(err)
	s.Require().False(hasKRWExchangeRate)
}

func (s *KeeperTestSuite) TestAccountingCounters() {
	pubKey := ed25519.GenPrivKey().PubKey()
	validator, err := stakingtypes.NewValidator(valAddr1.String(), pubKey, stakingtypes.Description{})
	s.Require().NoError(err)
	consAddr, err := validator.GetConsAddr()
	s.Require().NoError(err)

	s.stakingKeeper.EXPECT().ValidatorByConsAddr(s.ctx, consAddr).Return(validator, nil).Times(4)

	s.Require().NoError(s.keeper.IncrementMissCount(s.ctx, consAddr))
	s.Require().NoError(s.keeper.IncrementMissCount(s.ctx, consAddr))
	missCount, err := s.keeper.MissCount.Get(s.ctx, valAddr1)
	s.Require().NoError(err)
	s.Require().Equal(uint64(2), missCount)

	s.Require().NoError(s.keeper.AddScoreWeight(s.ctx, consAddr, 3))
	s.Require().NoError(s.keeper.AddScoreWeight(s.ctx, consAddr, 4))
	scoreWeight, err := s.keeper.ScoreWeight.Get(s.ctx, valAddr1)
	s.Require().NoError(err)
	s.Require().Equal(uint64(7), scoreWeight)
}

func (s *KeeperTestSuite) TestAccountingCountersSkipUnresolvedConsensusAddress() {
	consAddr := sdk.ConsAddress([]byte("missing_validator___"))
	s.stakingKeeper.EXPECT().
		ValidatorByConsAddr(s.ctx, consAddr).
		Return(nil, stakingtypes.ErrNoValidatorFound).
		Times(2)

	s.Require().NoError(s.keeper.IncrementMissCount(s.ctx, consAddr))
	s.Require().NoError(s.keeper.AddScoreWeight(s.ctx, consAddr, 3))

	missCountEntries := 0
	err := s.keeper.MissCount.Walk(s.ctx, nil, func(_ sdk.ValAddress, _ uint64) (bool, error) {
		missCountEntries++
		return false, nil
	})
	s.Require().NoError(err)
	s.Require().Zero(missCountEntries)

	scoreWeightEntries := 0
	err = s.keeper.ScoreWeight.Walk(s.ctx, nil, func(_ sdk.ValAddress, _ uint64) (bool, error) {
		scoreWeightEntries++
		return false, nil
	})
	s.Require().NoError(err)
	s.Require().Zero(scoreWeightEntries)
}
