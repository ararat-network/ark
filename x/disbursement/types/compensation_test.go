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
		{"the floor", types.MinFirstPeriod, 1, true},
		{"under the floor", types.MinFirstPeriod - 1, 1, false},
		{"a ten-year period", types.MaxCompensationDuration, 1, true},
		{"a period past ten years", types.MaxCompensationDuration + 1, 1, false},
		{"a period that wraps the product", stdmath.MaxUint64, 2, false},
		{"more periods than a shape holds", types.MinFirstPeriod, types.MaxSchedulePeriods + 2, false},
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
	year, quarter := types.DefaultCompensationWindow, types.MaxCompensationBackdate
	n := math.NewInt
	bits128 := math.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 128))
	for _, tc := range []struct {
		name     string
		value    math.Int
		duration uint64
		span     uint64
		want     math.Int
		bad      bool
	}{
		{"a bounty counts whole", n(200_000), 14 * 86_400, year, n(200_000), false},
		{"a year counts whole", n(131_000), year, year, n(131_000), false},
		{"two years count half a year each", n(262_000), 2 * year, year, n(131_000), false},
		{"ten years count a tenth", n(1_310_000), 10 * year, year, n(131_000), false},
		{"rounds up", n(10), 3 * year, year, n(4), false},
		// In its first window an award counts what it pays there: the window plus its backdate (D122).
		{"a two-year stream a year back counts whole at first", n(2_000_000), 2 * year, 2 * year, n(2_000_000), false},
		{"ten years a quarter back count an eighth at first", n(1_310_000), 10 * year, year + quarter, n(163_750), false},
		{"a 128-bit value spreads", bits128.SubRaw(1), 2 * year, year, bits128.QuoRaw(2), false},
		{"a value past 128 bits", bits128, 2 * year, year, math.Int{}, true},
		{"a value past 128 bits within a window", bits128, year, year, math.Int{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			charge, err := types.CompensationCharge(tc.value, tc.duration, tc.span)
			if tc.bad {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, charge)
		})
	}
}

func TestCompensationBackdate(t *testing.T) {
	for _, tc := range []struct {
		name           string
		start, awarded uint64
		want           uint64
	}{
		{"before the award", 100, 160, 60},
		{"at the award", 160, 160, 0},
		{"after the award", 200, 160, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, types.CompensationBackdate(tc.start, tc.awarded))
		})
	}
}

// TestLongestStreamChargesATenthOfItsValue pins what the founder's stream relies on: with the window at
// 26 fortnights, the longest award, 260 fortnights, charges a tenth of its value a window, so ten times
// the cap fits exactly and a base unit more does not (D129, D130).
func TestLongestStreamChargesATenthOfItsValue(t *testing.T) {
	p := types.DefaultParams()
	shape, err := types.CompensationSchedule(p.CompensationPeriod, 260)
	require.NoError(t, err)
	require.Equal(t, types.MaxCompensationDuration, shape.Duration(), "the bound is 260 fortnights exactly")
	_, err = types.CompensationSchedule(p.CompensationPeriod, 261)
	require.Error(t, err, "260 fortnights is the longest award")
	value := p.CompensationCap.MulRaw(10)
	charge, err := types.CompensationCharge(value, shape.Duration(), p.CompensationWindow)
	require.NoError(t, err)
	require.Equal(t, p.CompensationCap, charge)
	charge, err = types.CompensationCharge(value.AddRaw(1), shape.Duration(), p.CompensationWindow)
	require.NoError(t, err)
	require.True(t, charge.GT(p.CompensationCap))
}
