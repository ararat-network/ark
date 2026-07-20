package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/core/store"
	"cosmossdk.io/math"

	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

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

	"ark/x/treasury/keeper"
	"ark/x/treasury/testutil"
	"ark/x/treasury/types"
)

type KeeperTestSuite struct {
	suite.Suite

	ctx                   context.Context
	cdc                   codec.Codec
	keeper                *keeper.Keeper
	msgServer             types.MsgServer
	queryClient           types.QueryClient
	authority             string
	accountKeeper         *testutil.MockAccountKeeper
	bankKeeper            *testutil.MockBankKeeper
	oracleKeeper          *testutil.MockOracleKeeper
	transientStoreService store.TransientStoreService
	commitMultiStore      storetypes.CommitMultiStore
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}

func (s *KeeperTestSuite) SetupTest() {
	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	types.RegisterInterfaces(interfaceRegistry)
	s.cdc = codec.NewProtoCodec(interfaceRegistry)

	key := storetypes.NewKVStoreKey(types.StoreKey)
	transientKey := storetypes.NewTransientStoreKey("transient_test")
	storeService := runtime.NewKVStoreService(key)
	transientStoreService := runtime.NewTransientStoreService(transientKey)
	testCtx := sdktestutil.DefaultContextWithDB(
		s.T(),
		key,
		transientKey,
	)
	s.ctx = testCtx.Ctx
	s.authority = authtypes.NewModuleAddress(govtypes.ModuleName).String()

	ctrl := gomock.NewController(s.T())
	s.accountKeeper = testutil.NewMockAccountKeeper(ctrl)
	s.bankKeeper = testutil.NewMockBankKeeper(ctrl)
	s.oracleKeeper = testutil.NewMockOracleKeeper(ctrl)
	s.transientStoreService = transientStoreService
	s.commitMultiStore = testCtx.CMS
	for _, moduleName := range types.FundAccountNames() {
		s.accountKeeper.EXPECT().
			GetModuleAddress(moduleName).
			Return(authtypes.NewModuleAddress(moduleName)).
			AnyTimes()
	}
	s.accountKeeper.EXPECT().
		GetModuleAddress(types.StabilityTaxCollectorName).
		Return(authtypes.NewModuleAddress(types.StabilityTaxCollectorName)).
		AnyTimes()

	s.keeper = keeper.NewKeeper(
		s.cdc,
		storeService,
		transientStoreService,
		s.authority,
		s.accountKeeper,
		s.bankKeeper,
		s.oracleKeeper,
	)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, types.DefaultParams()))
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, types.DefaultMonetaryPolicy()))
	s.Require().NoError(s.keeper.ClaimsMandate.Set(
		s.ctx,
		types.DefaultClaimsMandate(),
	))
	s.Require().NoError(s.keeper.ClaimsAllowanceUsed.Set(
		s.ctx,
		math.ZeroInt(),
	))
	s.Require().NoError(s.keeper.InsuranceReserved.Set(
		s.ctx,
		math.ZeroInt(),
	))
	s.Require().NoError(s.keeper.RewardFunding.Set(
		s.ctx,
		types.DefaultRewardFundingState(),
	))
	s.Require().NoError(s.keeper.MonetaryMandate.Set(
		s.ctx,
		types.DefaultMonetaryMandate(),
	))

	queryHelper := baseapp.NewQueryServerTestHelper(testCtx.Ctx, interfaceRegistry)
	types.RegisterQueryServer(queryHelper, keeper.NewQueryServerImpl(s.keeper))
	s.queryClient = types.NewQueryClient(queryHelper)
	s.msgServer = keeper.NewMsgServerImpl(s.keeper)
}

func (s *KeeperTestSuite) setBlockHeight(height int64) {
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(height)
}

func (s *KeeperTestSuite) clearTransientStore() {
	transientStore := s.transientStoreService.OpenTransientStore(s.ctx)
	iterator, err := transientStore.Iterator(nil, nil)
	s.Require().NoError(err)
	var keys [][]byte
	for ; iterator.Valid(); iterator.Next() {
		keys = append(keys, append([]byte(nil), iterator.Key()...))
	}
	s.Require().NoError(iterator.Close())
	for _, key := range keys {
		s.Require().NoError(transientStore.Delete(key))
	}
}

func (s *KeeperTestSuite) requireTypedEvent(expected proto.Message) {
	expectedEvent, err := sdk.TypedEventToEvent(expected)
	s.Require().NoError(err)
	events := sdk.UnwrapSDKContext(s.ctx).EventManager().Events()
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event.Type != expectedEvent.Type {
			continue
		}
		s.Require().Equal(expectedEvent, event)
		parsed, err := sdk.ParseTypedEvent(sdk.Events{event}.ToABCIEvents()[0])
		s.Require().NoError(err)
		roundTripEvent, err := sdk.TypedEventToEvent(parsed)
		s.Require().NoError(err)
		s.Require().Equal(expectedEvent, roundTripEvent)
		return
	}
	s.FailNow("typed event not found", expectedEvent.Type)
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
