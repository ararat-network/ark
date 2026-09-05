package app_test

import (
	"testing"

	"github.com/CosmWasm/wasmd/x/wasm"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	dbm "github.com/cosmos/cosmos-db"
	ibcwasmtypes "github.com/cosmos/ibc-go/modules/light-clients/08-wasm/v11/types"
	icacontrollertypes "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/controller/types"
	icahosttypes "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/host/types"
	icatypes "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/types"
	packetforwardtypes "github.com/cosmos/ibc-go/v11/modules/apps/packet-forward-middleware/types"
	ratelimiting "github.com/cosmos/ibc-go/v11/modules/apps/rate-limiting"
	ratelimittypes "github.com/cosmos/ibc-go/v11/modules/apps/rate-limiting/types"
	ratelimitingv2 "github.com/cosmos/ibc-go/v11/modules/apps/rate-limiting/v2"
	ibctransfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	ibcexported "github.com/cosmos/ibc-go/v11/modules/core/exported"
	ibctm "github.com/cosmos/ibc-go/v11/modules/light-clients/07-tendermint"
	ibctesting "github.com/cosmos/ibc-go/v11/testing"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"

	"github.com/ararat-network/ark/app"
	apptestutil "github.com/ararat-network/ark/app/testutil"
)

var _ ibctesting.TestingApp = (*app.ArkApp)(nil)

func TestIBCWiring(t *testing.T) {
	arkApp := app.NewArkApp(
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

	// Rate limiting is outermost on both stacks, which is also what keeps the
	// callbacks middleware underneath it: a callback must not run for a packet
	// the limiter refused.
	transferRoute, ok := arkApp.IBCKeeper.PortKeeper.Route(ibctransfertypes.ModuleName)
	require.True(t, ok)
	require.IsType(t, &ratelimiting.IBCMiddleware{}, transferRoute)

	// Native contract channels, distinct from the callbacks path over transfer.
	wasmRoute, ok := arkApp.IBCKeeper.PortKeeper.Route(wasmtypes.ModuleName)
	require.True(t, ok)
	require.IsType(t, wasm.IBCHandler{}, wasmRoute)

	_, ok = arkApp.IBCKeeper.PortKeeper.Route(icacontrollertypes.SubModuleName)
	require.True(t, ok)
	_, ok = arkApp.IBCKeeper.PortKeeper.Route(icahosttypes.SubModuleName)
	require.True(t, ok)
	require.True(t, arkApp.IBCKeeper.PortKeeper.Router.Sealed())

	require.True(t, arkApp.IBCKeeper.ChannelKeeperV2.Router.HasRoute(ibctransfertypes.PortID))
	require.IsType(
		t,
		ratelimitingv2.IBCMiddleware{},
		arkApp.IBCKeeper.ChannelKeeperV2.Router.Route(ibctransfertypes.PortID),
	)
	// Contract v2 ports are per-contract, so any port under the prefix resolves.
	require.True(t, arkApp.IBCKeeper.ChannelKeeperV2.Router.HasRoute(
		wasmkeeper.PortIDPrefixV2+"1contractaddress",
	))

	for _, moduleName := range []string{
		ibcexported.ModuleName,
		ibctransfertypes.ModuleName,
		ratelimittypes.ModuleName,
		packetforwardtypes.ModuleName,
		icatypes.ModuleName,
		ibctm.ModuleName,
		ibcwasmtypes.ModuleName,
	} {
		require.Contains(t, arkApp.ModuleManager.Modules, moduleName)
	}

	requireOrderBefore(
		t,
		arkApp.ModuleManager.OrderInitGenesis,
		ibcexported.ModuleName,
		ibctransfertypes.ModuleName,
	)
	requireOrderBefore(
		t,
		arkApp.ModuleManager.OrderBeginBlockers,
		ibcexported.ModuleName,
		ratelimittypes.ModuleName,
	)
	require.NotContains(t, arkApp.ModuleManager.OrderEndBlockers, ibcexported.ModuleName)
}

// TestLightClientRoutes pins the client router: both wired light clients
// resolve, and an unwired client type is refused. Route reads the allowed
// clients param, so this runs over applied genesis rather than a bare app.
func TestLightClientRoutes(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContext(true)

	for _, clientID := range []string{"07-tendermint-0", "08-wasm-0"} {
		route, err := arkApp.IBCKeeper.ClientKeeper.Route(ctx, clientID)
		require.NoError(t, err, clientID)
		require.NotNil(t, route, clientID)
	}

	_, err := arkApp.IBCKeeper.ClientKeeper.Route(ctx, "06-solomachine-0")
	require.Error(t, err, "unwired client type must not route")
}
