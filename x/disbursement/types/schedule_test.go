package types_test

import (
	"math/big"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/x/disbursement/types"
)

func TestScheduleShares(t *testing.T) {
	for _, tc := range []struct {
		name              string
		amount            int64
		shape             types.Schedule
		first, each, last int64
		err               string
	}{
		{"remainder", 23, types.Schedule{FirstLength: 10, FirstWeight: 1, PeriodLength: 10, PeriodWeight: 1, Periods: 2}, 7, 7, 9, ""},
		{"exact", 40, types.Schedule{FirstLength: 1, FirstWeight: 1, PeriodLength: 20, PeriodWeight: 3, Periods: 1}, 10, 30, 30, ""},
		{"one period", 23, types.Schedule{FirstLength: 10, FirstWeight: 1}, 23, 0, 23, ""},
		{"too small", 1, types.Schedule{FirstLength: 1, FirstWeight: 1, PeriodLength: 1, PeriodWeight: 1, Periods: 1}, 0, 0, 0, "every period must receive a positive amount"},
		{"weight over the bound", 100, types.Schedule{FirstLength: 1, FirstWeight: types.MaxWeight + 1, PeriodLength: 1, PeriodWeight: 1, Periods: 1}, 0, 0, 0, "first period requires a bounded positive length and weight"},
		{"periods over the bound", 100, types.Schedule{FirstLength: 1, FirstWeight: 1, PeriodLength: 1, PeriodWeight: 1, Periods: types.MaxSchedulePeriods + 1}, 0, 0, 0, "schedule has at most 1200 further periods"},
		{"further periods without a length", 100, types.Schedule{FirstLength: 1, FirstWeight: 1, PeriodWeight: 1, Periods: 1}, 0, 0, 0, "further periods require a bounded positive length and weight"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first, each, last, err := tc.shape.Shares(math.NewInt(tc.amount))
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, math.NewInt(tc.first), first)
			require.Equal(t, math.NewInt(tc.each), each)
			require.Equal(t, math.NewInt(tc.last), last)
		})
	}
}

func TestRandomisedShareConservation(t *testing.T) {
	for _, seed := range []int64{1, 17, 982} {
		t.Run(big.NewInt(seed).String(), func(t *testing.T) {
			r := rand.New(rand.NewSource(seed))
			for range 100 {
				shape := types.Schedule{
					FirstLength: uint64(1 + r.Intn(100)), FirstWeight: uint64(1 + r.Intn(100)),
					PeriodLength: uint64(1 + r.Intn(100)), PeriodWeight: uint64(1 + r.Intn(100)), Periods: uint32(r.Intn(31)),
				}
				amount := math.NewInt(1_000_000 + r.Int63n(1_000_000))
				first, each, last, err := shape.Shares(amount)
				require.NoError(t, err)
				require.True(t, first.IsPositive() && last.IsPositive())
				total := last
				if shape.Periods > 0 {
					require.True(t, each.IsPositive())
					total = first.Add(each.MulRaw(int64(shape.Periods - 1))).Add(last)
				}
				require.Equal(t, amount, total)
			}
		})
	}
}
