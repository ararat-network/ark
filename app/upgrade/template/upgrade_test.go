package template_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"

	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/app/upgrade/template"
)

// TestBuildRunsMigrations pins the harness a real upgrade's test starts from:
// build the upgrade against a live app and run its handler at the version map
// the binary ships. With no migrations registered above it, the map comes
// back unchanged.
func TestBuildRunsMigrations(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{Height: arkApp.LastBlockHeight()})

	u := template.Build(arkApp.ModuleManager, arkApp.Configurator())
	require.Equal(t, template.PlanName, u.PlanName)

	fromVM := arkApp.ModuleManager.GetVersionMap()
	toVM, err := u.Handler(ctx, upgradetypes.Plan{Name: u.PlanName}, fromVM)
	require.NoError(t, err)
	require.Equal(t, fromVM, toVM)
}
