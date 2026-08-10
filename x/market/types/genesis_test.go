package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	chain "ark/pkg/chain"
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
			name: "default genesis carries the MNT override",
			mutate: func(gs *types.GenesisState) {
				require.Len(t, gs.TobinTaxOverrides, 1)
				require.Equal(t, chain.MNTBaseDenom, gs.TobinTaxOverrides[0].Denom)
				require.True(t,
					types.DefaultTobinTax.MulInt64(8).Equal(gs.TobinTaxOverrides[0].TobinTax),
				)
			},
		},
		{
			name: "override on an unpriceable denom",
			mutate: func(gs *types.GenesisState) {
				gs.TobinTaxOverrides = []types.TobinTaxOverride{
					{Denom: chain.NoahBaseDenom, TobinTax: types.DefaultTobinTax},
				}
			},
			expectErr: "is the numeraire and is never priced",
		},
		{
			name: "override with an out-of-range rate",
			mutate: func(gs *types.GenesisState) {
				gs.TobinTaxOverrides = []types.TobinTaxOverride{
					{Denom: chain.USDBaseDenom, TobinTax: math.LegacyOneDec()},
				}
			},
			expectErr: "tobin tax must be in [0, 1)",
		},
		{
			name: "unsorted overrides",
			mutate: func(gs *types.GenesisState) {
				gs.TobinTaxOverrides = []types.TobinTaxOverride{
					{Denom: chain.USDBaseDenom, TobinTax: types.DefaultTobinTax},
					{Denom: chain.KRWBaseDenom, TobinTax: types.DefaultTobinTax},
				}
			},
			expectErr: "must be sorted by unique denom",
		},
		{
			name: "duplicate overrides",
			mutate: func(gs *types.GenesisState) {
				gs.TobinTaxOverrides = []types.TobinTaxOverride{
					{Denom: chain.USDBaseDenom, TobinTax: types.DefaultTobinTax},
					{Denom: chain.USDBaseDenom, TobinTax: types.DefaultTobinTax},
				}
			},
			expectErr: "must be sorted by unique denom",
		},
		{
			name: "non-positive effective ark pool",
			mutate: func(gs *types.GenesisState) {
				gs.ArkPoolDelta = gs.ConversionPolicy.BasePool.Amount.Neg()
			},
			expectErr: "effective ark pool must be positive",
		},
		{
			name: "invalid conversion policy",
			mutate: func(gs *types.GenesisState) {
				gs.ConversionPolicy.PoolRecoveryPeriod = 0
			},
			expectErr: "invalid conversion policy: pool recovery period must be between one and",
		},
		{
			name: "invalid conversion mandate",
			mutate: func(gs *types.GenesisState) {
				gs.ConversionMandate.MaximumPolicy = types.DefaultConversionPolicy()
			},
			expectErr: "disabled conversion mandate must use identical zero bounds",
		},
		{
			// This is the shape a reference re-point leaves behind: the pool
			// rebased into the new unit, the corridor still in the unit
			// governance appointed it in. It is a state the chain reaches on its
			// own and therefore has to be able to restart from, so genesis
			// accepts it. The corridor authorizes nothing until governance
			// re-appoints, which is what makes accepting it safe.
			name: "enabled mandate stranded off the genesis pool unit",
			mutate: func(gs *types.GenesisState) {
				bound := types.ConversionPolicy{
					BasePool:           sdk.NewDecCoin(chain.USDBaseDenom, chain.NativeBaseAmount(1)),
					PoolRecoveryPeriod: types.DefaultPoolRecoveryPeriod,
					MinStabilitySpread: types.DefaultMinStabilitySpread,
				}
				gs.ConversionMandate = types.NewDisabledConversionMandate(1)
				gs.ConversionMandate.Committee = authtypes.NewModuleAddress("capacity").String()
				gs.ConversionMandate.ActivationHeight = 1
				gs.ConversionMandate.ExpiryHeight = 2
				gs.ConversionMandate.MinimumPolicy = bound
				gs.ConversionMandate.MaximumPolicy = bound
			},
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
