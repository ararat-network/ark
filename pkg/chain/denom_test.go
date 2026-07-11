package chain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ark/pkg/chain"
)

func TestValidateMicroDenom(t *testing.T) {
	tests := []struct {
		name    string
		denom   string
		wantErr string
	}{
		{
			name:  "canonical vote target",
			denom: "uusd",
		},
		{
			name:  "canonical base denom",
			denom: "unoah",
		},
		{
			name:    "empty denom",
			wantErr: "micro denom beginning with u",
		},
		{
			name:    "short denom",
			denom:   "ua",
			wantErr: "micro denom beginning with u",
		},
		{
			name:    "uppercase denom",
			denom:   "uUSD",
			wantErr: "canonical lowercase micro denom",
		},
		{
			name:    "path denom",
			denom:   "ufoo/bar",
			wantErr: "without path separators",
		},
		{
			name:    "invalid SDK denom",
			denom:   "u??",
			wantErr: "canonical lowercase micro denom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := chain.ValidateMicroDenom(tt.denom)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
		})
	}
}
