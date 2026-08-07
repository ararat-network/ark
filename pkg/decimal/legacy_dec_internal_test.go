package decimal

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"
)

// TestFastPathBoundsStayWithinLegacyDecRange re-derives the fast-path operand
// bounds against the live cosmossdk.io/math range rather than trusting the
// comments that justify them. Each case builds the widest operands its bound
// admits and hands them to unchecked LegacyDec: if a math release ever narrowed
// the valid range, or a bound were widened past what the range allows, the
// resulting overflow surfaces here instead of in a block.
func TestFastPathBoundsStayWithinLegacyDecRange(t *testing.T) {
	// LegacyDec's range stops just below 2^256 * 10^18, so an operand any wider
	// than this is rejected before a fast path can see it. Every case caps its
	// operands here, which keeps them admissible whichever way a bound moves.
	widestOperandBitLen := new(big.Int).Mul(bitValue(256), precision).BitLen() - 1

	widestAddSubOperand := widestRawDec(min(maxDirectAddSubOperandBitLen, widestOperandBitLen))

	// Only the combined width bounds the product, so the split is free; an even
	// one keeps both operands as far inside the valid range as it allows.
	widestMulOperandBitLen := min(maxDirectMulOperandBitLenSum/2, widestOperandBitLen)
	widestMulOperand := widestRawDec(widestMulOperandBitLen)
	widestMulMultiplier := widestRawDec(
		min(maxDirectMulOperandBitLenSum-widestMulOperandBitLen, widestOperandBitLen),
	)

	// A raw divisor of 1 is the narrowest there is, so it admits the widest
	// numerator the quotient bound allows.
	smallestQuoDivisor := rawDec(big.NewInt(1))
	widestQuoNumerator := widestRawDec(min(maxDirectQuotientBitDelta+1, widestOperandBitLen))

	tests := []struct {
		name      string
		a, b      math.LegacyDec
		operation func(a, b math.LegacyDec) math.LegacyDec
	}{
		{
			name:      "add bound",
			a:         widestAddSubOperand,
			b:         widestAddSubOperand,
			operation: math.LegacyDec.Add,
		},
		{
			name:      "subtract bound",
			a:         widestAddSubOperand,
			b:         widestAddSubOperand.Neg(),
			operation: math.LegacyDec.Sub,
		},
		{
			name:      "multiply bound",
			a:         widestMulOperand,
			b:         widestMulMultiplier,
			operation: math.LegacyDec.Mul,
		},
		{
			name:      "divide bound",
			a:         widestQuoNumerator,
			b:         smallestQuoDivisor,
			operation: math.LegacyDec.Quo,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Out-of-range operands never reach a fast path, so a case built
			// from them would prove nothing.
			require.True(t, tc.a.IsInValidRange(), "operand %s is rejected before the fast path", tc.a)
			require.True(t, tc.b.IsInValidRange(), "operand %s is rejected before the fast path", tc.b)

			require.NotPanics(t, func() {
				tc.operation(tc.a, tc.b)
			}, "the %s fast path admits operands LegacyDec cannot handle", tc.name)
		})
	}
}

// TestPrecisionAssumptionsMatchLegacyDec pins the two facts about LegacyDec's
// scale factor that the fast-path bounds are derived from: that the package
// scales by the same factor the library does, and that the factor sits between
// 2^59 and 2^60, the inequality the multiply and divide bounds cite.
func TestPrecisionAssumptionsMatchLegacyDec(t *testing.T) {
	require.Zero(t, math.LegacyOneDec().BigIntMut().Cmp(precision),
		"LegacyDec no longer scales by the package's precision factor")

	require.Equal(t, 60, precision.BitLen(),
		"the fast-path bounds assume 2^59 < 10^%d < 2^60", math.LegacyPrecision)

	require.Zero(t, squaredPrecision.Cmp(new(big.Int).Mul(precision, precision)),
		"the wide divide path no longer mirrors LegacyDec's double scaling")
}

func rawDec(value *big.Int) math.LegacyDec {
	return math.LegacyNewDecFromBigIntWithPrec(value, math.LegacyPrecision)
}

// widestRawDec returns the largest raw value of the given bit length.
func widestRawDec(bits int) math.LegacyDec {
	value := bitValue(bits)

	return rawDec(value.Sub(value, big.NewInt(1)))
}

func bitValue(bits int) *big.Int {
	return new(big.Int).Lsh(big.NewInt(1), uint(max(bits, 0)))
}
