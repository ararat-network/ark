package ante_test

import (
	stdmath "math"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/ararat-network/ark/app/ante"
	chain "github.com/ararat-network/ark/pkg/chain"
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

// TestFeeDecoratorEnforcesTheConsensusFloor pins the fee gate's contract:
// the reference-denom fee must cover ceil(BaseGasPrice × gas) and is charged
// exactly that, excess never leaves the payer and buys no rank, and another
// accepted denomination is held to its own converted requirement. The
// transaction carries no taxable message, so nothing but the base fee
// reaches the fee collector.
func TestFeeDecoratorEnforcesTheConsensusFloor(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	tx.msgs = nil
	// Default launch price: 0.1 axdr per gas unit, so 200k gas requires
	// 20,000 axdr.
	tx.gas = 200_000
	required := sdk.NewInt64Coin(chain.XDRBaseDenom, 20_000)
	fundAccount(t, arkApp, ctx, tx.payer, sdk.NewCoins(sdk.NewInt64Coin(chain.XDRBaseDenom, 1_000_000)))

	tx.fee = nil
	r := runFeeOnCache(t, arkApp, ctx, tx, false)
	require.ErrorContains(t, r.err, "base fee requires "+required.String())

	// NOAH is accepted through its cross, so a NOAH fee is not a zero
	// payment toward the floor: it is refused only for falling short of its
	// own requirement.
	params, err := arkApp.TreasuryKeeper.Params.Get(ctx)
	require.NoError(t, err)
	price, err := arkApp.TreasuryKeeper.BaseGasPrice.Get(ctx)
	require.NoError(t, err)
	noahRequired, _, err := arkApp.TreasuryKeeper.GetRequiredGasFee(ctx, params, price, tx.gas, chain.NoahBaseDenom)
	require.NoError(t, err)
	tx.fee = sdk.NewCoins(noahRequired.SubAmount(math.OneInt()))
	r = runFeeOnCache(t, arkApp, ctx, tx, false)
	require.ErrorContains(t, r.err, "insufficient fee")

	// An exactly covering fee is a tenth of a reference unit per gas, which
	// truncates to no rank, and reaches the fee collector whole.
	tx.fee = sdk.NewCoins(required)
	r = runFeeOnCache(t, arkApp, ctx, tx, false)
	require.NoError(t, r.err)
	require.Zero(t, r.handed.Priority())
	require.Equal(t, required.String(), r.gas)

	// 400,000axdr over 200,000 gas is a ceiling: the requirement is charged,
	// the 380,000 of excess stays with the payer, and a stable leg's excess
	// ranks nothing.
	tx.fee = sdk.NewCoins(sdk.NewInt64Coin(chain.XDRBaseDenom, 400_000))
	r = runFeeOnCache(t, arkApp, ctx, tx, false)
	require.NoError(t, r.err)
	require.Equal(t, required.String(), r.gas)
	require.Zero(t, r.handed.Priority())
	require.Equal(t, math.NewInt(980_000), arkApp.BankKeeper.GetBalance(r.cached, tx.payer, chain.XDRBaseDenom).Amount)
}

// TestFeeDecoratorHoldsTheFeeToTheDeclaredTax pins the declaration: the
// fee covers the tax coin for coin or is refused before anything is
// deducted, what remains is a ceiling charged at the requirement, the tax
// reaches its collector, and neither the tax nor a stable leg's excess buys
// rank.
func TestFeeDecoratorHoldsTheFeeToTheDeclaredTax(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	// The fixture send owes 10ausd. 200k gas at the 0.1 price needs 20,000
	// of the reference, or of ausd at its identity factor.
	tx.gas = 200_000
	fundAccount(t, arkApp, ctx, tx.payer, sdk.NewCoins(
		sdk.NewInt64Coin(chain.USDBaseDenom, 1_000_000),
		sdk.NewInt64Coin(chain.XDRBaseDenom, 1_000_000),
	))
	tax := sdk.NewInt64Coin(chain.USDBaseDenom, 10)
	run := func(fee sdk.Coins) feeRun {
		tx.fee = fee
		return runFeeOnCache(t, arkApp, ctx, tx, false)
	}

	t.Run("a fee short of the tax is refused whatever it offers for gas", func(t *testing.T) {
		r := run(sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 9), sdk.NewInt64Coin(chain.XDRBaseDenom, 1_000_000)))
		require.ErrorIs(t, r.err, sdkerrors.ErrInsufficientFee)
		require.ErrorContains(t, r.err, "does not cover transfer tax 10ausd")
		require.Empty(t, r.gas)
		require.Empty(t, r.tax)
	})

	t.Run("tax and gas in one denomination", func(t *testing.T) {
		r := run(sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 20_010)))
		require.NoError(t, r.err)
		require.Equal(t, "20000ausd", r.gas)
		require.Equal(t, "10ausd", r.tax)
		require.Zero(t, r.handed.Priority())
	})

	t.Run("tax in one denomination and gas in another", func(t *testing.T) {
		r := run(sdk.NewCoins(tax, sdk.NewInt64Coin(chain.XDRBaseDenom, 400_000)))
		require.NoError(t, r.err)
		require.Equal(t, "20000axdr", r.gas)
		require.Equal(t, "10ausd", r.tax)
		require.Zero(t, r.handed.Priority())
	})

	t.Run("excess in a stable leg never leaves the payer", func(t *testing.T) {
		r := run(sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 400_010)))
		require.NoError(t, r.err)
		require.Equal(t, "20000ausd", r.gas)
		require.Equal(t, "10ausd", r.tax)
		require.Zero(t, r.handed.Priority())
		require.Equal(t, math.NewInt(980_010), usdBalance(arkApp, r.cached, tx.payer))
	})

	t.Run("excess in the tax leg beside another for gas is a ceiling, untouched", func(t *testing.T) {
		r := run(sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 11), sdk.NewInt64Coin(chain.XDRBaseDenom, 20_000)))
		require.NoError(t, r.err)
		require.Equal(t, "20000axdr", r.gas)
		require.Equal(t, "10ausd", r.tax)
		require.Equal(t, math.NewInt(1_000_010), usdBalance(arkApp, r.cached, tx.payer))
	})

	t.Run("the tax alone buys no gas", func(t *testing.T) {
		r := run(sdk.NewCoins(tax))
		require.ErrorIs(t, r.err, sdkerrors.ErrInsufficientFee)
		require.ErrorContains(t, r.err,
			"base fee requires 20000axdr or its equivalent in an accepted fee denomination in addition to transfer tax 10ausd, got 10ausd")
		require.Empty(t, r.tax)
	})

	t.Run("a zero requirement admits the tax alone and deducts no gas", func(t *testing.T) {
		require.NoError(t, arkApp.TreasuryKeeper.BaseGasPrice.Set(ctx, math.LegacyZeroDec()))
		r := run(sdk.NewCoins(tax))
		require.NoError(t, r.err)
		require.Empty(t, r.gas)
		require.Equal(t, "10ausd", r.tax)
		require.Zero(t, r.handed.Priority())
	})
}

// TestFeeDecoratorWaivesGenesisHeight pins the gentx waiver: height zero
// carries fee-less genesis transactions, prices no tax — Treasury genesis
// may not have run — and moves nothing; and only height zero: a missing
// policy past genesis is an error, not a waiver.
func TestFeeDecoratorWaivesGenesisHeight(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	require.NoError(t, arkApp.TreasuryKeeper.EconomicPolicy.Remove(ctx))
	tx.fee = nil
	tx.gas = 200_000

	r := runFeeOnCache(t, arkApp, ctx.WithBlockHeight(0), tx, false)
	require.NoError(t, r.err)
	require.Zero(t, r.handed.Priority())
	require.Empty(t, r.gas)
	require.Empty(t, r.tax)
	require.Equal(t, math.NewInt(20), usdBalance(arkApp, r.cached, tx.payer))

	r = runFeeOnCache(t, arkApp, ctx, tx, false)
	require.Error(t, r.err)
}

// TestFeeDecoratorRefusesZeroGas mirrors the SDK: past genesis a transaction
// declares positive gas, except under simulation, whose gas limit is the
// figure being estimated.
func TestFeeDecoratorRefusesZeroGas(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	tx.gas = 0

	r := runFeeOnCache(t, arkApp, ctx, tx, false)
	require.ErrorContains(t, r.err, "must provide positive gas")

	r = runFeeOnCache(t, arkApp, ctx, tx, true)
	require.NoError(t, r.err)
}

// TestFeeDecoratorSkipsTheGateWithoutRefusing pins the two modes in which
// nothing is refused on fee grounds — simulation, and the harness with the
// gate off: the tax is set aside and charged in full, a stable leg's slack
// is deducted only where it covers the requirement, and nothing ranks.
func TestFeeDecoratorSkipsTheGateWithoutRefusing(t *testing.T) {
	for _, mode := range []struct {
		name     string
		simulate bool
	}{
		{"simulate", true},
		{"gate off", false},
	} {
		t.Run(mode.name, func(t *testing.T) {
			arkApp, ctx, tx := setupTreasuryAnteTest(t)
			if !mode.simulate {
				ante.SetBaseFeeGate(false)
				t.Cleanup(func() { ante.SetBaseFeeGate(true) })
			}
			// Gas far below the floor, which the gate would refuse.
			tx.gas = 1_000_000

			// Fee-less: nothing deducted, the tax charged.
			tx.fee = nil
			r := runFeeOnCache(t, arkApp, ctx, tx, mode.simulate)
			require.NoError(t, r.err)
			require.Zero(t, r.handed.Priority())
			require.Empty(t, r.gas)
			require.Equal(t, "10ausd", r.tax)

			// A fee short of the tax: all of it set aside, none deducted.
			tx.fee = sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 4))
			r = runFeeOnCache(t, arkApp, ctx, tx, mode.simulate)
			require.NoError(t, r.err)
			require.Empty(t, r.gas)
			require.Equal(t, "10ausd", r.tax)
			require.Equal(t, math.NewInt(10), usdBalance(arkApp, r.cached, tx.payer))

			// A fee beyond the tax but short of the requirement: the tax
			// set aside, the slack a ceiling nothing is charged under.
			tx.fee = sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 12))
			r = runFeeOnCache(t, arkApp, ctx, tx, mode.simulate)
			require.NoError(t, r.err)
			require.Zero(t, r.handed.Priority())
			require.Empty(t, r.gas)
			require.Equal(t, "10ausd", r.tax)
			require.Equal(t, math.NewInt(10), usdBalance(arkApp, r.cached, tx.payer))

			// A covering fee is charged what execution charges: the
			// requirement, 100,000 at the 0.1 price, and the tax.
			fundAccount(t, arkApp, ctx, tx.payer, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 1_000_000)))
			tx.fee = sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 200_010))
			r = runFeeOnCache(t, arkApp, ctx, tx, mode.simulate)
			require.NoError(t, r.err)
			require.Zero(t, r.handed.Priority())
			require.Equal(t, "100000ausd", r.gas)
			require.Equal(t, "10ausd", r.tax)
		})
	}
}

// TestGasPrioritySaturates pins the ranking's bounds: zero gas ranks zero
// rather than dividing by it, and an absurd fee pins at the int64 ceiling
// rather than overflowing.
func TestGasPrioritySaturates(t *testing.T) {
	require.Zero(t, ante.GasPriority(math.NewInt(1_000), 0))
	huge := math.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 100))
	require.Equal(t, int64(stdmath.MaxInt64), ante.GasPriority(huge, 1))
}

// TestFeeDecoratorAcceptsAnyCoveringDenom pins the 2b gate under D80: a fee
// satisfies when any single leg covers its own converted requirement,
// unaccepted denominations are tax-only ceilings rather than fatal, a stable
// leg is charged the requirement alone, and a NOAH leg is charged whole with
// its remainder ranked in reference units per gas.
func TestFeeDecoratorAcceptsAnyCoveringDenom(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	require.NoError(t, arkApp.TreasuryKeeper.ConversionFactors.Set(ctx, chain.USDBaseDenom, treasurytypes.ConversionFactor{
		Denom:  chain.USDBaseDenom,
		Factor: math.LegacyNewDec(2),
	}))
	require.NoError(t, arkApp.TreasuryKeeper.ConversionFactors.Set(ctx, chain.NoahBaseDenom, treasurytypes.ConversionFactor{
		Denom:  chain.NoahBaseDenom,
		Factor: math.LegacyMustNewDecFromStr("0.5"),
	}))
	// Genesis seeds every member's factor, so the unpriced case is made, not
	// found: dropping akrw's factor makes it a denomination the gate refuses.
	require.NoError(t, arkApp.TreasuryKeeper.ConversionFactors.Remove(ctx, chain.KRWBaseDenom))
	tx.msgs = nil
	tx.gas = 200_000
	fundAccount(t, arkApp, ctx, tx.payer, sdk.NewCoins(
		sdk.NewInt64Coin(chain.USDBaseDenom, 1_000_000),
		sdk.NewInt64Coin(chain.NoahBaseDenom, 1_000_000),
		sdk.NewInt64Coin(chain.KRWBaseDenom, 1_000_000_000),
	))
	run := func(fee sdk.Coins) feeRun {
		tx.fee = fee
		return runFeeOnCache(t, arkApp, ctx, tx, false)
	}
	// At the 0.1 base price: 20,000axdr, 40,000ausd, or 10,000anoah.

	// An unpriced denomination cannot satisfy the gate, and the refusal
	// quotes the reference requirement.
	r := run(sdk.NewCoins(sdk.NewInt64Coin(chain.KRWBaseDenom, 1_000_000_000)))
	require.ErrorContains(t, r.err, "base fee requires 20000axdr")

	// An unpriced leg beside a covering one is a ceiling nothing is charged
	// under: ausd pays, akrw stays whole.
	r = run(sdk.NewCoins(
		sdk.NewInt64Coin(chain.KRWBaseDenom, 1_000_000_000),
		sdk.NewInt64Coin(chain.USDBaseDenom, 40_000),
	))
	require.NoError(t, r.err)
	require.Equal(t, "40000ausd", r.gas)
	require.Equal(t, math.NewInt(1_000_000_000), arkApp.BankKeeper.GetBalance(r.cached, tx.payer, chain.KRWBaseDenom).Amount)

	// A covering factor-table member admits the transaction on its own.
	r = run(sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 40_000)))
	require.NoError(t, r.err)
	require.Equal(t, "40000ausd", r.gas)
	require.Zero(t, r.handed.Priority())

	// A NOAH fee of 200,000 is charged whole: 10,000 of base fee and a
	// 190,000 tip, which at factor 0.5 is 380,000 reference units over
	// 200,000 gas, ranking one.
	r = run(sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 200_000)))
	require.NoError(t, r.err)
	require.Equal(t, "200000anoah", r.gas)
	require.Equal(t, int64(1), r.handed.Priority())
}

// TestFeeDecoratorRanksOnlyTheNoahTip pins D80: a stable leg's excess is
// never charged and never ranks, a NOAH leg beside a covering stable leg is
// charged whole as the tip and ranks through NOAH's factor, NOAH pays the
// base fee only when no stable leg covers it, and an unpriced NOAH leg is
// refused.
func TestFeeDecoratorRanksOnlyTheNoahTip(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	require.NoError(t, arkApp.TreasuryKeeper.ConversionFactors.Set(ctx, chain.NoahBaseDenom, treasurytypes.ConversionFactor{
		Denom:  chain.NoahBaseDenom,
		Factor: math.LegacyMustNewDecFromStr("0.5"),
	}))
	// 200k gas at the 0.1 price: 20,000ausd at identity or 10,000anoah at
	// factor 0.5. The fixture send owes 10ausd.
	tx.gas = 200_000
	fundAccount(t, arkApp, ctx, tx.payer, sdk.NewCoins(
		sdk.NewInt64Coin(chain.USDBaseDenom, 1_000_000),
		sdk.NewInt64Coin(chain.NoahBaseDenom, 1_000_000),
	))
	run := func(fee sdk.Coins) feeRun {
		tx.fee = fee
		return runFeeOnCache(t, arkApp, ctx, tx, false)
	}

	t.Run("stable excess never leaves the payer and buys no rank", func(t *testing.T) {
		r := run(sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 500_010)))
		require.NoError(t, r.err)
		require.Equal(t, "20000ausd", r.gas)
		require.Equal(t, "10ausd", r.tax)
		require.Zero(t, r.handed.Priority())
		require.Equal(t, math.NewInt(980_010), usdBalance(arkApp, r.cached, tx.payer))
	})

	t.Run("a NOAH leg beside a covering stable leg is the tip, whole", func(t *testing.T) {
		r := run(sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 20_010), sdk.NewInt64Coin(chain.NoahBaseDenom, 200_000)))
		require.NoError(t, r.err)
		require.Equal(t, "200000anoah,20000ausd", r.gas)
		require.Equal(t, "10ausd", r.tax)
		// 200,000 at factor 0.5 is 400,000 reference units over 200,000 gas.
		require.Equal(t, int64(2), r.handed.Priority())
		require.Equal(t, "200000anoah", txEvent(t, r.handed)[ante.AttributeKeyTip])
	})

	t.Run("NOAH pays the base fee when no stable leg covers it", func(t *testing.T) {
		r := run(sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 10), sdk.NewInt64Coin(chain.NoahBaseDenom, 210_000)))
		require.NoError(t, r.err)
		require.Equal(t, "210000anoah", r.gas)
		require.Equal(t, "10ausd", r.tax)
		require.Equal(t, int64(2), r.handed.Priority())
	})

	t.Run("a tip past the decimal domain is refused rather than panicking", func(t *testing.T) {
		// A declared fee amount is bounded only by the decoder at 2^256, and
		// the 0.5 factor doubles it: the stock Quo panics where the checked
		// one reports. The stable leg covers the requirement, so the whole
		// NOAH leg is the tip.
		huge := math.NewIntFromBigInt(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)))
		r := run(sdk.NewCoins(
			sdk.NewInt64Coin(chain.USDBaseDenom, 20_010),
			sdk.NewCoin(chain.NoahBaseDenom, huge),
		))
		require.ErrorContains(t, r.err, "ranking the anoah tip")
		require.Empty(t, r.gas)
		require.Empty(t, r.tax)
	})

	t.Run("an unpriced NOAH leg is refused", func(t *testing.T) {
		require.NoError(t, arkApp.TreasuryKeeper.ConversionFactors.Remove(ctx, chain.NoahBaseDenom))
		fee := sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 20_010), sdk.NewInt64Coin(chain.NoahBaseDenom, 1))
		r := run(fee)
		require.ErrorContains(t, r.err, "reading the NOAH gas factor")
		require.Empty(t, r.gas)
		require.Empty(t, r.tax)

		// Genesis mandates the cross, so the state this reaches is broken,
		// not a launch stage. An estimate refuses it on the same terms
		// rather than quoting a fee execution would not accept.
		tx.fee = fee
		r = runFeeOnCache(t, arkApp, ctx, tx, true)
		require.ErrorContains(t, r.err, "reading the NOAH gas factor")
		require.Empty(t, r.gas)
		require.Empty(t, r.tax)
	})
}

// TestFeeDecoratorReadsAFeeTwoWays pins the two readings of a fee that
// arrives as decoded Coins, which nothing sorts or dedupes. The leg that pays
// the base fee follows the payer's own order, while the tax and the NOAH leg
// are read by denomination, which a binary search over unsorted coins gets
// wrong. Validating a sorted copy is what makes the second reading sound
// without refusing the first: a repeated denomination, a zero leg or a junk
// denom still fails.
func TestFeeDecoratorReadsAFeeTwoWays(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	for _, denom := range []string{chain.USDBaseDenom, chain.KRWBaseDenom} {
		require.NoError(t, arkApp.TreasuryKeeper.ConversionFactors.Set(ctx, denom, treasurytypes.ConversionFactor{
			Denom:  denom,
			Factor: math.LegacyOneDec(),
		}))
	}
	require.NoError(t, arkApp.TreasuryKeeper.ConversionFactors.Set(ctx, chain.NoahBaseDenom, treasurytypes.ConversionFactor{
		Denom:  chain.NoahBaseDenom,
		Factor: math.LegacyMustNewDecFromStr("0.5"),
	}))
	// 200k gas at the 0.1 price is 20,000 at identity. The fixture send owes
	// 10ausd.
	tx.gas = 200_000
	fundAccount(t, arkApp, ctx, tx.payer, sdk.NewCoins(
		sdk.NewInt64Coin(chain.USDBaseDenom, 1_000_000),
		sdk.NewInt64Coin(chain.KRWBaseDenom, 1_000_000),
		sdk.NewInt64Coin(chain.NoahBaseDenom, 1_000_000),
	))
	run := func(fee sdk.Coins) feeRun {
		tx.fee = fee
		return runFeeOnCache(t, arkApp, ctx, tx, false)
	}

	t.Run("the payer's order names the leg that pays", func(t *testing.T) {
		r := run(sdk.Coins{
			sdk.NewInt64Coin(chain.USDBaseDenom, 50_010),
			sdk.NewInt64Coin(chain.KRWBaseDenom, 50_000),
		})
		require.NoError(t, r.err)
		require.Equal(t, "20000ausd", r.gas)

		r = run(sdk.Coins{
			sdk.NewInt64Coin(chain.KRWBaseDenom, 50_000),
			sdk.NewInt64Coin(chain.USDBaseDenom, 50_010),
		})
		require.NoError(t, r.err)
		require.Equal(t, "20000akrw", r.gas)
	})

	t.Run("a leg a binary search would miss still covers the tax", func(t *testing.T) {
		// AmountOf over these three in encoded order finds only the middle
		// one, so reading the fee itself would see no ausd and refuse a fee
		// that carries 50,010 of it.
		r := run(sdk.Coins{
			sdk.NewInt64Coin(chain.USDBaseDenom, 50_010),
			sdk.NewInt64Coin(chain.NoahBaseDenom, 100),
			sdk.NewInt64Coin(chain.KRWBaseDenom, 50_000),
		})
		require.NoError(t, r.err)
		require.Equal(t, "10ausd", r.tax)
		require.Equal(t, "100anoah,20000ausd", r.gas)
	})

	t.Run("a denomination declared twice is refused", func(t *testing.T) {
		r := run(sdk.Coins{
			sdk.NewInt64Coin(chain.USDBaseDenom, 25_010),
			sdk.NewInt64Coin(chain.USDBaseDenom, 25_000),
		})
		require.ErrorContains(t, r.err, "duplicate denomination ausd")
		require.Empty(t, r.gas)
		require.Empty(t, r.tax)
	})

	t.Run("a zero leg is refused", func(t *testing.T) {
		r := run(sdk.Coins{
			sdk.NewInt64Coin(chain.USDBaseDenom, 50_010),
			sdk.NewInt64Coin(chain.KRWBaseDenom, 0),
		})
		require.ErrorContains(t, r.err, "not positive")
		require.Empty(t, r.gas)
		require.Empty(t, r.tax)
	})
}
