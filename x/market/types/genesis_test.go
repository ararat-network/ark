package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"noah/x/market/types"
)

func TestValidateGenesis(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*types.GenesisState)
		expectErr string
	}{
		{
			name:   "default is valid",
			mutate: func(gs *types.GenesisState) {},
		},
		{
			name: "nil noah pool delta",
			mutate: func(gs *types.GenesisState) {
				gs.NoahPoolDelta = math.LegacyDec{}
			},
			expectErr: "noah pool delta must not be nil",
		},
		{
			name: "custom valid genesis",
			mutate: func(gs *types.GenesisState) {
				*gs = *types.NewGenesisState(
					math.LegacyNewDec(500),
					types.DefaultParams(),
				)
			},
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
