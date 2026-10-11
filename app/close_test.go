package app_test

import (
	"runtime/debug"
	"testing"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"

	"github.com/ararat-network/ark/app"
)

// TestCloseReleasesTheWasmCache reopens an app on the home a closed one used. The contract VM locks
// its cache directory while it runs, so a second VM there fails unless Close released the first.
// Collection stays off: a finalised lock file would release the lock and hide a Close that did not.
func TestCloseReleasesTheWasmCache(t *testing.T) {
	defer debug.SetGCPercent(debug.SetGCPercent(-1))
	home := t.TempDir()
	first := app.NewArkApp(log.NewNopLogger(), dbm.NewMemDB(), true, simtestutil.NewAppOptionsWithFlagHome(home))
	require.NoError(t, first.Close())

	require.NotPanics(t, func() {
		second := app.NewArkApp(log.NewNopLogger(), dbm.NewMemDB(), true, simtestutil.NewAppOptionsWithFlagHome(home))
		require.NoError(t, second.Close())
	})
}
