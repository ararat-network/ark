package encoding

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
)

func TestEncodeDecodeRate(t *testing.T) {
	rate := math.LegacyMustNewDecFromStr("1.23")

	encoded, err := EncodeRate(rate)
	require.NoError(t, err)

	decoded, err := DecodeRate(encoded)
	require.NoError(t, err)
	require.True(t, rate.Equal(decoded))
}

func TestDecodeRateValidation(t *testing.T) {
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
		{
			name: "over max bytes",
			bz:   make([]byte, MaxRateBytes+1),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeRate(tc.bz)
			require.Error(t, err)
		})
	}
}
