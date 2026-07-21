package app

import (
	"testing"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"

	icacontrollertypes "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/controller/types"
	icahosttypes "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/host/types"
	icatypes "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/types"
	packetforwardtypes "github.com/cosmos/ibc-go/v11/modules/apps/packet-forward-middleware/types"
	ratelimiting "github.com/cosmos/ibc-go/v11/modules/apps/rate-limiting"
	ratelimittypes "github.com/cosmos/ibc-go/v11/modules/apps/rate-limiting/types"
	ratelimitingv2 "github.com/cosmos/ibc-go/v11/modules/apps/rate-limiting/v2"
	transfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	ibcexported "github.com/cosmos/ibc-go/v11/modules/core/exported"
	ibctm "github.com/cosmos/ibc-go/v11/modules/light-clients/07-tendermint"
	ibctesting "github.com/cosmos/ibc-go/v11/testing"
)

var _ ibctesting.TestingApp = (*ArkApp)(nil)

func TestIBCWiring(t *testing.T) {
	arkApp := NewArkApp(
		log.NewTestLogger(t),
		dbm.NewMemDB(),
		true,
		simtestutil.NewAppOptionsWithFlagHome(t.TempDir()),
	)

	require.NotNil(t, arkApp.IBCKeeper)
	require.NotNil(t, arkApp.TransferKeeper)
	require.NotNil(t, arkApp.RateLimitKeeper)
	require.NotNil(t, arkApp.PacketForwardKeeper)
	require.NotNil(t, arkApp.ICAControllerKeeper)
	require.NotNil(t, arkApp.ICAHostKeeper)

	transferRoute, ok := arkApp.IBCKeeper.PortKeeper.Route(transfertypes.ModuleName)
	require.True(t, ok)
	require.IsType(t, &ratelimiting.IBCMiddleware{}, transferRoute)

	_, ok = arkApp.IBCKeeper.PortKeeper.Route(icacontrollertypes.SubModuleName)
	require.True(t, ok)
	_, ok = arkApp.IBCKeeper.PortKeeper.Route(icahosttypes.SubModuleName)
	require.True(t, ok)
	require.True(t, arkApp.IBCKeeper.PortKeeper.Router.Sealed())

	require.True(t, arkApp.IBCKeeper.ChannelKeeperV2.Router.HasRoute(transfertypes.PortID))
	require.IsType(
		t,
		ratelimitingv2.IBCMiddleware{},
		arkApp.IBCKeeper.ChannelKeeperV2.Router.Route(transfertypes.PortID),
	)

	for _, moduleName := range []string{
		ibcexported.ModuleName,
		transfertypes.ModuleName,
		ratelimittypes.ModuleName,
		packetforwardtypes.ModuleName,
		icatypes.ModuleName,
		ibctm.ModuleName,
	} {
		require.Contains(t, arkApp.ModuleManager.Modules, moduleName)
	}
	require.NotContains(t, arkApp.ModuleManager.Modules, "08-wasm")

	requireOrderBefore(
		t,
		arkApp.ModuleManager.OrderInitGenesis,
		ibcexported.ModuleName,
		transfertypes.ModuleName,
	)
	requireOrderBefore(
		t,
		arkApp.ModuleManager.OrderBeginBlockers,
		ibcexported.ModuleName,
		ratelimittypes.ModuleName,
	)
	require.NotContains(t, arkApp.ModuleManager.OrderEndBlockers, ibcexported.ModuleName)
}

func TestIBCUsesStandardModuleGenesisDefaults(t *testing.T) {
	arkApp := NewArkApp(
		log.NewTestLogger(t),
		dbm.NewMemDB(),
		true,
		simtestutil.NewAppOptionsWithFlagHome(t.TempDir()),
	)

	actual := arkApp.DefaultGenesis()
	expected := IBCModuleBasics().DefaultGenesis(arkApp.AppCodec())
	require.NotEmpty(t, expected)

	for moduleName, expectedState := range expected {
		actualState, ok := actual[moduleName]
		require.True(t, ok, "missing %s default genesis", moduleName)
		if len(expectedState) == 0 {
			require.Empty(t, actualState, moduleName)
			continue
		}
		require.JSONEq(t, string(expectedState), string(actualState), moduleName)
	}
}
