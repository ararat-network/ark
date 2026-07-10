package encoding_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"ark/pkg/encoding"
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
