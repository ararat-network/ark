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

	core "noah/types"
	oracletypes "noah/x/oracle/types"
	"noah/x/treasury/keeper"
	"noah/x/treasury/testutil"
	"noah/x/treasury/types"
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
	marketKeeper  *testutil.MockMarketKeeper
	oracleKeeper  *testutil.MockOracleKeeper
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
	s.marketKeeper = testutil.NewMockMarketKeeper(ctrl)
	s.oracleKeeper = testutil.NewMockOracleKeeper(ctrl)
	s.stakingKeeper = testutil.NewMockStakingKeeper(ctrl)

	// Required by NewKeeper's panic guard
	s.accountKeeper.EXPECT().GetModuleAddress(types.ModuleName).Return(sdk.AccAddress{1})

	s.keeper = keeper.NewKeeper(
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
	s.Require().NoError(s.keeper.Params.Set(s.ctx, types.DefaultParams()))
	s.Require().NoError(s.keeper.TaxRate.Set(s.ctx, types.DefaultTaxRate))
	s.Require().NoError(s.keeper.RewardWeight.Set(s.ctx, types.DefaultRewardWeight))
	s.Require().NoError(s.keeper.EpochTaxProceeds.Set(s.ctx, types.EpochTaxProceeds{}))
	s.Require().NoError(s.keeper.EpochInitialIssuance.Set(s.ctx, types.EpochInitialIssuance{
		Issuance: sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000000000000))),
	}))

	// Wire gRPC query client
	queryHelper := baseapp.NewQueryServerTestHelper(testCtx.Ctx, interfaceRegistry)
	types.RegisterQueryServer(queryHelper, keeper.NewQueryServerImpl(s.keeper))
	s.queryClient = types.NewQueryClient(queryHelper)

	// Create message server
	s.msgServer = keeper.NewMsgServerImpl(s.keeper)
}

// setBlockHeight is a helper to set the block height on the context.
func (s *KeeperTestSuite) setBlockHeight(h int64) {
	sdkCtx := sdk.UnwrapSDKContext(s.ctx)
	s.ctx = sdkCtx.WithBlockHeight(h)
}

func (s *KeeperTestSuite) TestRecordEpochTaxProceeds() {
	tests := []struct {
		name     string
		deltas   []sdk.Coins // applied sequentially
		expected sdk.Coins
	}{
		{
			name:     "single denom",
			deltas:   []sdk.Coins{sdk.NewCoins(sdk.NewCoin("uusd", math.NewInt(1000)))},
			expected: sdk.NewCoins(sdk.NewCoin("uusd", math.NewInt(1000))),
		},
		{
			name:     "empty delta is no-op",
			deltas:   []sdk.Coins{{}},
			expected: sdk.Coins{},
		},
		{
			name:     "nil delta is no-op",
			deltas:   []sdk.Coins{nil},
			expected: sdk.Coins{},
		},
		{
			name: "multi-denom",
			deltas: []sdk.Coins{sdk.NewCoins(
				sdk.NewCoin("uusd", math.NewInt(1000)),
				sdk.NewCoin("ukrw", math.NewInt(2000)),
			)},
			expected: sdk.NewCoins(
				sdk.NewCoin("uusd", math.NewInt(1000)),
				sdk.NewCoin("ukrw", math.NewInt(2000)),
			),
		},
		{
			name: "accumulates across calls",
			deltas: []sdk.Coins{
				sdk.NewCoins(sdk.NewCoin("uusd", math.NewInt(1000))),
				sdk.NewCoins(sdk.NewCoin("uusd", math.NewInt(500))),
			},
			expected: sdk.NewCoins(sdk.NewCoin("uusd", math.NewInt(1500))),
		},
		{
			name: "accumulates across calls multi-denom",
			deltas: []sdk.Coins{
				sdk.NewCoins(sdk.NewCoin("uusd", math.NewInt(1000))),
				sdk.NewCoins(sdk.NewCoin("ukrw", math.NewInt(2000))),
			},
			expected: sdk.NewCoins(
				sdk.NewCoin("uusd", math.NewInt(1000)),
				sdk.NewCoin("ukrw", math.NewInt(2000)),
			),
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			// Reset proceeds for each sub-test
			s.Require().NoError(s.keeper.EpochTaxProceeds.Set(s.ctx, types.EpochTaxProceeds{}))

			for _, delta := range tc.deltas {
				err := s.keeper.RecordEpochTaxProceeds(s.ctx, delta)
				s.Require().NoError(err)
			}

			proceeds, err := s.keeper.EpochTaxProceeds.Get(s.ctx)
			s.Require().NoError(err)
			if len(tc.expected) == 0 {
				s.Require().True(proceeds.TaxProceeds.IsZero())
			} else {
				s.Require().True(tc.expected.Equal(proceeds.TaxProceeds))
			}
		})
	}
}

func (s *KeeperTestSuite) TestComputeEpochSeigniorage() {
	tests := []struct {
		name           string
		initialSupply  math.Int
		currentSupply  math.Int
		expectedResult math.Int
	}{
		{
			name:           "positive seigniorage (supply burned)",
			initialSupply:  math.NewInt(1000),
			currentSupply:  math.NewInt(800),
			expectedResult: math.NewInt(200),
		},
		{
			name:           "supply increased — clamps to zero",
			initialSupply:  math.NewInt(1000),
			currentSupply:  math.NewInt(1200),
			expectedResult: math.ZeroInt(),
		},
		{
			name:           "no change — zero seigniorage",
			initialSupply:  math.NewInt(1000),
			currentSupply:  math.NewInt(1000),
			expectedResult: math.ZeroInt(),
		},
		{
			name:           "large values",
			initialSupply:  math.NewInt(1_000_000_000_000),
			currentSupply:  math.NewInt(999_999_000_000),
			expectedResult: math.NewInt(1_000_000),
		},
		{
			name:           "all supply burned",
			initialSupply:  math.NewInt(1000),
			currentSupply:  math.ZeroInt(),
			expectedResult: math.NewInt(1000),
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.Require().NoError(s.keeper.EpochInitialIssuance.Set(s.ctx, types.EpochInitialIssuance{
				Issuance: sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, tc.initialSupply)),
			}))

			s.bankKeeper.EXPECT().GetSupply(s.ctx, core.MicroArkDenom).
				Return(sdk.NewCoin(core.MicroArkDenom, tc.currentSupply))

			seigniorage, err := s.keeper.ComputeEpochSeigniorage(s.ctx)
			s.Require().NoError(err)
			s.Require().True(tc.expectedResult.Equal(seigniorage), "expected %s, got %s", tc.expectedResult, seigniorage)
		})
	}
}

func (s *KeeperTestSuite) TestRecordEpochInitialIssuance() {
	tests := []struct {
		name      string
		whitelist oracletypes.TobinTaxes
		supplies  map[string]math.Int
		expected  sdk.Coins
	}{
		{
			name:      "no whitelist denoms — only ark",
			whitelist: nil,
			supplies: map[string]math.Int{
				core.MicroArkDenom: math.NewInt(1_000_000),
			},
			expected: sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1_000_000))),
		},
		{
			name: "with whitelist denoms",
			whitelist: oracletypes.TobinTaxes{
				{Denom: "uusd"},
				{Denom: "ukrw"},
			},
			supplies: map[string]math.Int{
				core.MicroArkDenom: math.NewInt(1_000_000),
				"uusd":             math.NewInt(500_000),
				"ukrw":             math.NewInt(2_000_000),
			},
			expected: sdk.NewCoins(
				sdk.NewCoin(core.MicroArkDenom, math.NewInt(1_000_000)),
				sdk.NewCoin("uusd", math.NewInt(500_000)),
				sdk.NewCoin("ukrw", math.NewInt(2_000_000)),
			),
		},
		{
			name: "single whitelist denom",
			whitelist: oracletypes.TobinTaxes{
				{Denom: "uusd"},
			},
			supplies: map[string]math.Int{
				core.MicroArkDenom: math.NewInt(5_000_000),
				"uusd":             math.NewInt(100),
			},
			expected: sdk.NewCoins(
				sdk.NewCoin(core.MicroArkDenom, math.NewInt(5_000_000)),
				sdk.NewCoin("uusd", math.NewInt(100)),
			),
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.oracleKeeper.EXPECT().GetTobinTaxes(s.ctx).Return(tc.whitelist, nil)
			for denom, amount := range tc.supplies {
				s.bankKeeper.EXPECT().GetSupply(s.ctx, denom).
					Return(sdk.NewCoin(denom, amount))
			}

			err := s.keeper.RecordEpochInitialIssuance(s.ctx)
			s.Require().NoError(err)

			issuance, err := s.keeper.EpochInitialIssuance.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().True(tc.expected.Equal(issuance.Issuance))
		})
	}
}
