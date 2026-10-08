package types_test

import (
	stdmath "math"
	"math/big"
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
		{"more periods than a shape holds", types.MinCompensationPeriod, types.MaxSchedulePeriods + 2, false},
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
	n := math.NewInt
	bits128 := math.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 128))
	for _, tc := range []struct {
		name     string
		value    math.Int
		duration uint64
		want     math.Int
		bad      bool
	}{
		{"a bounty counts whole", n(200_000), 14 * 86_400, n(200_000), false},
		{"a year counts whole", n(131_000), year, n(131_000), false},
		{"two years count half a year each", n(262_000), 2 * year, n(131_000), false},
		{"ten years count a tenth", n(1_310_000), 10 * year, n(131_000), false},
		{"rounds up", n(10), 3 * year, n(4), false},
		{"a 128-bit value spreads", bits128.SubRaw(1), 2 * year, bits128.QuoRaw(2), false},
		{"a value past 128 bits", bits128, 2 * year, math.Int{}, true},
		{"a value past 128 bits within a window", bits128, year, math.Int{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			charge, err := types.CompensationCharge(tc.value, tc.duration, year)
			if tc.bad {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, charge)
		})
	}
}
