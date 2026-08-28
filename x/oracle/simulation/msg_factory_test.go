package simulation_test

import (
	"context"
	"math/rand"
	"testing"
	"time"

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

	"github.com/ararat-network/ark/x/oracle/keeper"
	"github.com/ararat-network/ark/x/oracle/simulation"
	"github.com/ararat-network/ark/x/oracle/testutil"
	"github.com/ararat-network/ark/x/oracle/types"
)

func TestMsgUpdateParamsFactory(t *testing.T) {
	tests := []struct {
		name        string
		storeParams bool
		assert      func(t *testing.T, reporter simsx.SimulationReporter, msg *types.MsgUpdateParams, stored types.Params)
	}{
		{
			name:        "preserves staleness and participation policy",
			storeParams: true,
			assert: func(t *testing.T, reporter simsx.SimulationReporter, msg *types.MsgUpdateParams, stored types.Params) {
				require.False(t, reporter.IsSkipped())
				require.NotNil(t, msg)
				require.Equal(t, authtypes.NewModuleAddress(govtypes.ModuleName).String(), msg.Authority)
				// The factory randomises the windows and thresholds it owns and
				// carries the rest through untouched.
				require.Equal(t, stored.MaxExchangeRateAge, msg.Params.MaxExchangeRateAge)
				require.True(t, stored.ParticipationThreshold.Equal(msg.Params.ParticipationThreshold))
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
			stored.MaxExchangeRateAge = 2 * time.Minute
			stored.ParticipationThreshold = math.LegacyNewDecWithPrec(37, 2)
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
