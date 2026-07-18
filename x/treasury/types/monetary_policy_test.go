package types_test

import (
	"testing"

	"cosmossdk.io/math"

	"github.com/stretchr/testify/require"

	"ark/x/treasury/types"
)

func TestMonetaryPolicyValidate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*types.MonetaryPolicy)
		expectErr string
	}{
		{name: "default is valid", mutate: func(*types.MonetaryPolicy) {}},
		{
			name: "independent ratios may sum above one",
			mutate: func(p *types.MonetaryPolicy) {
				p.RedemptionBufferTargetRatio = math.LegacyOneDec()
				p.StrategicReserveTargetRatio = math.LegacyOneDec()
				p.InsuranceTargetRatio = math.LegacyOneDec()
			},
		},
		{name: "nil tax rate", mutate: func(p *types.MonetaryPolicy) { p.StabilityTaxRate = math.LegacyDec{} }, expectErr: "StabilityTaxRate must be set"},
		{name: "negative tax rate", mutate: func(p *types.MonetaryPolicy) { p.StabilityTaxRate = math.LegacyNewDec(-1) }, expectErr: "StabilityTaxRate must be between zero and one"},
		{name: "tax rate above one", mutate: func(p *types.MonetaryPolicy) {
			p.StabilityTaxRate = math.LegacyNewDecWithPrec(1001, 3)
		}, expectErr: "StabilityTaxRate must be between zero and one"},
		{name: "nil validator target", mutate: func(p *types.MonetaryPolicy) { p.ValidatorBlockRewardTarget = math.Int{} }, expectErr: "ValidatorBlockRewardTarget must be set"},
		{name: "negative validator target", mutate: func(p *types.MonetaryPolicy) { p.ValidatorBlockRewardTarget = math.NewInt(-1) }, expectErr: "ValidatorBlockRewardTarget must be zero or positive"},
		{name: "nil Oracle target", mutate: func(p *types.MonetaryPolicy) { p.OracleBlockRewardTarget = math.Int{} }, expectErr: "OracleBlockRewardTarget must be set"},
		{name: "negative Oracle target", mutate: func(p *types.MonetaryPolicy) { p.OracleBlockRewardTarget = math.NewInt(-1) }, expectErr: "OracleBlockRewardTarget must be zero or positive"},
		{name: "nil Buffer ratio", mutate: func(p *types.MonetaryPolicy) { p.RedemptionBufferTargetRatio = math.LegacyDec{} }, expectErr: "RedemptionBufferTargetRatio must be set"},
		{name: "negative Reserve ratio", mutate: func(p *types.MonetaryPolicy) { p.StrategicReserveTargetRatio = math.LegacyNewDec(-1) }, expectErr: "StrategicReserveTargetRatio must be between zero and one"},
		{name: "Insurance ratio above one", mutate: func(p *types.MonetaryPolicy) {
			p.InsuranceTargetRatio = math.LegacyNewDecWithPrec(1001, 3)
		}, expectErr: "InsuranceTargetRatio must be between zero and one"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			policy := types.DefaultMonetaryPolicy()
			tc.mutate(&policy)
			err := policy.Validate()
			if tc.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.expectErr)
		})
	}
}
