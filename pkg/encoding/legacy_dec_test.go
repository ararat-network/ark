package encoding_test

import (
	"encoding/hex"
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

// TestCompactLegacyDecBytesAreFixed pins the bytes themselves, which a round trip cannot: sidecars
// and vote extensions of other releases decode them, so a layout that still round-trips would break
// them all the same.
func TestCompactLegacyDecBytesAreFixed(t *testing.T) {
	tests := []struct {
		value string
		bytes string
	}{
		{value: "0.000000000000000001", bytes: "01"},
		{value: "1", bytes: "0de0b6b3a7640000"},
		{value: "0.729394602479941648", bytes: "0a1f5486c5e77810"},
		{value: "1371.25", bytes: "4a55eaa1ce5e650000"},
		{
			value: new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)).String(),
			bytes: "0de0b6b3a763fffffffffffffffffffffffffffffffffffffffffffffffffffff21f494c589c0000",
		},
	}

	for _, tc := range tests {
		t.Run(tc.value, func(t *testing.T) {
			value := math.LegacyMustNewDecFromStr(tc.value)
			encoded, err := encoding.EncodeCompactLegacyDec(value)
			require.NoError(t, err)
			require.Equal(t, tc.bytes, hex.EncodeToString(encoded))

			raw, err := hex.DecodeString(tc.bytes)
			require.NoError(t, err)
			decoded, err := encoding.DecodeCompactLegacyDec(raw)
			require.NoError(t, err)
			require.True(t, value.Equal(decoded))
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
