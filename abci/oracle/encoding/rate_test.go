package encoding_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	oracleencoding "ark/abci/oracle/encoding"
)

func TestEncodeDecodeRate(t *testing.T) {
	rate := math.LegacyMustNewDecFromStr("1.23")

	encoded, err := oracleencoding.EncodeRate(rate)
	require.NoError(t, err)

	decoded, err := oracleencoding.DecodeRate(encoded)
	require.NoError(t, err)
	require.True(t, rate.Equal(decoded))
}

func TestDecodeRateRejectsOversizedBytes(t *testing.T) {
	_, err := oracleencoding.DecodeRate(make([]byte, oracleencoding.MaxRateBytes+1))
	require.ErrorContains(t, err, "exceeds maximum")
}

func TestRateValidation(t *testing.T) {
	t.Run("nil rate", func(t *testing.T) {
		_, err := oracleencoding.EncodeRate(math.LegacyDec{})
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
			_, err := oracleencoding.DecodeRate(tc.bz)
			require.Error(t, err)
		})
	}
}
