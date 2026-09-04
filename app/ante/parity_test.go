package ante_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/app/ante"
	chain "github.com/ararat-network/ark/pkg/chain"
)

// gasOf runs a step under a fresh meter and returns what it consumed.
func gasOf(t *testing.T, ctx sdk.Context, run func(sdk.Context) error) storetypes.Gas {
	t.Helper()
	metered := ctx.WithGasMeter(storetypes.NewGasMeter(10_000_000))
	require.NoError(t, run(metered))
	return metered.GasMeter().GasConsumed()
}

// TestFeeDecoratorSimulationMovesWhatExecutionMoves pins the estimate: a
// declared fee is deducted under simulation exactly as in execution — the
// tax set aside by the ante and charged by the post, the base fee to the fee
// collector, the slack left with the payer — and the gate's reads are made
// without being enforced, so the balances agree and so does the gas. Each
// run gets its own discarded cache.
func TestFeeDecoratorSimulationMovesWhatExecutionMoves(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	type outcome struct {
		gas                storetypes.Gas
		payer, fees, taxes math.Int
	}
	run := func(simulate bool) outcome {
		cached, _ := ctx.CacheContext()
		gas := gasOf(t, cached, func(ctx sdk.Context) error {
			_, err := runFee(t, arkApp, ctx, tx, simulate)
			return err
		})
		return outcome{
			gas:   gas,
			payer: usdBalance(arkApp, cached, tx.payer),
			fees:  usdBalance(arkApp, cached, feeCollector),
			taxes: usdBalance(arkApp, cached, taxCollector),
		}
	}

	executed, simulated := run(false), run(true)
	require.Equal(t, math.NewInt(9), executed.payer)
	require.Equal(t, math.NewInt(10), executed.taxes)
	require.Equal(t, executed.payer, simulated.payer)
	require.Equal(t, executed.fees, simulated.fees)
	require.Equal(t, executed.taxes, simulated.taxes)
	require.Equal(t, executed.gas, simulated.gas)
}

// TestFeeDecoratorFeelessSimulationPaysForTheTransfer pins the stand-in: an
// estimate without a fee moves no gas fee and consumes at least what the
// paying transaction's transfer costs in its place, so the estimate never
// runs short of the execution it sizes.
func TestFeeDecoratorFeelessSimulationPaysForTheTransfer(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	run := func(fee sdk.Coins, simulate bool) storetypes.Gas {
		tx.fee = fee
		cached, _ := ctx.CacheContext()
		return gasOf(t, cached, func(ctx sdk.Context) error {
			_, err := runFee(t, arkApp, ctx, tx, simulate)
			return err
		})
	}

	executed := run(sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 15)), false)
	feeless := run(nil, true)
	require.GreaterOrEqual(t, feeless, executed)
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
