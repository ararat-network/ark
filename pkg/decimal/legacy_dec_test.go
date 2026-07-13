package decimal_test

import (
	"errors"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"ark/pkg/decimal"
)

func TestCheckedArithmeticMatchesLegacyDec(t *testing.T) {
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
			name:    "subtract",
			checked: decimal.Sub,
			legacy:  math.LegacyDec.Sub,
			a:       math.LegacyMustNewDecFromStr("123.456"),
			b:       math.LegacyMustNewDecFromStr("200.1"),
		},
		{
			name:    "multiply",
			checked: decimal.Mul,
			legacy:  math.LegacyDec.Mul,
			a:       math.LegacyMustNewDecFromStr("123.456"),
			b:       math.LegacyMustNewDecFromStr("0.789"),
		},
		{
			name:    "multiply uses bankers rounding",
			checked: decimal.Mul,
			legacy:  math.LegacyDec.Mul,
			a:       math.LegacyNewDecFromBigIntWithPrec(big.NewInt(3), math.LegacyPrecision),
			b:       math.LegacyMustNewDecFromStr("0.5"),
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
			name:    "divide negative",
			checked: decimal.Quo,
			legacy:  math.LegacyDec.Quo,
			a:       math.LegacyMustNewDecFromStr("-123.456"),
			b:       math.LegacyMustNewDecFromStr("0.789"),
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

func maxLegacyDec() math.LegacyDec {
	precision := new(big.Int).Exp(big.NewInt(10), big.NewInt(math.LegacyPrecision), nil)
	raw := new(big.Int).Lsh(big.NewInt(1), 256)
	raw.Mul(raw, precision)
	raw.Sub(raw, big.NewInt(1))
	return math.LegacyNewDecFromBigIntWithPrec(raw, math.LegacyPrecision)
}
