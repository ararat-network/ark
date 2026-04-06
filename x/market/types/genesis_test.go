package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"noah/x/market/types"
)

func TestValidateGenesisState(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*types.GenesisState)
		expectErr string
	}{
		{
			name: "nil noah pool delta",
			mutate: func(gs *types.GenesisState) {
				gs.NoahPoolDelta = math.LegacyDec{}
			},
			expectErr: "noah pool delta must not be nil",
		},
		{
			name:   "default genesis state",
			mutate: func(gs *types.GenesisState) {},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gs := types.DefaultGenesisState()
			tc.mutate(gs)
			err := gs.Validate()
			if tc.expectErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.ErrorContains(t, err, tc.expectErr)
			}
		})
	}
}
