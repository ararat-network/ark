package encoding_test

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/pkg/encoding"
)

func TestEncodeDecodeCompactLegacyDec(t *testing.T) {
	tests := []struct {
		name  string
		value math.LegacyDec
	}{
		{name: "fractional", value: math.LegacyMustNewDecFromStr("1.23")},
		{name: "smallest positive", value: math.LegacyNewDecWithPrec(1, math.LegacyPrecision)},
		{
			name: "largest in range",
			value: math.LegacyNewDecFromBigIntWithPrec(
				new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)),
				0,
			),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := encoding.EncodeCompactLegacyDec(tc.value)
			require.NoError(t, err)
			require.LessOrEqual(t, len(encoded), encoding.MaxEncodedCompactLegacyDecBytes)

			decoded, err := encoding.DecodeCompactLegacyDec(encoded)
			require.NoError(t, err)
			require.True(t, tc.value.Equal(decoded))
		})
	}
}

func TestEncodeCompactLegacyDecRejectsInvalidValues(t *testing.T) {
	outOfRange := new(big.Int).Lsh(big.NewInt(1), 256)
	outOfRange.Mul(outOfRange, new(big.Int).Exp(big.NewInt(10), big.NewInt(math.LegacyPrecision), nil))

	tests := []struct {
		name   string
		value  math.LegacyDec
		errStr string
	}{
		{name: "nil", value: math.LegacyDec{}, errStr: "nil LegacyDec"},
		{name: "zero", value: math.LegacyZeroDec(), errStr: "not positive"},
		{name: "negative", value: math.LegacyNewDec(-1), errStr: "not positive"},
		{
			name:   "out of range",
			value:  math.LegacyNewDecFromBigIntWithPrec(outOfRange, math.LegacyPrecision),
			errStr: "out of range",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := encoding.EncodeCompactLegacyDec(tc.value)
			require.ErrorContains(t, err, tc.errStr)
		})
	}
}

func TestDecodeCompactLegacyDecRejectsNonCanonicalBytes(t *testing.T) {
	oversized := make([]byte, encoding.MaxEncodedCompactLegacyDecBytes+1)
	oversized[0] = 1

	tests := []struct {
		name   string
		bz     []byte
		errStr string
	}{
		{name: "empty", bz: []byte{}, errStr: "empty LegacyDec bytes"},
		{name: "leading zero", bz: []byte{0x00, 0x01}, errStr: "leading zero"},
		{name: "encoded zero", bz: []byte{0x00}, errStr: "leading zero"},
		{name: "oversized", bz: oversized, errStr: "exceeds maximum"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := encoding.DecodeCompactLegacyDec(tc.bz)
			require.ErrorContains(t, err, tc.errStr)
		})
	}
}

// The 40-byte cap admits every in-range raw value, so the range check is what
// rejects the top of the encodable space, not the length check.
func TestDecodeCompactLegacyDecRejectsOutOfRangeValue(t *testing.T) {
	atCap := make([]byte, encoding.MaxEncodedCompactLegacyDecBytes)
	for i := range atCap {
		atCap[i] = 0xFF
	}

	_, err := encoding.DecodeCompactLegacyDec(atCap)
	require.ErrorContains(t, err, "out of range")
}
