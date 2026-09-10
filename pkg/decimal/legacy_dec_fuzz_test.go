package decimal_test

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/pkg/decimal"
)

// checkedOps pairs every checked operation with the LegacyDec operation it must
// reproduce exactly.
var checkedOps = []struct {
	name    string
	checked func(math.LegacyDec, math.LegacyDec) (math.LegacyDec, error)
	stock   func(math.LegacyDec, math.LegacyDec) math.LegacyDec
}{
	{name: "Add", checked: decimal.Add, stock: math.LegacyDec.Add},
	{name: "Sub", checked: decimal.Sub, stock: math.LegacyDec.Sub},
	{name: "Mul", checked: decimal.Mul, stock: math.LegacyDec.Mul},
	{name: "Quo", checked: decimal.Quo, stock: math.LegacyDec.Quo},
}

// maxFuzzOperandBytes spans every raw value LegacyDec can hold. The valid range
// stops just below 2^316, so 40 bytes also reaches the out-of-range values the
// checked operations must reject.
const maxFuzzOperandBytes = 40

// FuzzCheckedArithmeticMatchesLegacyDec checks identical results or error-versus-panic outcomes.
// The ordinary seed run covers fast-path boundaries and rounding ties; fuzzing explores additional
// operands.
func FuzzCheckedArithmeticMatchesLegacyDec(f *testing.F) {
	addFuzzSeeds(f)

	f.Fuzz(func(t *testing.T, aBytes, bBytes []byte, aNegative, bNegative bool, opIndex byte) {
		if len(aBytes) > maxFuzzOperandBytes || len(bBytes) > maxFuzzOperandBytes {
			return
		}
		a := decFromFuzzBytes(aBytes, aNegative)
		b := decFromFuzzBytes(bBytes, bNegative)
		op := checkedOps[int(opIndex)%len(checkedOps)]

		// Checked arithmetic is deliberately stricter than LegacyDec about its
		// inputs: LegacyDec guards only results, so it consumes an operand that
		// is already outside the valid range without complaint.
		if !a.IsInValidRange() || !b.IsInValidRange() {
			_, err := op.checked(cloneDec(a), cloneDec(b))
			require.ErrorIs(t, err, decimal.ErrOutOfRange, "%s(%s, %s)", op.name, a, b)
			return
		}

		expected, panicked := stockResult(op.stock, cloneDec(a), cloneDec(b))
		actual, err := op.checked(cloneDec(a), cloneDec(b))
		if panicked {
			require.Error(t, err, "%s(%s, %s) panics in LegacyDec but succeeded when checked", op.name, a, b)
			return
		}

		require.NoError(t, err, "%s(%s, %s) succeeds in LegacyDec but failed when checked", op.name, a, b)
		require.True(t, expected.Equal(actual), "%s(%s, %s): expected %s, got %s", op.name, a, b, expected, actual)
	})
}

// addFuzzSeeds pins the corpus to the values that decide the package's
// behaviour: both sides of every fast-path bound, half-to-even ties on the
// direct and the wide path, the extremes of the valid range, and the operands
// each guard clause rejects.
func addFuzzSeeds(f *testing.F) {
	f.Helper()

	var (
		one   = big.NewInt(1)
		three = big.NewInt(3)
		// 0.5, the multiplier that turns an odd raw operand into an exact
		// half-to-even tie.
		half = big.NewInt(500_000_000_000_000_000)
		// The widest operands each fast path admits, and the first value past
		// each bound: 314 bits for add and subtract, a combined 374 bits for
		// multiply, a 254-bit difference for divide.
		maxAddSubOperand    = bitValueMinusOne(314)
		beyondAddSubOperand = bitValue(314)
		maxMulOperand       = bitValueMinusOne(187)
		beyondMulOperand    = bitValue(187)
		maxQuoNumerator     = bitValueMinusOne(255)
		beyondQuoNumerator  = bitValue(255)
		// A 61-bit divisor takes a 316-bit numerator just past the divide bound
		// while keeping the quotient representable.
		wideQuoDivisor = new(big.Int).Lsh(three, 59)
		// Odd raw operands above the multiply bound: halving 2^315+1 lands on an
		// even quotient and rounds down, 2^315+3 lands on an odd one and rounds up.
		tieRoundsDown = new(big.Int).Add(bitValue(315), one)
		tieRoundsUp   = new(big.Int).Add(bitValue(315), three)
		maxRaw        = maxLegacyDec().BigIntMut()
		beyondMaxRaw  = bitValue(316)
		two           = big.NewInt(2_000_000_000_000_000_000)
	)

	seeds := []struct {
		a, b                 *big.Int
		aNegative, bNegative bool
		op                   byte
	}{
		// Add and subtract: on the bound, then past it.
		{a: maxAddSubOperand, b: maxAddSubOperand, op: 0},
		{a: beyondAddSubOperand, b: one, op: 0},
		{a: maxAddSubOperand, b: maxAddSubOperand, bNegative: true, op: 1},
		{a: beyondAddSubOperand, b: one, op: 1},

		// Multiply: on the bound, past it, then the ties either path must round
		// the same way.
		{a: maxMulOperand, b: maxMulOperand, op: 2},
		{a: beyondMulOperand, b: beyondMulOperand, op: 2},
		{a: three, b: half, op: 2},
		{a: three, b: half, aNegative: true, op: 2},
		{a: tieRoundsDown, b: half, op: 2},
		{a: tieRoundsUp, b: half, op: 2},
		{a: tieRoundsUp, b: half, aNegative: true, op: 2},

		// Divide: on the bound, past it, and past it with a remainder to round.
		{a: maxQuoNumerator, b: one, op: 3},
		{a: beyondQuoNumerator, b: one, op: 3},
		{a: bitValue(315), b: wideQuoDivisor, op: 3},
		{a: bitValue(315), b: wideQuoDivisor, aNegative: true, op: 3},

		// Results LegacyDec cannot represent, which must surface as errors.
		{a: maxRaw, b: one, op: 0},
		{a: maxRaw, b: one, aNegative: true, bNegative: true, op: 1},
		{a: maxRaw, b: two, op: 2},
		{a: maxRaw, b: one, op: 3},

		// Divisor of zero, and an operand that is already out of range.
		{a: maxRaw, b: new(big.Int), op: 3},
		{a: beyondMaxRaw, b: one, op: 0},
	}

	for _, seed := range seeds {
		f.Add(seed.a.Bytes(), seed.b.Bytes(), seed.aNegative, seed.bNegative, seed.op)
	}
}

// decFromFuzzBytes reads a fuzz operand as a raw LegacyDec value, so the corpus
// addresses the underlying integer directly rather than through decimal strings.
func decFromFuzzBytes(raw []byte, negative bool) math.LegacyDec {
	value := new(big.Int).SetBytes(raw)
	if negative {
		value.Neg(value)
	}

	return math.LegacyNewDecFromBigIntWithPrec(value, math.LegacyPrecision)
}

// cloneDec hands each call its own copy, so neither implementation can observe
// the other's mutations. LegacyNewDecFromBigIntWithPrec allocates a new integer.
func cloneDec(value math.LegacyDec) math.LegacyDec {
	return math.LegacyNewDecFromBigIntWithPrec(value.BigIntMut(), math.LegacyPrecision)
}

// stockResult reports whether the unchecked LegacyDec operation panics, which is
// the behaviour checked arithmetic replaces with an error.
func stockResult(
	op func(math.LegacyDec, math.LegacyDec) math.LegacyDec,
	a, b math.LegacyDec,
) (result math.LegacyDec, panicked bool) {
	defer func() {
		if recover() != nil {
			panicked = true
		}
	}()

	return op(a, b), false
}

func bitValue(bits uint) *big.Int {
	return new(big.Int).Lsh(big.NewInt(1), bits)
}

func bitValueMinusOne(bits uint) *big.Int {
	return new(big.Int).Sub(bitValue(bits), big.NewInt(1))
}
