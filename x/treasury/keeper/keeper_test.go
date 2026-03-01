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

	core "noah/types"
	"noah/x/treasury/keeper"
	treasurytestutil "noah/x/treasury/testutil"
	"noah/x/treasury/types"
)

type KeeperTestSuite struct {
	suite.Suite

	ctx             sdk.Context
	goCtx           gocontext.Context
	treasuryKeeper  *keeper.Keeper
	msgServer       types.MsgServer
	queryClient     types.QueryClient
	accountKeeper   *treasurytestutil.MockAccountKeeper
	bankKeeper      *treasurytestutil.MockBankKeeper
	distrKeeper     *treasurytestutil.MockDistributionKeeper
	marketKeeper    *treasurytestutil.MockMarketKeeper
	oracleKeeper    *treasurytestutil.MockOracleKeeper
	stakingKeeper   *treasurytestutil.MockStakingKeeper
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

	s.accountKeeper = treasurytestutil.NewMockAccountKeeper(ctrl)
	s.bankKeeper = treasurytestutil.NewMockBankKeeper(ctrl)
	s.distrKeeper = treasurytestutil.NewMockDistributionKeeper(ctrl)
	s.marketKeeper = treasurytestutil.NewMockMarketKeeper(ctrl)
	s.oracleKeeper = treasurytestutil.NewMockOracleKeeper(ctrl)
	s.stakingKeeper = treasurytestutil.NewMockStakingKeeper(ctrl)

	// Required by NewKeeper's panic guard
	s.accountKeeper.EXPECT().GetModuleAddress(types.ModuleName).Return(sdk.AccAddress{1})

	s.treasuryKeeper = keeper.NewKeeper(
		cdc,
		storeService,
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		"oracle",
		"distribution",
		s.accountKeeper,
		s.bankKeeper,
		s.distrKeeper,
		s.marketKeeper,
		s.oracleKeeper,
		s.stakingKeeper,
	)

	// Set default state
	s.Require().NoError(s.treasuryKeeper.Params.Set(s.ctx, types.DefaultParams()))
	s.Require().NoError(s.treasuryKeeper.TaxRate.Set(s.ctx, types.DefaultTaxRate))
	s.Require().NoError(s.treasuryKeeper.RewardWeight.Set(s.ctx, types.DefaultRewardWeight))
	s.Require().NoError(s.treasuryKeeper.EpochTaxProceeds.Set(s.ctx, types.EpochTaxProceeds{}))
	s.Require().NoError(s.treasuryKeeper.EpochInitialIssuance.Set(s.ctx, types.EpochInitialIssuance{
		Issuance: sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000000000000))),
	}))

	// Wire gRPC query client
	queryHelper := baseapp.NewQueryServerTestHelper(testCtx.Ctx, interfaceRegistry)
	types.RegisterQueryServer(queryHelper, keeper.NewQueryServerImpl(s.treasuryKeeper))
	s.queryClient = types.NewQueryClient(queryHelper)

	// Create message server
	s.msgServer = keeper.NewMsgServerImpl(s.treasuryKeeper)
}

// setBlockHeight is a helper to set the block height on the context.
func (s *KeeperTestSuite) setBlockHeight(h int64) {
	s.ctx = s.ctx.WithBlockHeight(h)
	s.goCtx = s.ctx
}

func (s *KeeperTestSuite) TestRecordEpochTaxProceeds() {
	// Record some tax proceeds
	delta := sdk.NewCoins(sdk.NewCoin("uusd", math.NewInt(1000)))
	err := s.treasuryKeeper.RecordEpochTaxProceeds(s.ctx, delta)
	s.Require().NoError(err)

	// Verify accumulation
	proceeds, err := s.treasuryKeeper.EpochTaxProceeds.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(1000), proceeds.TaxProceeds.AmountOf("uusd"))

	// Record more — should accumulate
	delta2 := sdk.NewCoins(sdk.NewCoin("uusd", math.NewInt(500)))
	err = s.treasuryKeeper.RecordEpochTaxProceeds(s.ctx, delta2)
	s.Require().NoError(err)

	proceeds, err = s.treasuryKeeper.EpochTaxProceeds.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(1500), proceeds.TaxProceeds.AmountOf("uusd"))
}

func (s *KeeperTestSuite) TestRecordEpochTaxProceeds_ZeroDelta() {
	// Zero delta should be a no-op
	err := s.treasuryKeeper.RecordEpochTaxProceeds(s.ctx, sdk.Coins{})
	s.Require().NoError(err)

	proceeds, err := s.treasuryKeeper.EpochTaxProceeds.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(proceeds.TaxProceeds.IsZero())
}

func (s *KeeperTestSuite) TestRecordEpochTaxProceeds_MultiDenom() {
	delta := sdk.NewCoins(
		sdk.NewCoin("uusd", math.NewInt(1000)),
		sdk.NewCoin("ukrw", math.NewInt(2000)),
	)
	err := s.treasuryKeeper.RecordEpochTaxProceeds(s.ctx, delta)
	s.Require().NoError(err)

	proceeds, err := s.treasuryKeeper.EpochTaxProceeds.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(1000), proceeds.TaxProceeds.AmountOf("uusd"))
	s.Require().Equal(math.NewInt(2000), proceeds.TaxProceeds.AmountOf("ukrw"))
}

func (s *KeeperTestSuite) TestComputeEpochSeigniorage() {
	// Set initial issuance to 1000 uark
	s.Require().NoError(s.treasuryKeeper.EpochInitialIssuance.Set(s.ctx, types.EpochInitialIssuance{
		Issuance: sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000))),
	}))

	// Current supply = 800 (burned 200)
	s.bankKeeper.EXPECT().GetSupply(s.ctx, core.MicroArkDenom).
		Return(sdk.NewCoin(core.MicroArkDenom, math.NewInt(800)))

	seigniorage, err := s.treasuryKeeper.ComputeEpochSeigniorage(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(200), seigniorage)
}

func (s *KeeperTestSuite) TestComputeEpochSeigniorage_Negative() {
	// Supply increased (no seigniorage)
	s.Require().NoError(s.treasuryKeeper.EpochInitialIssuance.Set(s.ctx, types.EpochInitialIssuance{
		Issuance: sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000))),
	}))

	s.bankKeeper.EXPECT().GetSupply(s.ctx, core.MicroArkDenom).
		Return(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1200)))

	seigniorage, err := s.treasuryKeeper.ComputeEpochSeigniorage(s.ctx)
	s.Require().NoError(err)
	s.Require().True(seigniorage.IsZero())
}

func (s *KeeperTestSuite) TestComputeEpochSeigniorage_NoChange() {
	s.Require().NoError(s.treasuryKeeper.EpochInitialIssuance.Set(s.ctx, types.EpochInitialIssuance{
		Issuance: sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000))),
	}))

	s.bankKeeper.EXPECT().GetSupply(s.ctx, core.MicroArkDenom).
		Return(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000)))

	seigniorage, err := s.treasuryKeeper.ComputeEpochSeigniorage(s.ctx)
	s.Require().NoError(err)
	s.Require().True(seigniorage.IsZero())
}
