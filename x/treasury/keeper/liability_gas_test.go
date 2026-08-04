package keeper

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/x/treasury/types"
)

// TestLiabilityValuationGasCoversTheMeteredRead holds liabilityValuationGas
// to the work it prices: cachedLiabilityValue charges the flat constant and
// evaluates the lookup against a free meter, so this test is the only thing
// that notices the fee drifting from the cost. It measures
// loadLiabilityValuation — the whole of the hit path's store work — so keep
// that path's store access inside it. The deliberately loose floor assertion
// catches the metering model changing (transient stores are metered with the
// KV gas config only because OpenTransientStore routes through
// Context.KVStore), not headroom added to the constant.
func TestLiabilityValuationGasCoversTheMeteredRead(t *testing.T) {
	transientKey := storetypes.NewTransientStoreKey("transient_test")
	testCtx := sdktestutil.DefaultContextWithDB(
		t,
		storetypes.NewKVStoreKey(types.StoreKey),
		transientKey,
	)
	// The snapshot helpers touch nothing but the transient store, so the keeper
	// needs nothing else wired to answer for its own gas.
	k := Keeper{transientStoreService: runtime.NewTransientStoreService(transientKey)}

	testCases := []struct {
		name      string
		liability math.LegacyDec
	}{
		{
			name: "typical aggregate",
			// A million NOAH of liability per member across a full registry, in
			// base units: the magnitude the chain actually stores.
			liability: math.LegacyNewDecFromInt(math.NewIntWithDecimal(1, 26)),
		},
		{
			name:      "largest storable",
			liability: largestStorableLiability(),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require.True(t, testCase.liability.IsInValidRange())
			require.NoError(t, k.storeLiabilitySnapshot(testCtx.Ctx, testCase.liability, true))

			meter := storetypes.NewInfiniteGasMeter()
			metered := sdk.UnwrapSDKContext(testCtx.Ctx).WithGasMeter(meter)
			stored, complete, found, err := k.loadLiabilityValuation(metered)
			require.NoError(t, err)
			require.True(t, found)
			require.True(t, complete)
			require.True(t, stored.Equal(testCase.liability))

			consumed := meter.GasConsumed()
			t.Logf("read consumed %d gas against a %d fee", consumed, liabilityValuationGas)
			require.LessOrEqual(
				t,
				consumed,
				uint64(liabilityValuationGas),
				"the flat fee no longer covers the read it prices",
			)
			require.Greater(
				t,
				consumed,
				uint64(liabilityValuationGas/4),
				"the read became far cheaper than its fee; recalibrate downward",
			)
		})
	}
}

// largestStorableLiability returns the largest value storeLiabilitySnapshot
// accepts, which is math's own upper limit of 2^256 * 10^18 - 1 in raw units.
// The marshalled Dec is that integer's decimal text, so this is also the
// longest value the snapshot key can hold and therefore the most expensive
// read the flat fee has to cover.
func largestStorableLiability() math.LegacyDec {
	raw := new(big.Int).Exp(big.NewInt(2), big.NewInt(256), nil)
	raw.Mul(raw, new(big.Int).Exp(big.NewInt(10), big.NewInt(math.LegacyPrecision), nil))
	raw.Sub(raw, big.NewInt(1))

	return math.LegacyNewDecFromBigIntWithPrec(raw, math.LegacyPrecision)
}
