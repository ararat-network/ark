package ante_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/app/ante"
)

// gasOf runs a step under a fresh meter and returns what it consumed.
func gasOf(t *testing.T, ctx sdk.Context, run func(sdk.Context) error) storetypes.Gas {
	t.Helper()
	metered := ctx.WithGasMeter(storetypes.NewGasMeter(10_000_000))
	require.NoError(t, run(metered))
	return metered.GasMeter().GasConsumed()
}

// TestStabilityTaxSimulationMatchesExecutionGas pins the estimate: the
// collection takes no simulation branch, so it meters under simulation what
// it meters in block execution. Each run gets its own discarded cache, so
// the second does not read the balance the first wrote.
func TestStabilityTaxSimulationMatchesExecutionGas(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	decorator := ante.NewStabilityTaxDecorator(arkApp.TreasuryKeeper, arkApp.BankKeeper, arkApp.FeeGrantKeeper)
	reached := false
	run := func(simulate bool) storetypes.Gas {
		return gasOf(t, ctx, func(ctx sdk.Context) error {
			cached, _ := ctx.CacheContext()
			_, err := decorator.AnteHandle(cached, tx, simulate, passThrough(t, &reached))
			return err
		})
	}

	executed := run(false)
	require.Positive(t, executed)
	require.Equal(t, executed, run(true))
}

// TestGasTallyDecoratorTalliesUnderSimulation pins the estimate side of the
// tally: simulation consumes what the block-execution write does, and
// CheckTx nothing. Each run gets its own discarded cache, so none reads the
// value another wrote.
func TestGasTallyDecoratorTalliesUnderSimulation(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	decorator := ante.NewGasTallyDecorator(arkApp.TreasuryKeeper)
	reached := false
	run := func(mode sdk.ExecMode, simulate bool) storetypes.Gas {
		return gasOf(t, ctx.WithExecMode(mode), func(ctx sdk.Context) error {
			cached, _ := ctx.CacheContext()
			_, err := decorator.AnteHandle(cached, tx, simulate, passThrough(t, &reached))
			return err
		})
	}

	inBlock := run(sdk.ExecModeFinalize, false)
	require.Positive(t, inBlock)
	require.Equal(t, inBlock, run(sdk.ExecModeSimulate, true))
	require.Zero(t, run(sdk.ExecModeCheck, false))
}
