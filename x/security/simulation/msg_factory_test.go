package simulation_test

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/cosmos/cosmos-sdk/baseapp"
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

	"github.com/ararat-network/ark/x/security/keeper"
	"github.com/ararat-network/ark/x/security/simulation"
	"github.com/ararat-network/ark/x/security/testutil"
	"github.com/ararat-network/ark/x/security/types"
)

// govModuleAccounts is the only account source the factory needs: the
// authority it signs as.
type govModuleAccounts struct{}

func (govModuleAccounts) GetModuleAddress(moduleName string) sdk.AccAddress {
	return authtypes.NewModuleAddress(moduleName)
}

// TestMsgSetSecurityMandateFactory pins the appointment the factory emits and
// delivers it through the handler it targets, which is the factory's whole job.
func TestMsgSetSecurityMandateFactory(t *testing.T) {
	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	types.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := sdktestutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("transient_test"))
	ctx := testCtx.Ctx.WithBlockHeight(10)

	ctrl := gomock.NewController(t)
	accountKeeper := testutil.NewMockAccountKeeper(ctrl)
	accountKeeper.EXPECT().GetAccount(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	k := keeper.NewKeeper(
		cdc,
		runtime.NewKVStoreService(key),
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		baseapp.NewMsgServiceRouter(),
		accountKeeper,
		testutil.NewMockUpgradeKeeper(ctrl),
	)
	require.NoError(t, k.Mandate.Set(ctx, types.DefaultSecurityMandate()))

	r := rand.New(rand.NewSource(1))
	testData := simsx.NewChainDataSource(
		ctx,
		r,
		govModuleAccounts{},
		nil,
		addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix()),
		simtypes.RandomAccounts(r, 2)...,
	)
	reporter := simsx.NewBasicSimulationReporter()

	_, msg := simulation.MsgSetSecurityMandateFactory()(ctx, testData, reporter)

	require.False(t, reporter.IsSkipped(), reporter.Comment())
	require.NotNil(t, msg)
	require.Equal(t, authtypes.NewModuleAddress(govtypes.ModuleName).String(), msg.Authority)
	require.NotEqual(t, msg.Authority, msg.Committee)
	require.Less(t, msg.ActivationHeight, msg.ExpiryHeight)
	// The handler refuses an expiry at or below the current height.
	require.Greater(t, msg.ExpiryHeight, uint64(ctx.BlockHeight()))

	_, err := keeper.NewMsgServerImpl(k).SetSecurityMandate(ctx, msg)
	require.NoError(t, err)
}
