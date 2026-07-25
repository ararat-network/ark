package simulation_test

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

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

	chain "ark/pkg/chain"
	"ark/x/oracle/keeper"
	"ark/x/oracle/simulation"
	"ark/x/oracle/testutil"
	"ark/x/oracle/types"
)

func TestMsgUpdateParamsFactory(t *testing.T) {
	tests := []struct {
		name        string
		storeParams bool
		assert      func(t *testing.T, reporter simsx.SimulationReporter, msg *types.MsgUpdateParams, stored types.Params)
	}{
		{
			name:        "preserves target and staleness policy",
			storeParams: true,
			assert: func(t *testing.T, reporter simsx.SimulationReporter, msg *types.MsgUpdateParams, stored types.Params) {
				require.False(t, reporter.IsSkipped())
				require.NotNil(t, msg)
				require.Equal(t, authtypes.NewModuleAddress(govtypes.ModuleName).String(), msg.Authority)
				require.Equal(t, stored.TobinTaxes, msg.Params.TobinTaxes)
				require.Equal(t, stored.MaxExchangeRateAge, msg.Params.MaxExchangeRateAge)
				require.NoError(t, msg.Params.Validate())
			},
		},
		{
			name: "skips when params are unavailable",
			assert: func(t *testing.T, reporter simsx.SimulationReporter, msg *types.MsgUpdateParams, _ types.Params) {
				require.True(t, reporter.IsSkipped())
				require.Nil(t, msg)
				require.Contains(t, reporter.Comment(), "get oracle params")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, oracleKeeper, accountKeeper := newOracleSimulationKeeper(t)
			stored := types.DefaultParams()
			stored.TobinTaxes = []types.TobinTax{
				{Denom: chain.SDRBaseDenom, TobinTax: types.DefaultTobinTax},
				{Denom: chain.USDBaseDenom, TobinTax: types.DefaultTobinTax},
			}
			stored.MaxExchangeRateAge = 2 * time.Minute
			if tt.storeParams {
				require.NoError(t, oracleKeeper.Params.Set(ctx, stored))
			}

			r := rand.New(rand.NewSource(1))
			accounts := simtypes.RandomAccounts(r, 1)
			testData := simsx.NewChainDataSource(
				ctx,
				r,
				accountKeeper,
				nil,
				addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix()),
				accounts...,
			)
			reporter := simsx.NewBasicSimulationReporter()

			_, msg := simulation.MsgUpdateParamsFactory(oracleKeeper)(ctx, testData, reporter)

			tt.assert(t, reporter, msg, stored)
		})
	}
}

func newOracleSimulationKeeper(t *testing.T) (context.Context, *keeper.Keeper, *testutil.MockAccountKeeper) {
	t.Helper()

	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	types.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)

	key := storetypes.NewKVStoreKey(types.StoreKey)
	storeService := runtime.NewKVStoreService(key)
	testCtx := sdktestutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("transient_test"))
	ctx := sdk.UnwrapSDKContext(testCtx.Ctx)

	ctrl := gomock.NewController(t)
	accountKeeper := testutil.NewMockAccountKeeper(ctrl)
	accountKeeper.EXPECT().GetModuleAddress(types.ModuleName).Return(sdk.AccAddress{1})
	accountKeeper.EXPECT().GetModuleAddress("distribution").Return(sdk.AccAddress{2})
	accountKeeper.EXPECT().GetModuleAddress(govtypes.ModuleName).Return(authtypes.NewModuleAddress(govtypes.ModuleName)).AnyTimes()

	oracleKeeper := keeper.NewKeeper(
		cdc,
		storeService,
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		"distribution",
		accountKeeper,
		nil,
		nil,
		nil,
	)

	return ctx, oracleKeeper, accountKeeper
}
