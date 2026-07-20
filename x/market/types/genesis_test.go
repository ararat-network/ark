package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"ark/x/market/types"
)

func TestValidateGenesisState(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*types.GenesisState)
		expectErr string
	}{
		{
			name: "nil ark pool delta",
			mutate: func(gs *types.GenesisState) {
				gs.ArkPoolDelta = math.LegacyDec{}
			},
			expectErr: "ark pool delta must not be nil",
		},
		{
			name:   "default genesis state",
			mutate: func(gs *types.GenesisState) {},
		},
		{
			name: "non-positive effective ark pool",
			mutate: func(gs *types.GenesisState) {
				gs.ArkPoolDelta = gs.Params.BasePool.Amount.Neg()
			},
			expectErr: "effective ark pool must be positive",
		},
		{
			name: "effective ark pool addition is out of range",
			mutate: func(gs *types.GenesisState) {
				gs.ArkPoolDelta = maxLegacyDec()
			},
			expectErr: "effective ark pool",
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
