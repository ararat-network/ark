package ante_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/cosmos/cosmos-sdk/x/feegrant"

	"github.com/ararat-network/ark/app/ante"
	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
)

// TestFeeDecoratorChargesFeePayer pins the fixture: rate 0.1 on a 100 send
// is ten, declared within the fee of fifteen. The base fee of one reaches
// the fee collector from the ante, the tax its collector from the post, the
// four of slack stay with the payer, and each half names what it moved on
// its own tx event: the ante the fee and its payer, never the tax, since an
// ante event outlives a failed transaction, which pays none; the post the
// tax and its payer.
func TestFeeDecoratorChargesFeePayer(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)

	r := runFeeOnCache(t, arkApp, ctx, tx, false)
	require.NoError(t, r.err)
	require.Equal(t, "1ausd", r.gas)
	require.Equal(t, "10ausd", r.tax)
	require.Equal(t, math.NewInt(9), usdBalance(arkApp, r.cached, tx.payer))

	events := txEvents(r.handed)
	require.Len(t, events, 2)
	require.Equal(t, "1ausd", events[0][sdk.AttributeKeyFee])
	require.Equal(t, tx.payer.String(), events[0][sdk.AttributeKeyFeePayer])
	require.NotContains(t, events[0], ante.AttributeKeyTransferTax)
	require.Equal(t, "10ausd", events[1][ante.AttributeKeyTransferTax])
	require.Equal(t, tx.payer.String(), events[1][sdk.AttributeKeyFeePayer])
	require.NotContains(t, events[1], sdk.AttributeKeyFee)
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
	apptestutil.FundAccount(t, arkApp, ctx, tx.payer, sdk.NewCoins(sdk.NewInt64Coin(chain.XDRBaseDenom, 1)))
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
// ceiling — each drawn on the allowance as it is charged: the base fee by
// the ante, the tax by the post once the messages have succeeded, so the
// allowance records exactly what left the granter at every step. The payer
// is untouched, and each event names the granter.
func TestFeeDecoratorChargesGranterWhenSet(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	granter := sdk.AccAddress(bytes.Repeat([]byte{3}, 20))
	apptestutil.FundAccount(t, arkApp, ctx, granter, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 20)))
	require.NoError(t, arkApp.FeeGrantKeeper.GrantAllowance(ctx, granter, tx.payer, &feegrant.BasicAllowance{
		SpendLimit: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 20)),
	}))
	tx.granter = granter
	spendLimit := func(ctx sdk.Context) sdk.Coins {
		allowance, err := arkApp.FeeGrantKeeper.GetAllowance(ctx, granter, tx.payer)
		require.NoError(t, err)
		basic, ok := allowance.(*feegrant.BasicAllowance)
		require.True(t, ok)
		return basic.SpendLimit
	}

	r := runAnteOnCache(t, arkApp, ctx, tx)
	require.NoError(t, r.err)
	require.Equal(t, "1ausd", r.gas)
	require.Empty(t, r.tax)
	require.Equal(t, math.NewInt(19), usdBalance(arkApp, r.cached, granter))
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 19)), spendLimit(r.cached))

	_, err := runTax(t, arkApp, r.handed, tx, false, true)
	require.NoError(t, err)
	require.Equal(t, "10ausd", collected(arkApp, ctx, r.cached, taxCollector).String())
	require.Equal(t, math.NewInt(9), usdBalance(arkApp, r.cached, granter))
	require.Equal(t, math.NewInt(20), usdBalance(arkApp, r.cached, tx.payer))
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 9)), spendLimit(r.cached))
	for _, event := range txEvents(r.handed) {
		require.Equal(t, granter.String(), event[sdk.AttributeKeyFeePayer])
	}
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

// TestFeeDecoratorRefusesAGrantShortOfTheTax pins where a grant too small
// for both charges fails: not in the ante, which draws only the base fee it
// charges, but at the tax charge after the messages. The base fee stays
// drawn and charged, as it does for any transaction whose messages fail;
// nothing is taxed.
func TestFeeDecoratorRefusesAGrantShortOfTheTax(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	granter := sdk.AccAddress(bytes.Repeat([]byte{3}, 20))
	apptestutil.FundAccount(t, arkApp, ctx, granter, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 20)))
	require.NoError(t, arkApp.FeeGrantKeeper.GrantAllowance(ctx, granter, tx.payer, &feegrant.BasicAllowance{
		SpendLimit: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 5)),
	}))
	tx.granter = granter

	r := runFeeOnCache(t, arkApp, ctx, tx, false)
	require.ErrorIs(t, r.err, feegrant.ErrFeeLimitExceeded)
	require.ErrorContains(t, r.err, "does not allow to pay fees for")
	require.Equal(t, "1ausd", r.gas)
	require.Empty(t, r.tax)
	require.Equal(t, math.NewInt(19), usdBalance(arkApp, r.cached, granter))
	require.Equal(t, math.NewInt(20), usdBalance(arkApp, r.cached, tx.payer))

	allowance, err := arkApp.FeeGrantKeeper.GetAllowance(r.cached, granter, tx.payer)
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 4)), allowance.(*feegrant.BasicAllowance).SpendLimit)
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
