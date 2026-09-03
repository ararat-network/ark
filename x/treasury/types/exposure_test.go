package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/x/treasury/types"
)

// TestExposureModelLaunchesInert pins the launch guarantee the whole design
// rests on, across both owners: the committee's weights are zero and the
// governance machinery is populated, so the multiplier is one and every target
// is unscaled.
func TestExposureModelLaunchesInert(t *testing.T) {
	policy := types.DefaultEconomicPolicy()
	require.True(t, policy.LiabilityRatioWeight.IsZero())
	require.True(t, policy.VolatilityWeight.IsZero())
	require.True(t, policy.FlowWeight.IsZero())
	require.NoError(t, policy.Validate())

	// The machinery is not zero: it shapes the model the moment a weight turns
	// positive, so a weight vote must not have to carry it.
	params := types.DefaultParams()
	require.True(t, params.MultiplierCap.GTE(math.LegacyOneDec()))
	require.True(t, params.MultiplierMaxStep.IsPositive())
	require.NotZero(t, params.ExposureRefreshPeriodBlocks)
	require.NoError(t, params.Validate())

	require.Equal(t, math.LegacyOneDec(), types.DefaultExposureState().Multiplier)
}

func TestExposureStateValidate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*types.ExposureState)
		expectErr string
	}{
		{name: defaultValidCase, mutate: func(*types.ExposureState) {}},
		{
			// A nil Dec panics on comparison, so the unset guard ahead of every
			// range check is the gate. Each field is asserted separately because
			// four of them are guarded through a list: one left out of it is the
			// live risk, and a shared branch would hide that.
			name:      "unset reference price",
			mutate:    func(s *types.ExposureState) { s.LastReferencePrice = math.LegacyDec{} },
			expectErr: "LastReferencePrice must be set",
		},
		{
			name:      "unset flow pressure",
			mutate:    func(s *types.ExposureState) { s.FlowPressure = math.LegacyDec{} },
			expectErr: "FlowPressure must be set",
		},
		{
			name:      "unset liability ratio",
			mutate:    func(s *types.ExposureState) { s.LiabilityRatio = math.LegacyDec{} },
			expectErr: "LiabilityRatio must be set",
		},
		{
			name:      "unset flow ratio",
			mutate:    func(s *types.ExposureState) { s.FlowRatio = math.LegacyDec{} },
			expectErr: "FlowRatio must be set",
		},
		{
			name:      "unset variance",
			mutate:    func(s *types.ExposureState) { s.VolatilityVariance = math.LegacyDec{} },
			expectErr: "VolatilityVariance must be set",
		},
		{
			name:      "unset multiplier",
			mutate:    func(s *types.ExposureState) { s.Multiplier = math.LegacyDec{} },
			expectErr: "Multiplier must be set",
		},
		{
			name:      "negative variance",
			mutate:    func(s *types.ExposureState) { s.VolatilityVariance = math.LegacyNewDec(-1) },
			expectErr: "VolatilityVariance must be between zero and one",
		},
		{
			// The sample clamp bounds the series here by induction, so exactly
			// one is reachable and anything above it is a corrupted import.
			name:   "variance of exactly one",
			mutate: func(s *types.ExposureState) { s.VolatilityVariance = math.LegacyOneDec() },
		},
		{
			name: "variance above one",
			mutate: func(s *types.ExposureState) {
				s.VolatilityVariance = math.LegacyMustNewDecFromStr("1.5")
			},
			expectErr: "VolatilityVariance must be between zero and one",
		},
		{
			name:      "negative flow pressure",
			mutate:    func(s *types.ExposureState) { s.FlowPressure = math.LegacyNewDec(-1) },
			expectErr: "FlowPressure must be zero or positive",
		},
		{
			name:      "negative liability ratio",
			mutate:    func(s *types.ExposureState) { s.LiabilityRatio = math.LegacyNewDec(-1) },
			expectErr: "LiabilityRatio must be zero or positive",
		},
		{
			name:      "negative flow ratio",
			mutate:    func(s *types.ExposureState) { s.FlowRatio = math.LegacyNewDec(-1) },
			expectErr: "FlowRatio must be zero or positive",
		},
		{
			name:      "negative reference price",
			mutate:    func(s *types.ExposureState) { s.LastReferencePrice = math.LegacyNewDec(-1) },
			expectErr: "LastReferencePrice must be zero or positive",
		},
		{
			name:      "multiplier below one",
			mutate:    func(s *types.ExposureState) { s.Multiplier = math.LegacyMustNewDecFromStr("0.5") },
			expectErr: "Multiplier must be at least one",
		},
		{
			name:   "multiplier above one",
			mutate: func(s *types.ExposureState) { s.Multiplier = math.LegacyNewDec(3) },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := types.DefaultExposureState()
			test.mutate(&state)

			err := state.Validate()
			if test.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.expectErr)
		})
	}
}
