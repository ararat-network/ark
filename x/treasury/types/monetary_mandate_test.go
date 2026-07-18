package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"ark/x/treasury/types"
)

func TestMonetaryMandateValidate(t *testing.T) {
	tests := []struct {
		name      string
		mandate   func() types.MonetaryMandate
		mutate    func(*types.MonetaryMandate)
		expectErr string
	}{
		{name: "disabled", mandate: types.DefaultMonetaryMandate, mutate: func(*types.MonetaryMandate) {}},
		{name: "configured", mandate: validMonetaryMandate, mutate: func(*types.MonetaryMandate) {}},
		{
			name: "configured zero term", mandate: validMonetaryMandate,
			mutate: func(m *types.MonetaryMandate) { m.Term = 0 }, expectErr: "term must be positive",
		},
		{
			name: "invalid committee", mandate: validMonetaryMandate,
			mutate: func(m *types.MonetaryMandate) { m.Committee = "invalid" }, expectErr: "committee",
		},
		{
			name: "empty interval", mandate: validMonetaryMandate,
			mutate: func(m *types.MonetaryMandate) { m.ExpiryHeight = m.ActivationHeight }, expectErr: "must precede",
		},
		{
			name: "inverted bound", mandate: validMonetaryMandate,
			mutate: func(m *types.MonetaryMandate) {
				m.MinimumPolicy.InsuranceTargetRatio = math.LegacyMustNewDecFromStr("0.9")
			}, expectErr: "outside mandate range",
		},
		{
			name: "disabled with heights", mandate: types.DefaultMonetaryMandate,
			mutate: func(m *types.MonetaryMandate) { m.ExpiryHeight = 1 }, expectErr: "disabled",
		},
		{
			name: "disabled with policy power", mandate: types.DefaultMonetaryMandate,
			mutate: func(m *types.MonetaryMandate) {
				m.MaximumPolicy.ValidatorBlockRewardTarget = math.OneInt()
			}, expectErr: "identical zero bounds",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mandate := tc.mandate()
			tc.mutate(&mandate)
			err := mandate.Validate()
			if tc.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.expectErr)
		})
	}
}

func TestMonetaryMandateActivation(t *testing.T) {
	mandate := validMonetaryMandate()
	require.False(t, mandate.IsActive(9))
	require.True(t, mandate.IsActive(10))
	require.True(t, mandate.IsActive(19))
	require.False(t, mandate.IsActive(20))
}

func TestMonetaryMandateCandidateBounds(t *testing.T) {
	mandate := validMonetaryMandate()
	require.NoError(t, mandate.ValidatePolicy(boundedCandidatePolicy()))

	tests := []struct {
		name   string
		mutate func(*types.MonetaryPolicy)
	}{
		{name: "tax rate", mutate: func(p *types.MonetaryPolicy) { p.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.11") }},
		{name: "validator rewards", mutate: func(p *types.MonetaryPolicy) { p.ValidatorBlockRewardTarget = math.NewInt(11) }},
		{name: "Oracle rewards", mutate: func(p *types.MonetaryPolicy) { p.OracleBlockRewardTarget = math.NewInt(11) }},
		{name: "Buffer ratio", mutate: func(p *types.MonetaryPolicy) { p.RedemptionBufferTargetRatio = math.LegacyMustNewDecFromStr("0.51") }},
		{name: "Reserve ratio", mutate: func(p *types.MonetaryPolicy) { p.StrategicReserveTargetRatio = math.LegacyMustNewDecFromStr("0.51") }},
		{name: "Insurance ratio", mutate: func(p *types.MonetaryPolicy) { p.InsuranceTargetRatio = math.LegacyMustNewDecFromStr("0.51") }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			policy := boundedCandidatePolicy()
			tc.mutate(&policy)
			require.Error(t, mandate.ValidatePolicy(policy))
		})
	}
}

func validMonetaryMandate() types.MonetaryMandate {
	minimum := types.DefaultMonetaryPolicy()
	maximum := types.MonetaryPolicy{
		StabilityTaxRate:            math.LegacyMustNewDecFromStr("0.1"),
		ValidatorBlockRewardTarget:  math.NewInt(10),
		OracleBlockRewardTarget:     math.NewInt(10),
		RedemptionBufferTargetRatio: math.LegacyMustNewDecFromStr("0.5"),
		StrategicReserveTargetRatio: math.LegacyMustNewDecFromStr("0.5"),
		InsuranceTargetRatio:        math.LegacyMustNewDecFromStr("0.5"),
	}
	return types.MonetaryMandate{
		Term:             1,
		Committee:        testAddress(9),
		ActivationHeight: 10,
		ExpiryHeight:     20,
		MinimumPolicy:    minimum,
		MaximumPolicy:    maximum,
	}
}

func boundedCandidatePolicy() types.MonetaryPolicy {
	return types.MonetaryPolicy{
		StabilityTaxRate:            math.LegacyMustNewDecFromStr("0.05"),
		ValidatorBlockRewardTarget:  math.NewInt(5),
		OracleBlockRewardTarget:     math.NewInt(5),
		RedemptionBufferTargetRatio: math.LegacyMustNewDecFromStr("0.25"),
		StrategicReserveTargetRatio: math.LegacyMustNewDecFromStr("0.25"),
		InsuranceTargetRatio:        math.LegacyMustNewDecFromStr("0.25"),
	}
}
