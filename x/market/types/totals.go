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
// Only what settlement actually decides is carried. Every value is priced at
// the rate its own conversion quoted, so nothing here needs a rate set to
// interpret and settlement performs no conversion at all.
//
// Every field is an aggregate over the block, so a zero is ordinary: a block
// may hold only expansions, only redemptions, or neither.
type ConversionTotals struct {
	// GrossOffer is the NOAH this block's expansions took in, spread included.
	// It is what the waterfall places (D6): principal and premium alike go to
	// fund targets first, and only the overflow burns.
	GrossOffer math.Int
	// EligiblePrincipal is the NOAH value of the stable supply this block's
	// expansions minted, truncated per conversion at that conversion's own
	// quoted rate. It is what liability grew by, which is what the flow
	// indicator reads; the difference from GrossOffer is the spread and dust.
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
	return t.GrossOffer.IsZero() &&
		t.EligiblePrincipal.IsZero() &&
		t.RedemptionOutput.IsZero() &&
		t.RedeemedValue.IsZero()
}

// Validate reports whether the totals are internally coherent enough to settle.
// It is the aggregate backstop behind the per-conversion checks the recording
// path already made: those refuse a bad conversion as it happens, and this
// catches an aggregate that disagrees with the parts it was built from.
func (t ConversionTotals) Validate() error {
	if t.GrossOffer.IsNil() ||
		t.EligiblePrincipal.IsNil() ||
		t.RedemptionOutput.IsNil() ||
		t.RedeemedValue.IsNil() {
		return fmt.Errorf("conversion totals are incomplete")
	}
	if t.GrossOffer.IsNegative() ||
		t.EligiblePrincipal.IsNegative() ||
		t.RedemptionOutput.IsNegative() ||
		t.RedeemedValue.IsNegative() {
		return fmt.Errorf("conversion totals cannot be negative")
	}
	// The stable an expansion minted is worth no more than the NOAH that bought
	// it: the per-conversion refusal, restated over the block.
	if t.EligiblePrincipal.GT(t.GrossOffer) {
		return fmt.Errorf(
			"eligible principal %s exceeds the gross offer %s",
			t.EligiblePrincipal,
			t.GrossOffer,
		)
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
