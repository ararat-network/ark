package testutil

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/x/treasury/keeper"
	"github.com/ararat-network/ark/x/treasury/types"
)

// SetDerivedTaxCap fixes the reference cap at one and stores the requested cap
// as its denomination factor, allowing independent per-denomination fixtures.
//
//nolint:revive // tb leads every test helper here, as it does in app.Setup.
func SetDerivedTaxCap(tb testing.TB, k *keeper.Keeper, ctx context.Context, denom string, amount math.Int) {
	tb.Helper()
	params, err := k.Params.Get(ctx)
	require.NoError(tb, err)
	params.ReferenceTaxCap = math.OneInt()
	require.NoError(tb, k.Params.Set(ctx, params))
	require.NoError(tb, k.ConversionFactors.Set(ctx, denom, types.ConversionFactor{
		Denom:  denom,
		Factor: math.LegacyNewDecFromInt(amount),
	}))
}
