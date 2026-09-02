package ante_test

import (
	stdmath "math"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

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

// TestBaseFeeCheckerEnforcesTheConsensusFloor pins the fee gate's contract:
// the reference-denom fee must cover ceil(BaseGasPrice × gas), the whole fee
// per gas becomes priority, and another accepted denomination is held to its
// own converted requirement.
func TestBaseFeeCheckerEnforcesTheConsensusFloor(t *testing.T) {
	arkApp, ctx, _ := setupTreasuryAnteTest(t)
	checker := ante.NewBaseFeeChecker(arkApp.TreasuryKeeper)

	// Default launch price: 0.1 axdr per gas unit, so 200k gas requires
	// 20,000 axdr.
	gas := uint64(200_000)
	required := sdk.NewInt64Coin(chain.XDRBaseDenom, 20_000)

	_, _, err := checker(ctx, treasuryFeeTx{gas: gas})
	require.ErrorContains(t, err, "base fee requires "+required.String())

	// NOAH is accepted through its cross, so a NOAH fee is not a zero
	// payment toward the floor: it is refused only for falling short of its
	// own requirement.
	params, err := arkApp.TreasuryKeeper.Params.Get(ctx)
	require.NoError(t, err)
	price, err := arkApp.TreasuryKeeper.BaseGasPrice.Get(ctx)
	require.NoError(t, err)
	noahRequired, _, err := arkApp.TreasuryKeeper.GetRequiredGasFee(ctx, params, price, gas, chain.NoahBaseDenom)
	require.NoError(t, err)
	_, _, err = checker(ctx, treasuryFeeTx{
		gas: gas,
		fee: sdk.NewCoins(noahRequired.SubAmount(math.OneInt())),
	})
	require.ErrorContains(t, err, "insufficient fee")

	// An exactly covering fee is a tenth of a reference unit per gas, which
	// truncates to no rank.
	fee, priority, err := checker(ctx, treasuryFeeTx{gas: gas, fee: sdk.NewCoins(required)})
	require.NoError(t, err)
	require.Equal(t, sdk.NewCoins(required), fee)
	require.Zero(t, priority)

	// 400,000axdr over 200,000 gas ranks two: the requirement is not
	// subtracted, so a transaction ranks by what it pays, not by what it
	// pays above the floor — which would rank this one.
	paying := sdk.NewCoins(sdk.NewInt64Coin(chain.XDRBaseDenom, 400_000))
	_, priority, err = checker(ctx, treasuryFeeTx{gas: gas, fee: paying})
	require.NoError(t, err)
	require.Equal(t, int64(2), priority)
}

// TestBaseFeeCheckerWaivesGenesisHeight pins the gentx waiver: height zero
// carries fee-less genesis transactions, and only height zero.
func TestBaseFeeCheckerWaivesGenesisHeight(t *testing.T) {
	arkApp, ctx, _ := setupTreasuryAnteTest(t)
	checker := ante.NewBaseFeeChecker(arkApp.TreasuryKeeper)

	genesisCtx := ctx.WithBlockHeight(0)
	fee, priority, err := checker(genesisCtx, treasuryFeeTx{gas: 200_000})
	require.NoError(t, err)
	require.True(t, fee.IsZero())
	require.Zero(t, priority)
}

// TestGasPrioritySaturates pins the ranking's bounds: zero gas ranks zero
// rather than dividing by it, and an absurd fee pins at the int64 ceiling
// rather than overflowing.
func TestGasPrioritySaturates(t *testing.T) {
	require.Zero(t, ante.GasPriority(math.NewInt(1_000), 0))
	huge := math.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 100))
	require.Equal(t, int64(stdmath.MaxInt64), ante.GasPriority(huge, 1))
}

// TestBaseFeeCheckerAcceptsAnyCoveringDenom pins the 2b gate: a fee
// satisfies when any single denomination covers its own converted
// requirement, unaccepted denominations are skipped rather than fatal, and
// priority is the whole fee normalised to reference units per gas whatever
// denomination paid it.
func TestBaseFeeCheckerAcceptsAnyCoveringDenom(t *testing.T) {
	arkApp, ctx, _ := setupTreasuryAnteTest(t)
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
	checker := ante.NewBaseFeeChecker(arkApp.TreasuryKeeper)
	gas := uint64(200_000)
	// At the 0.1 base price: 20,000axdr, 40,000ausd, or 10,000anoah.

	// An unpriced denomination cannot satisfy the gate, and the refusal
	// quotes the reference requirement.
	_, _, err := checker(ctx, treasuryFeeTx{
		gas: gas,
		fee: sdk.NewCoins(sdk.NewInt64Coin(chain.KRWBaseDenom, 1_000_000_000)),
	})
	require.ErrorContains(t, err, "base fee requires 20000axdr")

	// Any single covering denomination admits the transaction, junk coins
	// beside it notwithstanding.
	fee, priority, err := checker(ctx, treasuryFeeTx{
		gas: gas,
		fee: sdk.NewCoins(
			sdk.NewInt64Coin(chain.KRWBaseDenom, 1_000_000_000),
			sdk.NewInt64Coin(chain.USDBaseDenom, 40_000),
		),
	})
	require.NoError(t, err)
	require.Len(t, fee, 2)
	require.Zero(t, priority)

	// A NOAH fee of 200,000 at factor 0.5 is 400,000 reference units, two
	// per gas unit — the same rank the equal-value reference fee earns, and
	// the requirement is no more subtracted here than there.
	_, priority, err = checker(ctx, treasuryFeeTx{
		gas: gas,
		fee: sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 200_000)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), priority)
}
