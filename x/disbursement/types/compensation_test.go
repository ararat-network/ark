package types_test

import (
	stdmath "math"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/x/disbursement/types"
)

func TestCompensationSchedule(t *testing.T) {
	const fortnight = types.DefaultCompensationPeriod
	for _, tc := range []struct {
		name   string
		period uint64
		count  uint32
		valid  bool
	}{
		{"bounty", fortnight, 1, true},
		{"a year of fortnights", fortnight, 26, true},
		{"ten years of fortnights", fortnight, 260, true},
		{"past ten years", fortnight, 261, false},
		{"none", fortnight, 0, false},
		{"a week", types.MinCompensationPeriod, 1, true},
		{"under a week", types.MinCompensationPeriod - 1, 1, false},
		{"a ten-year period", types.MaxCompensationDuration, 1, true},
		{"a period past ten years", types.MaxCompensationDuration + 1, 1, false},
		{"a period that wraps the product", stdmath.MaxUint64, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shape, err := types.CompensationSchedule(tc.period, tc.count)
			if !tc.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, types.Schedule{FirstLength: tc.period, FirstWeight: 1, PeriodLength: tc.period, PeriodWeight: 1, Periods: tc.count - 1}, shape)
		})
	}
}

func TestCompensationCharge(t *testing.T) {
	year := uint64(365 * 86_400)
	for _, tc := range []struct {
		name     string
		value    int64
		duration uint64
		want     int64
	}{
		{"a bounty counts whole", 200_000, 14 * 86_400, 200_000},
		{"a year counts whole", 131_000, year, 131_000},
		{"two years count half a year each", 262_000, 2 * year, 131_000},
		{"ten years count a tenth", 1_310_000, 10 * year, 131_000},
		{"rounds up", 10, 3 * year, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			charge, err := types.CompensationCharge(math.NewInt(tc.value), tc.duration, year)
			require.NoError(t, err)
			require.Equal(t, math.NewInt(tc.want), charge)
		})
	}
}
