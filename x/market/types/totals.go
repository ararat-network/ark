package types

import (
	"fmt"

	"cosmossdk.io/math"
)

// ConversionTotals is one block's recorded conversion flow: the facts Market
// hands Treasury to settle. Market owns them because Market produced them —
// every figure here is one its own quote already stood behind — and Treasury
// owns what they are worth against the block's liability, which is the one
// thing no conversion can tell it.
//
// Only what settlement actually decides is carried. The spread is absent
// because nothing decides it: it is quote arithmetic owing nothing to
// liability, fund state, or valuation completeness, so Market burns it in the
// conversion that charged it and what arrives here is already net of it. Every
// value is priced at the rate its own conversion quoted, so nothing here needs
// a rate set to interpret and settlement performs no conversion at all.
//
// Every field is an aggregate over the block, so a zero is ordinary: a block
// may hold only expansions, only redemptions, or neither.
type ConversionTotals struct {
	// EligiblePrincipal is the NOAH value of the stable supply this block's
	// expansions minted, truncated per conversion at that conversion's own
	// quoted rate and net of the spread already burned. It is exactly what the
	// waterfall has to place.
	EligiblePrincipal math.Int
	// RedemptionOutput is the NOAH minted across both redemption paths.
	RedemptionOutput math.Int
	// RedeemedValue is the NOAH value of the stable supply those redemptions
	// burned. Both paths accumulate here on the same terms even though their
	// rates come from different places — the oracle set for a quoted
	// redemption, the plan's own committed rate for a settled one — because
	// what settlement divides by is the value, never the supply that carried
	// it.
	RedeemedValue math.LegacyDec
}

// IsZero reports whether the block recorded no conversion at all, which is the
// EndBlocker's licence to skip settlement — and with it the block's only
// liability valuation.
func (t ConversionTotals) IsZero() bool {
	return t.EligiblePrincipal.IsZero() &&
		t.RedemptionOutput.IsZero() &&
		t.RedeemedValue.IsZero()
}

// Validate reports whether the totals are internally coherent enough to settle.
// It is the aggregate backstop behind the per-conversion checks the recording
// path already made: those refuse a bad conversion as it happens, and this
// catches an aggregate that disagrees with the parts it was built from.
func (t ConversionTotals) Validate() error {
	if t.EligiblePrincipal.IsNil() ||
		t.RedemptionOutput.IsNil() ||
		t.RedeemedValue.IsNil() {
		return fmt.Errorf("conversion totals are incomplete")
	}
	if t.EligiblePrincipal.IsNegative() ||
		t.RedemptionOutput.IsNegative() ||
		t.RedeemedValue.IsNegative() {
		return fmt.Errorf("conversion totals cannot be negative")
	}
	// Redemption output cannot outvalue the liability it retired. This is the
	// bound that keeps the coverage draw inside the Buffer: the draw is a
	// fraction of the output, the fraction is the Buffer over a basis that
	// contains this value, so an output within it can never draw more than the
	// Buffer holds.
	if math.LegacyNewDecFromInt(t.RedemptionOutput).GT(t.RedeemedValue) {
		return fmt.Errorf(
			"redemption output %s exceeds the redeemed liability %s",
			t.RedemptionOutput,
			t.RedeemedValue,
		)
	}

	return nil
}
