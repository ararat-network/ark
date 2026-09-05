package ante_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/app/ante"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// TestGasTallySkipsGenesisHeight pins the genesis gate on the block gas
// tally: a finalise-mode transaction at height zero leaves the tally
// untouched, and the same transaction one block later writes it.
func TestGasTallySkipsGenesisHeight(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	decorator := ante.NewGasTallyDecorator(arkApp.TreasuryKeeper)
	// Runtime names each module's transient key after the module.
	tally := ctx.TransientStore(arkApp.UnsafeFindStoreKey("transient:" + treasurytypes.ModuleName))
	empty := func() bool {
		it := tally.Iterator(nil, nil)
		defer it.Close()
		return !it.Valid()
	}
	require.True(t, empty())

	reached := false
	_, err := decorator.AnteHandle(
		ctx.WithBlockHeight(0).WithExecMode(sdk.ExecModeFinalize), tx, false, passThrough(t, &reached))
	require.NoError(t, err)
	require.True(t, reached)
	require.True(t, empty())

	_, err = decorator.AnteHandle(ctx.WithExecMode(sdk.ExecModeFinalize), tx, false, passThrough(t, &reached))
	require.NoError(t, err)
	require.False(t, empty())
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
