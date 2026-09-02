package ante_test

import (
	"testing"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	corestoretypes "cosmossdk.io/core/store"

	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/app"
)

// wasmTxCounterStore rebuilds the counter's store service from the registered
// Wasm store key, the way setupWasm hands it to the ante chain.
func wasmTxCounterStore(t *testing.T, arkApp *app.ArkApp) corestoretypes.KVStoreService {
	t.Helper()
	key := arkApp.GetKey(wasmtypes.StoreKey)
	require.NotNil(t, key, "the Wasm store must be registered")
	return runtime.NewKVStoreService(key)
}

// The per-block transaction counter is what makes an instantiated contract's
// address a function of its position in the block rather than of anything a
// caller supplies. It is the reason the ante chain is spelled out by hand, so
// this pins that the store backing it is actually wired and counts.
func TestWasmTxCounterIsWired(t *testing.T) {
	arkApp := app.Setup(t, false)
	counterStore := wasmTxCounterStore(t, arkApp)

	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{Height: 7})
	decorator := wasmkeeper.NewCountTXDecorator(counterStore)

	var seen []uint32
	terminator := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		counter, ok := wasmtypes.TXCounter(ctx)
		require.True(t, ok, "the counter must reach the transaction's context")
		seen = append(seen, counter)
		return ctx, nil
	}

	// Three transactions in one block count up from zero.
	for range 3 {
		_, err := decorator.AnteHandle(ctx, nil, false, terminator)
		require.NoError(t, err)
	}
	require.Equal(t, []uint32{0, 1, 2}, seen)

	// A new block restarts the count, so an address derived at position N is
	// reproducible from the block alone.
	nextBlock := arkApp.NewContextLegacy(false, cmtproto.Header{Height: 8})
	_, err := decorator.AnteHandle(nextBlock, nil, false, terminator)
	require.NoError(t, err)
	require.Equal(t, []uint32{0, 1, 2, 0}, seen)
}

// Simulation gets no counter, so a simulated instantiation cannot be mistaken
// for a real one at a real position.
func TestWasmTxCounterSkipsSimulation(t *testing.T) {
	arkApp := app.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{Height: 7})

	_, err := wasmkeeper.NewCountTXDecorator(wasmTxCounterStore(t, arkApp)).AnteHandle(
		ctx, nil, true,
		func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
			_, ok := wasmtypes.TXCounter(ctx)
			require.False(t, ok, "simulation must carry no counter")
			return ctx, nil
		},
	)
	require.NoError(t, err)
}
