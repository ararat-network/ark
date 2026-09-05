package ante_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
)

// A transaction that fails both the fee gate and the tax charge reports the
// fee error: the gate runs first, inside the fee decorator, and refuses
// without moving a balance. Charging first reported insufficient funds for
// a fee problem.
func TestFeeGateRefusalPrecedesTheTaxCharge(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)

	// A payer holding nothing: it can pay neither the fee nor the tax.
	tx.payer = sdk.AccAddress(bytes.Repeat([]byte{7}, 20))
	arkApp.AccountKeeper.SetAccount(ctx, arkApp.AccountKeeper.NewAccountWithAddress(ctx, tx.payer))

	// The fee declares the ten the send owes and one for gas, priced far
	// below the consensus floor, so the gate refuses it too.
	tx.gas = 1_000_000
	tx.fee = sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 11))

	r := runFeeOnCache(t, arkApp, ctx, tx, false)
	require.ErrorContains(t, r.err, "base fee requires")
	require.ErrorContains(t, r.err, "in addition to transfer tax 10ausd")
	require.NotContains(t, r.err.Error(), "collecting transfer tax")
	require.Empty(t, r.gas)
	require.Empty(t, r.tax)
}

// A fee short of the tax is refused ahead of any deduction, so a signer is
// never charged past their declaration: nothing moves, and the error names
// the tax rather than an insufficient balance.
func TestDeclaredTaxShortfallIsRefusedBeforeAnyCharge(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)

	// The send owes ten; the fee declares nine.
	tx.fee = sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 9))

	r := runFeeOnCache(t, arkApp, ctx, tx, false)
	require.ErrorContains(t, r.err, "does not cover transfer tax 10ausd")
	require.Empty(t, r.gas)
	require.Empty(t, r.tax)
	require.Equal(t, math.NewInt(20), usdBalance(arkApp, r.cached, tx.payer))
}
