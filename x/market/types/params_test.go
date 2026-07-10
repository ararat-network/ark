package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"ark/x/market/types"
)

func TestValidateParams(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*types.Params)
		expectErr string
	}{
		{
			name:   "default params",
			mutate: func(p *types.Params) {},
		},
		{
			name: "negative base pool",
			mutate: func(p *types.Params) {
				p.BasePool = math.LegacyNewDec(-1)
			},
			expectErr: "base pool must be positive or zero",
		},
		{
			name: "zero pool recovery period",
			mutate: func(p *types.Params) {
				p.PoolRecoveryPeriod = 0
			},
			expectErr: "pool recovery period must be positive",
		},
		{
			name: "negative min stability spread",
			mutate: func(p *types.Params) {
				p.MinStabilitySpread = math.LegacyNewDec(-1)
			},
			expectErr: "min stability spread must be in [0, 1]",
		},
		{
			name: "min stability spread greater than 1",
			mutate: func(p *types.Params) {
				p.MinStabilitySpread = math.LegacyNewDecWithPrec(101, 2)
			},
			expectErr: "min stability spread must be in [0, 1]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := types.DefaultParams()
			tc.mutate(&p)
			err := p.Validate()
			if tc.expectErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.ErrorContains(t, err, tc.expectErr)
			}
		})
	}
}
