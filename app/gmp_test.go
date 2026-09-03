package app

import (
	"testing"

	gmp "github.com/cosmos/ibc-go/v11/modules/apps/27-gmp"
	gmptypes "github.com/cosmos/ibc-go/v11/modules/apps/27-gmp/types"
	ibcexported "github.com/cosmos/ibc-go/v11/modules/core/exported"
	"github.com/stretchr/testify/require"
)

func TestGMPWiring(t *testing.T) {
	arkApp := Setup(t, false)

	require.NotNil(t, arkApp.GMPKeeper)
	require.Contains(t, arkApp.ModuleManager.Modules, gmptypes.ModuleName)

	// GMP is a v2-only application and carries no middleware of its own.
	require.True(t, arkApp.IBCKeeper.ChannelKeeperV2.Router.HasRoute(gmptypes.PortID))
	require.IsType(
		t,
		&gmp.IBCModule{},
		arkApp.IBCKeeper.ChannelKeeperV2.Router.Route(gmptypes.PortID),
	)
	require.False(t, arkApp.IBCKeeper.PortKeeper.Router.HasRoute(gmptypes.ModuleName),
		"GMP has no Classic route")

	// It holds no module account: a derived account is an ordinary account, and
	// the module itself never takes custody.
	require.NotContains(t, arkApp.AccountKeeper.GetModulePermissions(), gmptypes.ModuleName)

	requireOrderBefore(
		t,
		arkApp.ModuleManager.OrderInitGenesis,
		ibcexported.ModuleName,
		gmptypes.ModuleName,
	)
}

// D48's substance: GMP and the contract runtime dispatch through one router, so
// a derived account is charged execution-generated tax on exactly the terms a
// contract is. This pins that they are the same construction rather than two
// that happen to agree today.
func TestGMPSharesTheExecutionPolicyRouter(t *testing.T) {
	arkApp := Setup(t, false)

	router := arkApp.executionPolicyRouter()
	require.Equal(t, arkApp.MsgServiceRouter(), router.inner)
	require.Equal(t, arkApp.TreasuryKeeper, router.treasury)
	require.Equal(t, arkApp.appCodec, router.cdc)
}
