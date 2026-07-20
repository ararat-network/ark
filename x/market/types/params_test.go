package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
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
			name: "nil base pool",
			mutate: func(p *types.Params) {
				p.BasePool = sdk.DecCoin{}
			},
			expectErr: "base pool amount must be set",
		},
		{
			name: "nil base pool amount with valid denom",
			mutate: func(p *types.Params) {
				p.BasePool = sdk.DecCoin{Denom: chain.MicroSDRDenom}
			},
			expectErr: "base pool amount must be set",
		},
		{
			name: "zero base pool",
			mutate: func(p *types.Params) {
				p.BasePool = sdk.NewDecCoin(chain.MicroSDRDenom, math.ZeroInt())
			},
			expectErr: "base pool must be positive",
		},
		{
			name: "negative base pool",
			mutate: func(p *types.Params) {
				p.BasePool = sdk.DecCoin{
					Denom:  chain.MicroSDRDenom,
					Amount: math.LegacyNewDec(-1),
				}
			},
			expectErr: "invalid base pool",
		},
		{
			name: "base pool square is out of range",
			mutate: func(p *types.Params) {
				p.BasePool = sdk.NewDecCoinFromDec(chain.MicroSDRDenom, maxLegacyDec())
			},
			expectErr: "base pool square must be representable",
		},
		{
			name: "zero pool recovery period",
			mutate: func(p *types.Params) {
				p.PoolRecoveryPeriod = 0
			},
			expectErr: "pool recovery period must be positive",
		},
		{
			name: "nil min stability spread",
			mutate: func(p *types.Params) {
				p.MinStabilitySpread = math.LegacyDec{}
			},
			expectErr: "min stability spread must be set",
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
