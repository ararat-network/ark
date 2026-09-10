package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/market/types"
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
			name: "default genesis carries no override",
			mutate: func(gs *types.GenesisState) {
				require.Empty(t, gs.TobinTaxOverrides)
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
			// A rebased pool with an unchanged corridor is a valid export/import state. The
			// mismatched mandate grants no policy authority.
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
