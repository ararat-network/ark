package simulation_test

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	codectestutil "github.com/cosmos/cosmos-sdk/codec/testutil"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/std"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/ararat-network/ark/x/asset/keeper"
	"github.com/ararat-network/ark/x/asset/simulation"
	"github.com/ararat-network/ark/x/asset/testutil"
	"github.com/ararat-network/ark/x/asset/types"
)

// govModuleAccounts is the only account source the governance factories need:
// the authority they sign as.
type govModuleAccounts struct{}

func (govModuleAccounts) GetModuleAddress(moduleName string) sdk.AccAddress {
	return authtypes.NewModuleAddress(moduleName)
}

type assetFixture struct {
	ctx      sdk.Context
	keeper   *keeper.Keeper
	bank     *testutil.MockBankKeeper
	testData *simsx.ChainDataSource
	reporter *simsx.BasicSimulationReporter
}

// newAssetFixture builds a real Asset keeper over mocks at height 10, so a
// message the factory emits can be delivered through the handler it targets.
func newAssetFixture(t *testing.T) assetFixture {
	t.Helper()

	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	types.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := sdktestutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("transient_test"))
	ctx := testCtx.Ctx.WithBlockHeight(10)

	ctrl := gomock.NewController(t)
	bank := testutil.NewMockBankKeeper(ctrl)
	k := keeper.NewKeeper(
		cdc,
		runtime.NewKVStoreService(key),
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		testutil.NewMockAccountKeeper(ctrl),
		bank,
		testutil.NewMockOracleKeeper(ctrl),
	)
	require.NoError(t, k.Params.Set(ctx, types.DefaultParams()))

	r := rand.New(rand.NewSource(1))
	testData := simsx.NewChainDataSource(
		ctx,
		r,
		govModuleAccounts{},
		nil,
		addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix()),
		simtypes.RandomAccounts(r, 1)...,
	)

	return assetFixture{ctx: ctx, keeper: k, bank: bank, testData: testData, reporter: simsx.NewBasicSimulationReporter()}
}

// seed stores a launch asset in the given status carrying the given supply.
func (f assetFixture) seed(t *testing.T, index int, status types.AssetStatus, supply int64) types.Asset {
	t.Helper()
	asset := types.DefaultGenesisState().Assets[index]
	asset.Status = status
	asset.Version = 1
	require.NoError(t, f.keeper.Assets.Set(f.ctx, asset.Denom, asset))
	f.bank.EXPECT().GetSupply(gomock.Any(), asset.Denom).Return(sdk.NewInt64Coin(asset.Denom, supply)).AnyTimes()

	return asset
}

func TestMsgOpenSettlementFactory(t *testing.T) {
	tests := []struct {
		name   string
		status types.AssetStatus
		supply int64
		skip   string
	}{
		{name: "active asset is not settleable", status: types.AssetStatus_ASSET_STATUS_ACTIVE, supply: 5, skip: "no asset may open settlement"},
		{name: "suspended without supply is not settleable", status: types.AssetStatus_ASSET_STATUS_SUSPENDED, supply: 0, skip: "no asset may open settlement"},
		{name: "suspended with supply opens", status: types.AssetStatus_ASSET_STATUS_SUSPENDED, supply: 3},
		{name: "written off with supply opens", status: types.AssetStatus_ASSET_STATUS_WRITTEN_OFF, supply: 9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newAssetFixture(t)
			asset := f.seed(t, 0, tt.status, tt.supply)

			_, msg := simulation.MsgOpenSettlementFactory(f.keeper)(f.ctx, f.testData, f.reporter)

			if tt.skip != "" {
				require.True(t, f.reporter.IsSkipped())
				require.Contains(t, f.reporter.Comment(), tt.skip)
				require.Nil(t, msg)

				return
			}
			require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
			require.NotNil(t, msg)
			require.Equal(t, authtypes.NewModuleAddress(govtypes.ModuleName).String(), msg.Authority)
			require.Equal(t, asset.Denom, msg.Denom)
			require.Equal(t, asset.Version, msg.ExpectedVersion)
			require.True(t, msg.RedemptionRate.IsPositive())
			require.True(t, msg.RedemptionRate.LTE(math.LegacyOneDec()))
			params, err := f.keeper.Params.Get(f.ctx)
			require.NoError(t, err)
			require.Greater(t, msg.EarliestClosingHeight, 10+int64(params.SettlementActivationDelayBlocks))
			// The factory's whole job is to emit what the handler accepts.
			require.NoError(t, f.keeper.OpenSettlement(
				f.ctx, msg.Denom, msg.ExpectedVersion, msg.RedemptionRate, msg.EarliestClosingHeight,
			))
		})
	}
}

func TestMsgFinaliseRetirementFactory(t *testing.T) {
	tests := []struct {
		name   string
		status types.AssetStatus
		supply int64
		skip   string
		// minBound is the floor the emitted bound must respect.
		minBound int64
	}{
		{name: "active asset cannot retire", status: types.AssetStatus_ASSET_STATUS_ACTIVE, supply: 5, skip: "no asset may finalise retirement"},
		{name: "suspended with supply cannot retire", status: types.AssetStatus_ASSET_STATUS_SUSPENDED, supply: 3, skip: "no asset may finalise retirement"},
		{name: "halted issuance approves at least its supply", status: types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED, supply: 7, minBound: 7},
		{name: "suspended without supply approves zero", status: types.AssetStatus_ASSET_STATUS_SUSPENDED, supply: 0},
		{name: "written off approves zero", status: types.AssetStatus_ASSET_STATUS_WRITTEN_OFF, supply: 9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newAssetFixture(t)
			asset := f.seed(t, 0, tt.status, tt.supply)

			_, msg := simulation.MsgFinaliseRetirementFactory(f.keeper)(f.ctx, f.testData, f.reporter)

			if tt.skip != "" {
				require.True(t, f.reporter.IsSkipped())
				require.Contains(t, f.reporter.Comment(), tt.skip)
				require.Nil(t, msg)

				return
			}
			require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
			require.NotNil(t, msg)
			require.Equal(t, asset.Denom, msg.Denom)
			require.Equal(t, asset.Version, msg.ExpectedVersion)
			require.True(t, msg.MaxResidualSupply.GTE(math.NewInt(tt.minBound)))
			if tt.status != types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED {
				require.True(t, msg.MaxResidualSupply.IsZero(), "only halted issuance may approve a residual")
			}
			require.NoError(t, f.keeper.FinaliseRetirement(
				f.ctx, msg.Denom, msg.ExpectedVersion, msg.MaxResidualSupply,
			))
		})
	}
}
