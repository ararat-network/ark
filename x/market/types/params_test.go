package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"noah/x/market/types"
)

func TestParamsValidate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*types.Params)
		expectErr string
	}{
		{
			name:   "default is valid",
			mutate: func(p *types.Params) {},
		},
		// BasePool
		{
			name:      "base pool negative",
			mutate:    func(p *types.Params) { p.BasePool = math.LegacyNewDec(-1) },
			expectErr: "base pool must be positive or zero",
		},
		{
			name:   "base pool zero",
			mutate: func(p *types.Params) { p.BasePool = math.LegacyZeroDec() },
		},
		// PoolRecoveryPeriod
		{
			name:      "pool recovery period zero",
			mutate:    func(p *types.Params) { p.PoolRecoveryPeriod = 0 },
			expectErr: "pool recovery period must be positive",
		},
		{
			name:   "pool recovery period one",
			mutate: func(p *types.Params) { p.PoolRecoveryPeriod = 1 },
		},
		// MinStabilitySpread
		{
			name:      "min stability spread negative",
			mutate:    func(p *types.Params) { p.MinStabilitySpread = math.LegacyNewDecWithPrec(-1, 2) },
			expectErr: "min stability spread must be in [0, 1]",
		},
		{
			name:      "min stability spread above one",
			mutate:    func(p *types.Params) { p.MinStabilitySpread = math.LegacyNewDecWithPrec(101, 2) },
			expectErr: "min stability spread must be in [0, 1]",
		},
		{
			name:   "min stability spread at zero",
			mutate: func(p *types.Params) { p.MinStabilitySpread = math.LegacyZeroDec() },
		},
		{
			name:   "min stability spread at one",
			mutate: func(p *types.Params) { p.MinStabilitySpread = math.LegacyOneDec() },
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
