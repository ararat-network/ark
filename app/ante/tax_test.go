package ante_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/cosmos/cosmos-sdk/x/feegrant"

	chain "github.com/ararat-network/ark/pkg/chain"
)

// TestFeeDecoratorChargesFeePayer pins the fixture: rate 0.1 on a 100 send
// is ten, declared within the fee of fifteen. The tax reaches its collector,
// the base fee of one the fee collector, the four of slack stay with the
// payer, and the tx event names the charge, its payer, and the tax within
// it.
func TestFeeDecoratorChargesFeePayer(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)

	r := runFeeOnCache(t, arkApp, ctx, tx, false)
	require.NoError(t, r.err)
	require.Equal(t, "1ausd", r.gas)
	require.Equal(t, "10ausd", r.tax)
	require.Equal(t, math.NewInt(9), usdBalance(arkApp, r.cached, tx.payer))

	event := txEvent(t, r.handed)
	require.Equal(t, "11ausd", event[sdk.AttributeKeyFee])
	require.Equal(t, tx.payer.String(), event[sdk.AttributeKeyFeePayer])
	require.Equal(t, "10ausd", event["transfer_tax"])
}

// TestFeeDecoratorRefusesAnUnfundedPayer pins that an estimate never
// succeeds where the transaction would not: an unfunded payer fails
// simulation as it fails execution — at the gas deduction with the fixture
// fee, and at the tax charge when a simulation carries no fee at all.
func TestFeeDecoratorRefusesAnUnfundedPayer(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	tx.payer = sdk.AccAddress(bytes.Repeat([]byte{9}, 20))
	arkApp.AccountKeeper.SetAccount(ctx, arkApp.AccountKeeper.NewAccountWithAddress(ctx, tx.payer))

	for _, simulate := range []bool{false, true} {
		r := runFeeOnCache(t, arkApp, ctx, tx, simulate)
		require.ErrorContains(t, r.err, "insufficient funds")
		require.Empty(t, r.tax)
	}

	tx.fee = nil
	r := runFeeOnCache(t, arkApp, ctx, tx, true)
	require.ErrorContains(t, r.err, "collecting transfer tax")
}

// TestFeeDecoratorRefusesAnUnknownPayer mirrors the SDK: a payer without an
// account cannot pay, fee or tax.
func TestFeeDecoratorRefusesAnUnknownPayer(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	tx.payer = sdk.AccAddress(bytes.Repeat([]byte{9}, 20))

	r := runFeeOnCache(t, arkApp, ctx, tx, true)
	require.ErrorContains(t, r.err, "does not exist")
}

// TestFeeDecoratorAcceptsTransfersOfUncappedDenom proves the fee path has no
// cap-derivation dependency left: a denomination Treasury holds no cap for is
// untaxed and its transfer is accepted, so a stalled cap refresh can never
// reject an otherwise valid transaction. Gas rides the reference here, since
// the dropped factor prices ausd for neither.
func TestFeeDecoratorAcceptsTransfersOfUncappedDenom(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	require.NoError(t, arkApp.TreasuryKeeper.ConversionFactors.Remove(ctx, chain.USDBaseDenom))
	fundAccount(t, arkApp, ctx, tx.payer, sdk.NewCoins(sdk.NewInt64Coin(chain.XDRBaseDenom, 1)))
	tx.fee = sdk.NewCoins(sdk.NewInt64Coin(chain.XDRBaseDenom, 1))

	r := runFeeOnCache(t, arkApp, ctx, tx, false)
	require.NoError(t, r.err)
	require.Equal(t, "1axdr", r.gas)
	require.Empty(t, r.tax)
	require.Equal(t, math.NewInt(20), usdBalance(arkApp, r.cached, tx.payer))
}

// Funding a vesting account owes tax like the send it is — this was Terra
// Classic's untaxed transfer shape.
func TestFeeDecoratorChargesVestingAccountFunding(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	tx.msgs = []sdk.Msg{&vestingtypes.MsgCreateVestingAccount{
		FromAddress: sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String(),
		ToAddress:   sdk.AccAddress(bytes.Repeat([]byte{2}, 20)).String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100)),
		EndTime:     3600,
	}}

	r := runFeeOnCache(t, arkApp, ctx, tx, false)
	require.NoError(t, r.err)
	require.Equal(t, "10ausd", r.tax)
}

// A granter sponsors the base fee and tax together — the charge, not the
// ceiling: charged to the granter's account, decremented from the allowance
// in one draw, with the payer untouched and named as such on the event.
func TestFeeDecoratorChargesGranterWhenSet(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	granter := sdk.AccAddress(bytes.Repeat([]byte{3}, 20))
	fundAccount(t, arkApp, ctx, granter, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 20)))
	require.NoError(t, arkApp.FeeGrantKeeper.GrantAllowance(ctx, granter, tx.payer, &feegrant.BasicAllowance{
		SpendLimit: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 20)),
	}))
	tx.granter = granter

	r := runFeeOnCache(t, arkApp, ctx, tx, false)
	require.NoError(t, r.err)
	require.Equal(t, "1ausd", r.gas)
	require.Equal(t, "10ausd", r.tax)
	require.Equal(t, math.NewInt(9), usdBalance(arkApp, r.cached, granter))
	require.Equal(t, math.NewInt(20), usdBalance(arkApp, r.cached, tx.payer))
	require.Equal(t, granter.String(), txEvent(t, r.handed)[sdk.AttributeKeyFeePayer])

	allowance, err := arkApp.FeeGrantKeeper.GetAllowance(r.cached, granter, tx.payer)
	require.NoError(t, err)
	basic, ok := allowance.(*feegrant.BasicAllowance)
	require.True(t, ok)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 9)), basic.SpendLimit)
}

// Strict semantics: a grant that cannot cover the tax denom fails the
// transaction outright — no fallback charge to the payer.
func TestFeeDecoratorStrictWhenGrantExcludesTaxDenom(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	granter := sdk.AccAddress(bytes.Repeat([]byte{3}, 20))
	require.NoError(t, arkApp.FeeGrantKeeper.GrantAllowance(ctx, granter, tx.payer, &feegrant.BasicAllowance{
		SpendLimit: sdk.NewCoins(sdk.NewInt64Coin(chain.XDRBaseDenom, 1_000_000)),
	}))
	tx.granter = granter

	r := runFeeOnCache(t, arkApp, ctx, tx, false)
	require.ErrorContains(t, r.err, "does not allow to pay fees for")
	require.Empty(t, r.gas)
	require.Empty(t, r.tax)
	require.Equal(t, math.NewInt(20), usdBalance(arkApp, r.cached, tx.payer))
}

func TestFeeDecoratorRefusesGranterWithoutAllowance(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	tx.granter = sdk.AccAddress(bytes.Repeat([]byte{3}, 20))

	r := runFeeOnCache(t, arkApp, ctx, tx, false)
	require.ErrorContains(t, r.err, "does not allow to pay fees for")
	require.Empty(t, r.tax)
}

// A granter equal to the payer skips the allowance, as the SDK's deduction
// does.
func TestFeeDecoratorSelfGranterNeedsNoAllowance(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	tx.granter = tx.payer

	r := runFeeOnCache(t, arkApp, ctx, tx, false)
	require.NoError(t, r.err)
	require.Equal(t, "1ausd", r.gas)
	require.Equal(t, "10ausd", r.tax)
}
