package ante_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/feegrant"

	"github.com/ararat-network/ark/app/ante"
	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
)

func TestTransferTaxDecoratorExecutionModes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mode     sdk.ExecMode
		simulate bool
		collect  bool
	}{
		{name: "check", mode: sdk.ExecModeCheck},
		{name: "recheck", mode: sdk.ExecModeReCheck},
		{name: "prepare proposal", mode: sdk.ExecModePrepareProposal},
		{name: "process proposal", mode: sdk.ExecModeProcessProposal},
		{name: "finalise", mode: sdk.ExecModeFinalize, collect: true},
		{name: "simulate", mode: sdk.ExecModeSimulate, simulate: true, collect: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, sponsored := range []bool{false, true} {
				name := "payer"
				if sponsored {
					name = "granter"
				}
				t.Run(name, func(t *testing.T) {
					arkApp, ctx, tx := setupTreasuryAnteTest(t)
					charged := tx.payer
					if sponsored {
						charged = sdk.AccAddress(bytes.Repeat([]byte{3}, 20))
						tx.granter = charged
						apptestutil.FundAccount(t, arkApp, ctx, charged, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 20)))
						require.NoError(t, arkApp.FeeGrantKeeper.GrantAllowance(ctx, charged, tx.payer, &feegrant.BasicAllowance{
							SpendLimit: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 20)),
						}))
					}
					cached, _ := ctx.WithExecMode(tc.mode).CacheContext()
					cached = cached.WithGasMeter(storetypes.NewInfiniteGasMeter())
					tax := sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 7))
					handed, err := runTax(t, arkApp, ante.WithTransferTax(cached, tax), tx, tc.simulate, true)
					require.NoError(t, err)
					require.NotZero(t, handed)
					if tc.collect {
						require.Positive(t, cached.GasMeter().GasConsumed())
						require.Equal(t, "7ausd", collected(arkApp, ctx, cached, taxCollector).String())
						require.Equal(t, math.NewInt(13), usdBalance(arkApp, cached, charged))
					} else {
						require.Zero(t, cached.GasMeter().GasConsumed(), "pre-execution checks must not touch tax state")
						require.Empty(t, txEvents(handed))
						require.Empty(t, collected(arkApp, ctx, cached, taxCollector))
						require.Equal(t, math.NewInt(20), usdBalance(arkApp, cached, charged))
					}
					if sponsored {
						allowance, err := arkApp.FeeGrantKeeper.GetAllowance(cached, charged, tx.payer)
						require.NoError(t, err)
						remaining := int64(20)
						if tc.collect {
							remaining = 13
						}
						require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, remaining)), allowance.(*feegrant.BasicAllowance).SpendLimit)
					}

					// Without an ante-provided figure, execution must still fail
					// closed. Pre-execution modes must not inspect that figure.
					_, err = runTax(t, arkApp, ctx.WithExecMode(tc.mode), tx, tc.simulate, true)
					if tc.collect {
						require.ErrorContains(t, err, "no transfer tax on the context")
					} else {
						require.NoError(t, err)
					}

					unaffordable, _ := ctx.WithExecMode(tc.mode).CacheContext()
					unaffordable = ante.WithTransferTax(unaffordable, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 21)))
					_, err = runTax(t, arkApp, unaffordable, tx, tc.simulate, true)
					if tc.collect {
						require.Error(t, err, "execution and simulation must enforce tax payment")
					} else {
						require.NoError(t, err, "pre-execution checks must allow messages to fund the tax")
					}
				})
			}
		})
	}
}

// TestTransferTaxDecoratorChargesNothingOnFailure pins D82 at the seam: a
// transaction whose messages failed reaches the post decorator with success
// false and is not taxed — the gas fee the ante moved is all that moved,
// and a granter's allowance has been drawn for the gas fee alone. BaseApp
// discards the post's branch on failure anyway; this pins that the decorator
// itself charges nothing, so the outcome does not rest on the discard.
func TestTransferTaxDecoratorChargesNothingOnFailure(t *testing.T) {
	t.Run("payer", func(t *testing.T) {
		arkApp, ctx, tx := setupTreasuryAnteTest(t)
		r := runAnteOnCache(t, arkApp, ctx, tx)
		require.NoError(t, r.err)
		require.Equal(t, "1ausd", r.gas)

		handed, err := runTax(t, arkApp, r.handed, tx, false, false)
		require.NoError(t, err)
		require.NotZero(t, handed, "the decorator must hand a failed transaction on")
		require.Empty(t, collected(arkApp, ctx, r.cached, taxCollector))
		require.Equal(t, math.NewInt(19), usdBalance(arkApp, r.cached, tx.payer))
		require.Len(t, txEvents(r.handed), 1)
	})

	t.Run("granter", func(t *testing.T) {
		arkApp, ctx, tx := setupTreasuryAnteTest(t)
		granter := sdk.AccAddress(bytes.Repeat([]byte{3}, 20))
		apptestutil.FundAccount(t, arkApp, ctx, granter, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 20)))
		require.NoError(t, arkApp.FeeGrantKeeper.GrantAllowance(ctx, granter, tx.payer, &feegrant.BasicAllowance{
			SpendLimit: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 20)),
		}))
		tx.granter = granter
		r := runAnteOnCache(t, arkApp, ctx, tx)
		require.NoError(t, r.err)

		_, err := runTax(t, arkApp, r.handed, tx, false, false)
		require.NoError(t, err)
		require.Empty(t, collected(arkApp, ctx, r.cached, taxCollector))
		require.Equal(t, math.NewInt(19), usdBalance(arkApp, r.cached, granter))
		allowance, err := arkApp.FeeGrantKeeper.GetAllowance(r.cached, granter, tx.payer)
		require.NoError(t, err)
		require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 19)), allowance.(*feegrant.BasicAllowance).SpendLimit)
	})
}

// TestTransferTaxDecoratorFailsADrainedPayer pins the other side of charging
// after the messages: a payer the messages left short of the tax fails the
// transaction at the charge. The gas fee the ante moved stays moved, as
// BaseApp keeps the ante's branch, and nothing reaches the tax collector.
func TestTransferTaxDecoratorFailsADrainedPayer(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	r := runAnteOnCache(t, arkApp, ctx, tx)
	require.NoError(t, r.err)
	require.Equal(t, math.NewInt(19), usdBalance(arkApp, r.cached, tx.payer))

	// The messages spend the payer down to nine, one short of the tax.
	elsewhere := sdk.AccAddress(bytes.Repeat([]byte{7}, 20))
	require.NoError(t, arkApp.BankKeeper.SendCoins(r.cached, tx.payer, elsewhere, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 10))))

	_, err := runTax(t, arkApp, r.handed, tx, false, true)
	require.ErrorIs(t, err, errortypes.ErrInsufficientFunds)
	require.ErrorContains(t, err, "collecting transfer tax 10ausd")
	require.Empty(t, collected(arkApp, ctx, r.cached, taxCollector))
	require.Equal(t, "1ausd", collected(arkApp, ctx, r.cached, feeCollector).String())
}

// TestTransferTaxDecoratorChargesTheFigureItIsHanded pins the seam between
// the halves: the post charges the tax on the context and nothing else. A
// figure the ante priced as nothing — genesis, or messages that owe none —
// is handed on without an event; a context carrying no figure at all is the
// post chain running without the ante, and fails the transaction rather than
// letting it through untaxed.
func TestTransferTaxDecoratorChargesTheFigureItIsHanded(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)

	handed, err := runTax(t, arkApp, ante.WithTransferTax(ctx, nil), tx, false, true)
	require.NoError(t, err)
	require.NotZero(t, handed)
	require.Empty(t, txEvents(handed))
	require.Equal(t, math.NewInt(20), usdBalance(arkApp, ctx, tx.payer))

	handed, err = runTax(t, arkApp, ante.WithTransferTax(ctx, sdk.NewCoins()), tx, false, true)
	require.NoError(t, err)
	require.NotZero(t, handed)
	require.Empty(t, txEvents(handed))

	cached, _ := ctx.CacheContext()
	_, err = runTax(t, arkApp, ante.WithTransferTax(cached, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 7))), tx, false, true)
	require.NoError(t, err)
	require.Equal(t, "7ausd", collected(arkApp, ctx, cached, taxCollector).String())

	_, err = runTax(t, arkApp, ctx, tx, false, true)
	require.ErrorContains(t, err, "no transfer tax on the context")
	require.Equal(t, math.NewInt(20), usdBalance(arkApp, ctx, tx.payer))

	// A failed transaction is handed on before the figure is read, so it
	// cannot fail again on the seam.
	_, err = runTax(t, arkApp, ctx, tx, false, false)
	require.NoError(t, err)
}
