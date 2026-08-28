package decimal_test

import (
	"errors"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/pkg/decimal"
)

func TestCheckedArithmeticMatchesLegacyDec(t *testing.T) {
	directBoundary := math.LegacyNewDecFromBigIntWithPrec(
		new(big.Int).Lsh(big.NewInt(1), 313),
		math.LegacyPrecision,
	)
	outsideDirectBound := math.LegacyNewDecFromBigIntWithPrec(
		new(big.Int).Lsh(big.NewInt(1), 314),
		math.LegacyPrecision,
	)
	half := math.LegacyMustNewDecFromStr("0.5")
	// Halving an odd raw operand lands exactly on a tie. This one is wide enough
	// to leave the direct multiply path, and its quotient is odd, so bankers
	// rounding must round it up.
	wideTieOperand := math.LegacyNewDecFromBigIntWithPrec(
		new(big.Int).Add(bitValue(315), big.NewInt(3)),
		math.LegacyPrecision,
	)
	// A 61-bit divisor puts this 316-bit numerator past the direct divide bound
	// while leaving a remainder for the wide path to round.
	wideQuoNumerator := math.LegacyNewDecFromBigIntWithPrec(bitValue(315), math.LegacyPrecision)
	wideQuoDivisor := math.LegacyNewDecFromBigIntWithPrec(
		new(big.Int).Lsh(big.NewInt(3), 59),
		math.LegacyPrecision,
	)
	tests := []struct {
		name    string
		checked func(math.LegacyDec, math.LegacyDec) (math.LegacyDec, error)
		legacy  func(math.LegacyDec, math.LegacyDec) math.LegacyDec
		a       math.LegacyDec
		b       math.LegacyDec
	}{
		{
			name:    "add",
			checked: decimal.Add,
			legacy:  math.LegacyDec.Add,
			a:       math.LegacyMustNewDecFromStr("123.456"),
			b:       math.LegacyMustNewDecFromStr("78.9"),
		},
		{
			name:    "add at direct bound",
			checked: decimal.Add,
			legacy:  math.LegacyDec.Add,
			a:       directBoundary,
			b:       directBoundary,
		},
		{
			name:    "add representable result outside direct bound",
			checked: decimal.Add,
			legacy:  math.LegacyDec.Add,
			a:       outsideDirectBound,
			b:       math.LegacySmallestDec(),
		},
		{
			name:    "subtract",
			checked: decimal.Sub,
			legacy:  math.LegacyDec.Sub,
			a:       math.LegacyMustNewDecFromStr("123.456"),
			b:       math.LegacyMustNewDecFromStr("200.1"),
		},
		{
			name:    "subtract at direct bound",
			checked: decimal.Sub,
			legacy:  math.LegacyDec.Sub,
			a:       directBoundary,
			b:       directBoundary.Neg(),
		},
		{
			name:    "subtract representable result outside direct bound",
			checked: decimal.Sub,
			legacy:  math.LegacyDec.Sub,
			a:       outsideDirectBound,
			b:       math.LegacySmallestDec(),
		},
		{
			name:    "multiply",
			checked: decimal.Mul,
			legacy:  math.LegacyDec.Mul,
			a:       math.LegacyMustNewDecFromStr("123.456"),
			b:       math.LegacyMustNewDecFromStr("0.789"),
		},
		{
			name:    "multiply at direct bound",
			checked: decimal.Mul,
			legacy:  math.LegacyDec.Mul,
			a:       directBoundary,
			b:       math.LegacyOneDec(),
		},
		{
			name:    "multiply representable result outside direct bound",
			checked: decimal.Mul,
			legacy:  math.LegacyDec.Mul,
			a:       outsideDirectBound,
			b:       math.LegacyOneDec(),
		},
		{
			name:    "multiply uses bankers rounding",
			checked: decimal.Mul,
			legacy:  math.LegacyDec.Mul,
			a:       math.LegacyNewDecFromBigIntWithPrec(big.NewInt(3), math.LegacyPrecision),
			b:       half,
		},
		{
			name:    "multiply uses bankers rounding on negatives",
			checked: decimal.Mul,
			legacy:  math.LegacyDec.Mul,
			a:       math.LegacyNewDecFromBigIntWithPrec(big.NewInt(-3), math.LegacyPrecision),
			b:       half,
		},
		{
			name:    "multiply uses bankers rounding outside direct bound",
			checked: decimal.Mul,
			legacy:  math.LegacyDec.Mul,
			a:       wideTieOperand,
			b:       half,
		},
		{
			name:    "multiply uses bankers rounding outside direct bound on negatives",
			checked: decimal.Mul,
			legacy:  math.LegacyDec.Mul,
			a:       wideTieOperand.Neg(),
			b:       half,
		},
		{
			name:    "multiply negative",
			checked: decimal.Mul,
			legacy:  math.LegacyDec.Mul,
			a:       math.LegacyMustNewDecFromStr("-123.456"),
			b:       math.LegacyMustNewDecFromStr("0.789"),
		},
		{
			name:    "divide",
			checked: decimal.Quo,
			legacy:  math.LegacyDec.Quo,
			a:       math.LegacyMustNewDecFromStr("123.456"),
			b:       math.LegacyMustNewDecFromStr("0.789"),
		},
		{
			name:    "divide at direct bound",
			checked: decimal.Quo,
			legacy:  math.LegacyDec.Quo,
			a:       directBoundary,
			b:       math.LegacyOneDec(),
		},
		{
			name:    "divide negative",
			checked: decimal.Quo,
			legacy:  math.LegacyDec.Quo,
			a:       math.LegacyMustNewDecFromStr("-123.456"),
			b:       math.LegacyMustNewDecFromStr("0.789"),
		},
		{
			name:    "divide by negative",
			checked: decimal.Quo,
			legacy:  math.LegacyDec.Quo,
			a:       math.LegacyMustNewDecFromStr("123.456"),
			b:       math.LegacyMustNewDecFromStr("-0.789"),
		},
		{
			name:    "divide zero",
			checked: decimal.Quo,
			legacy:  math.LegacyDec.Quo,
			a:       math.LegacyZeroDec(),
			b:       math.LegacyOneDec(),
		},
		{
			name:    "divide rounds to zero",
			checked: decimal.Quo,
			legacy:  math.LegacyDec.Quo,
			a:       math.LegacySmallestDec(),
			b:       math.LegacyNewDec(3),
		},
		{
			name:    "divide rounds outside direct bound",
			checked: decimal.Quo,
			legacy:  math.LegacyDec.Quo,
			a:       wideQuoNumerator,
			b:       wideQuoDivisor,
		},
		{
			name:    "divide rounds outside direct bound on negatives",
			checked: decimal.Quo,
			legacy:  math.LegacyDec.Quo,
			a:       wideQuoNumerator.Neg(),
			b:       wideQuoDivisor,
		},
		{
			name:    "divide representable result outside direct bound",
			checked: decimal.Quo,
			legacy:  math.LegacyDec.Quo,
			a: math.LegacyNewDecFromBigInt(
				new(big.Int).Lsh(big.NewInt(1), 255),
			),
			b: math.LegacyOneDec(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expected := tc.legacy(tc.a, tc.b)
			actual, err := tc.checked(tc.a, tc.b)
			require.NoError(t, err)
			require.True(t, expected.Equal(actual), "expected %s, got %s", expected, actual)
		})
	}
}

func TestCheckedArithmeticReturnsErrors(t *testing.T) {
	max := maxLegacyDec()
	tests := []struct {
		name      string
		operation func() error
		expectErr error
	}{
		{
			name: "nil operand",
			operation: func() error {
				_, err := decimal.Add(math.LegacyDec{}, math.LegacyOneDec())
				return err
			},
			expectErr: decimal.ErrNil,
		},
		{
			name: "add overflow",
			operation: func() error {
				_, err := decimal.Add(max, math.LegacySmallestDec())
				return err
			},
			expectErr: decimal.ErrOutOfRange,
		},
		{
			name: "subtract overflow",
			operation: func() error {
				_, err := decimal.Sub(max.Neg(), math.LegacySmallestDec())
				return err
			},
			expectErr: decimal.ErrOutOfRange,
		},
		{
			name: "multiply overflow",
			operation: func() error {
				_, err := decimal.Mul(max, math.LegacyNewDec(2))
				return err
			},
			expectErr: decimal.ErrOutOfRange,
		},
		{
			name: "divide overflow",
			operation: func() error {
				_, err := decimal.Quo(max, math.LegacySmallestDec())
				return err
			},
			expectErr: decimal.ErrOutOfRange,
		},
		{
			name: "divide by zero",
			operation: func() error {
				_, err := decimal.Quo(math.LegacyOneDec(), math.LegacyZeroDec())
				return err
			},
			expectErr: decimal.ErrDivisionByZero,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.NotPanics(t, func() {
				err := tc.operation()
				require.True(t, errors.Is(err, tc.expectErr), "expected %v, got %v", tc.expectErr, err)
			})
		})
	}
}

var (
	benchmarkResult math.LegacyDec
	benchmarkErr    error
)

func BenchmarkCheckedArithmetic(b *testing.B) {
	a := math.LegacyMustNewDecFromStr("123.456")
	other := math.LegacyMustNewDecFromStr("0.789")
	benchmarks := []struct {
		name      string
		operation func(math.LegacyDec, math.LegacyDec) (math.LegacyDec, error)
	}{
		{name: "add", operation: decimal.Add},
		{name: "subtract", operation: decimal.Sub},
		{name: "multiply", operation: decimal.Mul},
		{name: "divide", operation: decimal.Quo},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			var result math.LegacyDec
			var err error
			b.ReportAllocs()
			for b.Loop() {
				result, err = bm.operation(a, other)
			}
			benchmarkResult = result
			benchmarkErr = err
		})
	}
}

func maxLegacyDec() math.LegacyDec {
	precision := new(big.Int).Exp(big.NewInt(10), big.NewInt(math.LegacyPrecision), nil)
	raw := new(big.Int).Lsh(big.NewInt(1), 256)
	raw.Mul(raw, precision)
	raw.Sub(raw, big.NewInt(1))
	return math.LegacyNewDecFromBigIntWithPrec(raw, math.LegacyPrecision)
}
