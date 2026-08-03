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

// TestLiabilityValuationGasCoversTheMeteredRead holds liabilityValuationGas to
// the work it prices. cachedLiabilityValue charges the constant flat and then
// evaluates the lookup against a free meter, so nothing at runtime can notice
// the fee drifting away from the cost — the flat charge is by construction
// whatever the constant says. This test is what notices: it performs the same
// read against a real meter and compares.
//
// It measures loadLiabilityValuation because that call is the whole of the hit
// path's store work. A second read added there — the case the constant's
// comment warns costs about 2,015 — pushes the measurement past the fee and
// fails here. A read added to cachedLiabilityValue beside this call would not
// be seen, so keep the hit path's store access inside loadLiabilityValuation.
//
// The floor assertion is the opposite guard, and the one no reviewer would
// otherwise catch: transient stores are metered with the KV gas config only
// because OpenTransientStore routes through Context.KVStore. Were that to
// become the far cheaper transient config, every swap would keep paying 2,000
// for work costing tens of gas, and nothing would fail. The floor is deliberately
// loose — it exists to catch a change in the metering model, not to police
// headroom someone deliberately adds to the constant.
//
// The test reads the SDK's default KV gas config, which is what the app uses
// today. It cannot see an app-level override installed through baseapp, so a
// change there still needs the constant recalibrated by hand.
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
