package keeper_test

import (
	gocontext "context"
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
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"noah/x/market/keeper"
	markettestutil "noah/x/market/testutil"
	"noah/x/market/types"
)

type KeeperTestSuite struct {
	suite.Suite

	ctx           sdk.Context
	goCtx         gocontext.Context
	marketKeeper  *keeper.Keeper
	msgServer     types.MsgServer
	queryClient   types.QueryClient
	accountKeeper *markettestutil.MockAccountKeeper
	bankKeeper    *markettestutil.MockBankKeeper
	oracleKeeper  *markettestutil.MockOracleKeeper
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
	testCtx := testutil.DefaultContextWithDB(s.T(), key, storetypes.NewTransientStoreKey("transient_test"))
	s.ctx = testCtx.Ctx
	s.goCtx = s.ctx

	ctrl := gomock.NewController(s.T())

	accountKeeper := markettestutil.NewMockAccountKeeper(ctrl)
	bankKeeper := markettestutil.NewMockBankKeeper(ctrl)
	oracleKeeper := markettestutil.NewMockOracleKeeper(ctrl)

	// Required by NewKeeper's panic guard
	accountKeeper.EXPECT().GetModuleAddress(types.ModuleName).Return(sdk.AccAddress{1})

	s.marketKeeper = keeper.NewKeeper(
		cdc,
		storeService,
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		accountKeeper,
		bankKeeper,
		oracleKeeper,
	)

	s.accountKeeper = accountKeeper
	s.bankKeeper = bankKeeper
	s.oracleKeeper = oracleKeeper

	// Set default state
	err := s.marketKeeper.Params.Set(s.ctx, types.DefaultParams())
	s.Require().NoError(err)
	err = s.marketKeeper.NoahPoolDelta.Set(s.ctx, math.LegacyZeroDec())
	s.Require().NoError(err)

	// Wire gRPC query client
	queryHelper := baseapp.NewQueryServerTestHelper(testCtx.Ctx, interfaceRegistry)
	types.RegisterQueryServer(queryHelper, keeper.NewQueryServerImpl(s.marketKeeper))
	s.queryClient = types.NewQueryClient(queryHelper)

	// Create message server
	s.msgServer = keeper.NewMsgServerImpl(s.marketKeeper)
}
