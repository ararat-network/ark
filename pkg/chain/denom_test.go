package chain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ark/pkg/chain"
)

func TestValidateNativeBaseDenom(t *testing.T) {
	tests := []struct {
		name    string
		denom   string
		wantErr string
	}{
		{
			name:  "canonical vote target",
			denom: "ausd",
		},
		{
			name:  "canonical base denom",
			denom: "anoah",
		},
		{
			name:    "empty denom",
			wantErr: "Ark-native base denom beginning with a",
		},
		{
			name:    "short denom",
			denom:   "a",
			wantErr: "Ark-native base denom beginning with a",
		},
		{
			name:    "uppercase denom",
			denom:   "aUSD",
			wantErr: "canonical lowercase Ark-native base denom",
		},
		{
			name:    "path denom",
			denom:   "afoo/bar",
			wantErr: "without path separators",
		},
		{
			name:    "invalid SDK denom",
			denom:   "a??",
			wantErr: "canonical lowercase Ark-native base denom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := chain.ValidateNativeBaseDenom(tt.denom)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestNativeBaseAmount(t *testing.T) {
	require.Equal(
		t,
		"1000000000000000000",
		chain.NativeBaseAmount(1).String(),
	)
	require.Equal(
		t,
		"1000000000000000000000000",
		chain.NativeBaseAmount(1_000_000).String(),
	)
}
