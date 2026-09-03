package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/pkg/mandate"
	"github.com/ararat-network/ark/x/treasury/types"
)

func TestEconomicMandateValidate(t *testing.T) {
	tests := []struct {
		name      string
		mandate   func() types.EconomicMandate
		mutate    func(*types.EconomicMandate)
		expectErr string
	}{
		{name: "disabled", mandate: types.DefaultEconomicMandate, mutate: func(*types.EconomicMandate) {}},
		{name: "configured", mandate: validEconomicMandate, mutate: func(*types.EconomicMandate) {}},
		{
			name: "configured zero term", mandate: validEconomicMandate,
			mutate: func(m *types.EconomicMandate) { m.Term = 0 }, expectErr: "term must be positive",
		},
		{
			name: "invalid committee", mandate: validEconomicMandate,
			mutate: func(m *types.EconomicMandate) { m.Committee = "invalid" }, expectErr: "committee",
		},
		{
			name: "empty interval", mandate: validEconomicMandate,
			mutate: func(m *types.EconomicMandate) { m.ExpiryHeight = m.ActivationHeight }, expectErr: "must precede",
		},
		{
			name: "inverted bound", mandate: validEconomicMandate,
			mutate: func(m *types.EconomicMandate) {
				m.MinimumPolicy.InsuranceTargetRatio = math.LegacyMustNewDecFromStr("0.9")
			}, expectErr: "outside mandate range",
		},
		{
			name: "disabled with heights", mandate: types.DefaultEconomicMandate,
			mutate: func(m *types.EconomicMandate) { m.ExpiryHeight = 1 }, expectErr: "disabled",
		},
		{
			name: "disabled with policy power", mandate: types.DefaultEconomicMandate,
			mutate: func(m *types.EconomicMandate) {
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

func TestEconomicMandateActivation(t *testing.T) {
	mandate := validEconomicMandate()
	require.False(t, mandate.IsActive(9))
	require.True(t, mandate.IsActive(10))
	require.True(t, mandate.IsActive(19))
	require.False(t, mandate.IsActive(20))
}

func TestEconomicMandateCandidateBounds(t *testing.T) {
	mandate := validEconomicMandate()
	require.NoError(t, mandate.ValidatePolicy(boundedCandidatePolicy()))

	tests := []struct {
		name   string
		mutate func(*types.EconomicPolicy)
	}{
		{name: "tax rate", mutate: func(p *types.EconomicPolicy) { p.TransferTaxRate = math.LegacyMustNewDecFromStr("0.11") }},
		{name: "validator rewards", mutate: func(p *types.EconomicPolicy) { p.ValidatorBlockRewardTarget = math.NewInt(11) }},
		{name: "Oracle rewards", mutate: func(p *types.EconomicPolicy) { p.OracleBlockRewardTarget = math.NewInt(11) }},
		{name: "Buffer ratio", mutate: func(p *types.EconomicPolicy) { p.RedemptionBufferTargetRatio = math.LegacyMustNewDecFromStr("0.51") }},
		{name: "Reserve ratio", mutate: func(p *types.EconomicPolicy) { p.StrategicReserveTargetRatio = math.LegacyMustNewDecFromStr("0.51") }},
		{name: "Insurance ratio", mutate: func(p *types.EconomicPolicy) { p.InsuranceTargetRatio = math.LegacyMustNewDecFromStr("0.51") }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			policy := boundedCandidatePolicy()
			tc.mutate(&policy)
			require.Error(t, mandate.ValidatePolicy(policy))
		})
	}
}

func validEconomicMandate() types.EconomicMandate {
	minimum := types.DefaultEconomicPolicy()
	maximum := types.EconomicPolicy{
		TransferTaxRate:             math.LegacyMustNewDecFromStr("0.1"),
		ValidatorBlockRewardTarget:  math.NewInt(10),
		OracleBlockRewardTarget:     math.NewInt(10),
		RedemptionBufferTargetRatio: math.LegacyMustNewDecFromStr("0.5"),
		StrategicReserveTargetRatio: math.LegacyMustNewDecFromStr("0.5"),
		InsuranceTargetRatio:        math.LegacyMustNewDecFromStr("0.5"),
		LiabilityRatioWeight:        math.LegacyOneDec(),
		VolatilityWeight:            math.LegacyOneDec(),
		FlowWeight:                  math.LegacyOneDec(),
	}
	return types.EconomicMandate{
		Envelope: mandate.Envelope{
			Term:             1,
			Committee:        testAddress(9),
			ActivationHeight: 10,
			ExpiryHeight:     20,
		},
		MinimumPolicy: minimum,
		MaximumPolicy: maximum,
	}
}

func boundedCandidatePolicy() types.EconomicPolicy {
	return types.EconomicPolicy{
		TransferTaxRate:             math.LegacyMustNewDecFromStr("0.05"),
		ValidatorBlockRewardTarget:  math.NewInt(5),
		OracleBlockRewardTarget:     math.NewInt(5),
		RedemptionBufferTargetRatio: math.LegacyMustNewDecFromStr("0.25"),
		StrategicReserveTargetRatio: math.LegacyMustNewDecFromStr("0.25"),
		InsuranceTargetRatio:        math.LegacyMustNewDecFromStr("0.25"),
		LiabilityRatioWeight:        math.LegacyMustNewDecFromStr("0.5"),
		VolatilityWeight:            math.LegacyMustNewDecFromStr("0.5"),
		FlowWeight:                  math.LegacyMustNewDecFromStr("0.5"),
	}
}
