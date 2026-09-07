package app_test

import (
	"testing"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"

	"github.com/ararat-network/ark/app"
)

// The contract runtime's cache counters register on whatever registry start
// hands over, and on nothing when none is handed over.
func TestWasmVMCacheMetricsFollowTheHandedRegistry(t *testing.T) {
	registry := prometheus.NewRegistry()
	appOpts := simtestutil.NewAppOptionsWithFlagHome(t.TempDir()).(simtestutil.AppOptionsMap)
	appOpts[app.WasmVMCacheMetricsRegistererOpt] = registry

	arkApp := app.NewArkApp(log.NewNopLogger(), dbm.NewMemDB(), true, appOpts)
	t.Cleanup(func() { require.NoError(t, arkApp.Close()) })

	families, err := registry.Gather()
	require.NoError(t, err)
	names := make([]string, 0, len(families))
	for _, family := range families {
		names = append(names, family.GetName())
	}
	require.Contains(t, names, "wasmvm_cache_misses_total")
	require.Contains(t, names, "wasmvm_cache_size_bytes")
}
