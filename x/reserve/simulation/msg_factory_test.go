package simulation_test

import (
	"context"
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

	"github.com/ararat-network/ark/pkg/chain"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	"github.com/ararat-network/ark/x/reserve/keeper"
	"github.com/ararat-network/ark/x/reserve/simulation"
	"github.com/ararat-network/ark/x/reserve/testutil"
	"github.com/ararat-network/ark/x/reserve/types"
)

// govModuleAccounts is the only account source these factories need: the
// authority they sign as.
type govModuleAccounts struct{}

func (govModuleAccounts) GetModuleAddress(moduleName string) sdk.AccAddress {
	return authtypes.NewModuleAddress(moduleName)
}

type reserveFixture struct {
	ctx       sdk.Context
	keeper    *keeper.Keeper
	msgServer types.MsgServer
	testData  *simsx.ChainDataSource
	reporter  *simsx.BasicSimulationReporter
}

// newReserveFixture builds a real Reserve keeper over mocks, holding the given
// NOAH balance, so a message the factory emits can be delivered through the
// handler it targets.
func newReserveFixture(t *testing.T, balance int64) reserveFixture {
	t.Helper()

	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	types.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := sdktestutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("transient_test"))
	ctx := testCtx.Ctx.WithBlockHeight(10)

	ctrl := gomock.NewController(t)
	reserveAddr := authtypes.NewModuleAddress(types.StrategicReserveName)
	accountKeeper := testutil.NewMockAccountKeeper(ctrl)
	accountKeeper.EXPECT().GetAccount(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	accountKeeper.EXPECT().GetModuleAddress(types.StrategicReserveName).Return(reserveAddr).AnyTimes()
	bankKeeper := testutil.NewMockBankKeeper(ctrl)
	bankKeeper.EXPECT().
		GetBalance(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
			if !addr.Equals(reserveAddr) || denom != chain.NoahBaseDenom {
				return sdk.NewCoin(denom, math.ZeroInt())
			}
			return sdk.NewCoin(denom, math.NewInt(balance))
		}).
		AnyTimes()
	bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	bankKeeper.EXPECT().BurnCoins(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	oracleKeeper := testutil.NewMockOracleKeeper(ctrl)
	oracleKeeper.EXPECT().FeedPhase(gomock.Any(), gomock.Any()).Return(oracletypes.FeedPhaseActive, nil).AnyTimes()

	k := keeper.NewKeeper(
		cdc,
		runtime.NewKVStoreService(key),
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		accountKeeper,
		bankKeeper,
		oracleKeeper,
		testutil.NewMockAssetKeeper(ctrl),
	)
	require.NoError(t, k.Mandate.Set(ctx, types.DefaultReserveMandate()))
	require.NoError(t, k.AllowanceUsed.Set(ctx, math.ZeroInt()))

	r := rand.New(rand.NewSource(1))
	testData := simsx.NewChainDataSource(
		ctx,
		r,
		govModuleAccounts{},
		nil,
		addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix()),
		simtypes.RandomAccounts(r, 3)...,
	)

	return reserveFixture{
		ctx:       ctx,
		keeper:    k,
		msgServer: keeper.NewMsgServerImpl(k),
		testData:  testData,
		reporter:  simsx.NewBasicSimulationReporter(),
	}
}

func TestMsgSetReserveMandateFactory(t *testing.T) {
	f := newReserveFixture(t, 0)

	_, msg := simulation.MsgSetReserveMandateFactory()(f.ctx, f.testData, f.reporter)

	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	require.NotNil(t, msg)
	require.Equal(t, authtypes.NewModuleAddress(govtypes.ModuleName).String(), msg.Authority)
	require.NotEqual(t, msg.Authority, msg.Committee)
	require.Less(t, msg.ActivationHeight, msg.ExpiryHeight)
	require.True(t, msg.DeploymentAllowance.Amount.IsPositive())
	require.NotEmpty(t, msg.Destinations)
	// The factory's whole job is to emit what the handler accepts.
	_, err := f.msgServer.SetReserveMandate(f.ctx, msg)
	require.NoError(t, err)
}

func TestMsgSetRecognitionPolicyFactory(t *testing.T) {
	t.Run("skips without a standing policy", func(t *testing.T) {
		f := newReserveFixture(t, 0)

		_, msg := simulation.MsgSetRecognitionPolicyFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.True(t, f.reporter.IsSkipped())
		require.Contains(t, f.reporter.Comment(), "no standing recognition policy")
		require.Nil(t, msg)
	})

	t.Run("restates the standing policy", func(t *testing.T) {
		f := newReserveFixture(t, 0)
		standing := []string{"axau-lbma", "axag-lbma"}
		for _, denom := range standing {
			require.NoError(t, f.keeper.RecognitionPolicy.Set(f.ctx, denom, types.EligibilityEntry{
				Denom:               denom,
				HaircutFactor:       math.LegacyNewDecWithPrec(5, 1),
				RecognitionCapRatio: math.LegacyNewDecWithPrec(1, 1),
				MaxRateAge:          types.MaxRecognitionRateAge,
			}))
		}

		_, msg := simulation.MsgSetRecognitionPolicyFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.NotNil(t, msg)
		require.Len(t, msg.Entries, len(standing))
		require.NoError(t, types.ValidateRecognitionPolicy(msg.Entries))
		_, err := f.msgServer.SetRecognitionPolicy(f.ctx, msg)
		require.NoError(t, err)
	})
}

func TestFundAndBurnFactories(t *testing.T) {
	t.Run("skip when the reserve is empty", func(t *testing.T) {
		f := newReserveFixture(t, 0)

		_, buffer := simulation.MsgFundBufferFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.True(t, f.reporter.IsSkipped())
		require.Contains(t, f.reporter.Comment(), "retaining a floor")
		require.Nil(t, buffer)
	})

	t.Run("fund the buffer", func(t *testing.T) {
		f := newReserveFixture(t, 1_000_000)

		_, msg := simulation.MsgFundBufferFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.True(t, msg.Amount.Amount.Add(msg.MinimumReserveBalance.Amount).LTE(math.NewInt(1_000_000)))
		_, err := f.msgServer.FundBuffer(f.ctx, msg)
		require.NoError(t, err)
	})

	t.Run("fund insurance", func(t *testing.T) {
		f := newReserveFixture(t, 1_000_000)

		_, msg := simulation.MsgFundInsuranceFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		_, err := f.msgServer.FundInsurance(f.ctx, msg)
		require.NoError(t, err)
	})

	t.Run("burn reserve assets", func(t *testing.T) {
		f := newReserveFixture(t, 1_000_000)

		_, msg := simulation.MsgBurnReserveAssetsFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.False(t, msg.Amounts.Empty())
		_, err := f.msgServer.BurnReserveAssets(f.ctx, msg)
		require.NoError(t, err)
	})
}
