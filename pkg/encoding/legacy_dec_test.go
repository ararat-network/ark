package encoding_test

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/pkg/encoding"
)

func TestEncodeDecodeLegacyDec(t *testing.T) {
	value := math.LegacyMustNewDecFromStr("1.23")

	encoded, err := encoding.EncodeLegacyDec(value)
	require.NoError(t, err)

	decoded, err := encoding.DecodeLegacyDec(encoded)
	require.NoError(t, err)
	require.True(t, value.Equal(decoded))
}

func TestLegacyDecValidation(t *testing.T) {
	t.Run("nil value", func(t *testing.T) {
		_, err := encoding.EncodeLegacyDec(math.LegacyDec{})
		require.Error(t, err)
	})

	tests := []struct {
		name string
		bz   []byte
	}{
		{
			name: "empty bytes",
			bz:   []byte{},
		},
		{
			name: "malformed bytes",
			bz:   []byte("not-a-dec"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := encoding.DecodeLegacyDec(tc.bz)
			require.Error(t, err)
		})
	}
}

func TestEncodeLegacyDecRejectsOutOfRangeValue(t *testing.T) {
	raw := new(big.Int).Lsh(big.NewInt(1), 256)
	raw.Mul(raw, new(big.Int).Exp(big.NewInt(10), big.NewInt(math.LegacyPrecision), nil))
	value := math.LegacyNewDecFromBigIntWithPrec(raw, math.LegacyPrecision)

	_, err := encoding.EncodeLegacyDec(value)

	require.ErrorContains(t, err, "out of range")
}

func TestDecodeLegacyDecRejectsOversizedInput(t *testing.T) {
	_, err := encoding.DecodeLegacyDec(make([]byte, encoding.MaxEncodedLegacyDecBytes+1))

	require.ErrorContains(t, err, "exceeds maximum")
}
