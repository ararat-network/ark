package ante_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/cosmos/cosmos-sdk/x/feegrant"

	"github.com/ararat-network/ark/app/ante"
	chain "github.com/ararat-network/ark/pkg/chain"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

func TestStabilityTaxChargesFeePayer(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	collector := authtypes.NewModuleAddress(treasurytypes.StabilityTaxCollectorName)
	decorator := ante.NewStabilityTaxDecorator(arkApp.TreasuryKeeper, arkApp.BankKeeper, arkApp.FeeGrantKeeper)

	reached := false
	_, err := decorator.AnteHandle(ctx, tx, false, passThrough(t, &reached))
	require.NoError(t, err)
	require.True(t, reached)

	// Rate 0.1 on a 100 send: ten to the collector, ten left with the payer,
	// and the declared fee untouched — it is pure gas payment now.
	require.Equal(t, math.NewInt(10),
		arkApp.BankKeeper.GetBalance(ctx, collector, chain.USDBaseDenom).Amount)
	require.Equal(t, math.NewInt(10),
		arkApp.BankKeeper.GetBalance(ctx, tx.payer, chain.USDBaseDenom).Amount)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 15)), tx.GetFee())
}

// An unfunded payer fails simulation as it fails execution, so an estimate
// never succeeds where the transaction would not.
func TestStabilityTaxRefusesAnUnfundedPayer(t *testing.T) {
	for _, simulate := range []bool{false, true} {
		t.Run(fmt.Sprintf("simulate=%t", simulate), func(t *testing.T) {
			arkApp, ctx, tx := setupTreasuryAnteTest(t)
			decorator := ante.NewStabilityTaxDecorator(arkApp.TreasuryKeeper, arkApp.BankKeeper, arkApp.FeeGrantKeeper)

			tx.payer = sdk.AccAddress(bytes.Repeat([]byte{9}, 20))
			reached := false
			_, err := decorator.AnteHandle(ctx, tx, simulate, passThrough(t, &reached))
			require.ErrorContains(t, err, "collecting stability tax")
			require.False(t, reached)
		})
	}
}

// TestStabilityTaxWaivedAtGenesisHeight pins the gentx waiver: height zero
// moves nothing even with the policy in place, and only height zero — a
// missing policy past genesis is an error, not a waiver.
func TestStabilityTaxWaivedAtGenesisHeight(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	collector := authtypes.NewModuleAddress(treasurytypes.StabilityTaxCollectorName)
	decorator := ante.NewStabilityTaxDecorator(arkApp.TreasuryKeeper, arkApp.BankKeeper, arkApp.FeeGrantKeeper)

	reached := false
	_, err := decorator.AnteHandle(ctx.WithBlockHeight(0), tx, false, passThrough(t, &reached))
	require.NoError(t, err)
	require.True(t, reached)
	require.True(t,
		arkApp.BankKeeper.GetBalance(ctx, collector, chain.USDBaseDenom).IsZero())

	require.NoError(t, arkApp.TreasuryKeeper.MonetaryPolicy.Remove(ctx))
	reached = false
	_, err = decorator.AnteHandle(ctx, tx, false, passThrough(t, &reached))
	require.Error(t, err)
	require.False(t, reached)
}

// TestStabilityTaxAcceptsTransfersOfUncappedDenom proves the fee path has no
// cap-derivation dependency left: a denomination Treasury holds no cap for is
// untaxed and its transfer is accepted, so a stalled cap refresh can never
// reject an otherwise valid transaction.
func TestStabilityTaxAcceptsTransfersOfUncappedDenom(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	collector := authtypes.NewModuleAddress(treasurytypes.StabilityTaxCollectorName)
	decorator := ante.NewStabilityTaxDecorator(arkApp.TreasuryKeeper, arkApp.BankKeeper, arkApp.FeeGrantKeeper)
	require.NoError(t, arkApp.TreasuryKeeper.ConversionFactors.Remove(ctx, chain.USDBaseDenom))

	reached := false
	_, err := decorator.AnteHandle(ctx, tx, false, passThrough(t, &reached))
	require.NoError(t, err)
	require.True(t, reached)
	require.True(t,
		arkApp.BankKeeper.GetBalance(ctx, collector, chain.USDBaseDenom).IsZero())
}

// Funding a vesting account owes tax like the send it is — this was Terra
// Classic's untaxed transfer shape.
func TestStabilityTaxChargesVestingAccountFunding(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	collector := authtypes.NewModuleAddress(treasurytypes.StabilityTaxCollectorName)
	decorator := ante.NewStabilityTaxDecorator(arkApp.TreasuryKeeper, arkApp.BankKeeper, arkApp.FeeGrantKeeper)
	tx.msgs = []sdk.Msg{&vestingtypes.MsgCreateVestingAccount{
		FromAddress: sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String(),
		ToAddress:   sdk.AccAddress(bytes.Repeat([]byte{2}, 20)).String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100)),
		EndTime:     3600,
	}}

	_, err := decorator.AnteHandle(ctx, tx, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.NoError(t, err)
	require.Equal(t, math.NewInt(10),
		arkApp.BankKeeper.GetBalance(ctx, collector, chain.USDBaseDenom).Amount)
}

// A granter sponsors the tax on gas's terms: charged to the granter's
// account, decremented from the allowance, with the payer untouched.
func TestStabilityTaxChargesGranterWhenSet(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	collector := authtypes.NewModuleAddress(treasurytypes.StabilityTaxCollectorName)
	decorator := ante.NewStabilityTaxDecorator(arkApp.TreasuryKeeper, arkApp.BankKeeper, arkApp.FeeGrantKeeper)

	granter := sdk.AccAddress(bytes.Repeat([]byte{3}, 20))
	fundAccount(t, arkApp, ctx, granter, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 20)))
	require.NoError(t, arkApp.FeeGrantKeeper.GrantAllowance(ctx, granter, tx.payer, &feegrant.BasicAllowance{
		SpendLimit: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 15)),
	}))
	tx.granter = granter

	reached := false
	_, err := decorator.AnteHandle(ctx, tx, false, passThrough(t, &reached))
	require.NoError(t, err)
	require.True(t, reached)

	require.Equal(t, math.NewInt(10),
		arkApp.BankKeeper.GetBalance(ctx, collector, chain.USDBaseDenom).Amount)
	require.Equal(t, math.NewInt(10),
		arkApp.BankKeeper.GetBalance(ctx, granter, chain.USDBaseDenom).Amount)
	require.Equal(t, math.NewInt(20),
		arkApp.BankKeeper.GetBalance(ctx, tx.payer, chain.USDBaseDenom).Amount)

	allowance, err := arkApp.FeeGrantKeeper.GetAllowance(ctx, granter, tx.payer)
	require.NoError(t, err)
	basic, ok := allowance.(*feegrant.BasicAllowance)
	require.True(t, ok)
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 5)), basic.SpendLimit)
}

// Strict semantics: a grant that cannot cover the tax denom fails the
// transaction outright — no fallback charge to the payer.
func TestStabilityTaxStrictWhenGrantExcludesTaxDenom(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	collector := authtypes.NewModuleAddress(treasurytypes.StabilityTaxCollectorName)
	decorator := ante.NewStabilityTaxDecorator(arkApp.TreasuryKeeper, arkApp.BankKeeper, arkApp.FeeGrantKeeper)

	granter := sdk.AccAddress(bytes.Repeat([]byte{3}, 20))
	require.NoError(t, arkApp.FeeGrantKeeper.GrantAllowance(ctx, granter, tx.payer, &feegrant.BasicAllowance{
		SpendLimit: sdk.NewCoins(sdk.NewInt64Coin(chain.SDRBaseDenom, 1_000_000)),
	}))
	tx.granter = granter

	reached := false
	_, err := decorator.AnteHandle(ctx, tx, false, passThrough(t, &reached))
	require.ErrorContains(t, err, "does not allow to pay stability tax")
	require.False(t, reached)
	require.True(t,
		arkApp.BankKeeper.GetBalance(ctx, collector, chain.USDBaseDenom).IsZero())
	require.Equal(t, math.NewInt(20),
		arkApp.BankKeeper.GetBalance(ctx, tx.payer, chain.USDBaseDenom).Amount)
}

func TestStabilityTaxRefusesGranterWithoutAllowance(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	decorator := ante.NewStabilityTaxDecorator(arkApp.TreasuryKeeper, arkApp.BankKeeper, arkApp.FeeGrantKeeper)
	tx.granter = sdk.AccAddress(bytes.Repeat([]byte{3}, 20))

	reached := false
	_, err := decorator.AnteHandle(ctx, tx, false, passThrough(t, &reached))
	require.ErrorContains(t, err, "does not allow to pay stability tax")
	require.False(t, reached)
}

// A granter equal to the payer skips the allowance, mirroring
// checkDeductFee's branch.
func TestStabilityTaxSelfGranterNeedsNoAllowance(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	collector := authtypes.NewModuleAddress(treasurytypes.StabilityTaxCollectorName)
	decorator := ante.NewStabilityTaxDecorator(arkApp.TreasuryKeeper, arkApp.BankKeeper, arkApp.FeeGrantKeeper)
	tx.granter = tx.payer

	reached := false
	_, err := decorator.AnteHandle(ctx, tx, false, passThrough(t, &reached))
	require.NoError(t, err)
	require.True(t, reached)
	require.Equal(t, math.NewInt(10),
		arkApp.BankKeeper.GetBalance(ctx, collector, chain.USDBaseDenom).Amount)
}
