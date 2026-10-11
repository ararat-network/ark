package types_test

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/x/disbursement/types"
)

func TestParamsValidation(t *testing.T) {
	const (
		member       = "member schedule: "
		ownership    = "ownership schedule: "
		firstFloor   = "member and ownership first periods must be at least two weeks"
		window       = "compensation window must be positive and at most a century"
		period       = "compensation period must be at least two weeks and at most ten years"
		firstBound   = "first period requires a bounded positive length and weight"
		furtherBound = "further periods require a bounded positive length and weight"
		amountBits   = "amount must be nonnegative and at most 128 bits"
	)
	bits128 := math.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 128))
	for _, tc := range []struct {
		name   string
		mutate func(*types.Params)
		err    string
	}{
		{"defaults", func(*types.Params) {}, ""},
		{"zero amount", func(p *types.Params) { p.MemberAmount = math.ZeroInt() }, member + "amount must be positive"},
		{"nil amount", func(p *types.Params) { p.MemberAmount = math.Int{} }, member + amountBits},
		{"excess amount", func(p *types.Params) { p.MemberAmount = bits128 }, member + amountBits},
		{"dust period", func(p *types.Params) { p.MemberAmount = math.OneInt() }, member + "every period must receive a positive amount"},
		{"member shape past the period limit", func(p *types.Params) { p.MemberSchedule.Periods = types.MaxSchedulePeriods + 1 }, member + "schedule has at most 1200 further periods"},
		{"zero length", func(p *types.Params) { p.MemberSchedule.FirstLength = 0 }, member + firstBound},
		{"zero weight", func(p *types.Params) { p.MemberSchedule.FirstWeight = 0 }, member + firstBound},
		{"zero cap", func(p *types.Params) { p.CompensationCap = math.ZeroInt() }, "compensation cap: amount must be positive"},
		{"excess cap", func(p *types.Params) { p.CompensationCap = bits128 }, "compensation cap: " + amountBits},
		{"zero window", func(p *types.Params) { p.CompensationWindow = 0 }, window},
		{"century window", func(p *types.Params) { p.CompensationWindow = types.MaxPeriodLength }, ""},
		{"window over a century", func(p *types.Params) { p.CompensationWindow = types.MaxPeriodLength + 1 }, window},
		{"period under two weeks", func(p *types.Params) { p.CompensationPeriod = types.MinFirstPeriod - 1 }, period},
		{"member hold under two weeks", func(p *types.Params) { p.MemberSchedule.FirstLength = types.MinFirstPeriod - 1 }, firstFloor},
		{"member hold past the floor", func(p *types.Params) { p.MemberSchedule.FirstLength = types.MinFirstPeriod + 1 }, ""},
		{"ownership cliff under two weeks", func(p *types.Params) { p.OwnershipSchedule.FirstLength = types.MinFirstPeriod - 1 }, firstFloor},
		{"ownership cliff of zero", func(p *types.Params) { p.OwnershipSchedule.FirstLength = 0 }, firstFloor},
		{"ownership cliff at the floor", func(p *types.Params) { p.OwnershipSchedule.FirstLength = types.MinFirstPeriod }, ""},
		{"ten-year period", func(p *types.Params) { p.CompensationPeriod = types.MaxCompensationDuration }, ""},
		{"period over ten years", func(p *types.Params) { p.CompensationPeriod = types.MaxCompensationDuration + 1 }, period},
		{"zero ownership cap", func(p *types.Params) { p.OwnershipCap = math.ZeroInt() }, "ownership cap: amount must be positive"},
		// Past the two-week floor, the ownership shape is bounded by its own validation.
		{"ownership cliff over a century", func(p *types.Params) { p.OwnershipSchedule.FirstLength = types.MaxPeriodLength + 1 }, ownership + firstBound},
		{"ownership cliff of a century", func(p *types.Params) { p.OwnershipSchedule.FirstLength = types.MaxPeriodLength }, ""},
		{"ownership cliff without a weight", func(p *types.Params) { p.OwnershipSchedule.FirstWeight = 0 }, ownership + firstBound},
		{"ownership shape past the period limit", func(p *types.Params) { p.OwnershipSchedule.Periods = types.MaxSchedulePeriods + 1 }, ownership + "schedule has at most 1200 further periods"},
		{"ownership shape at the period limit", func(p *types.Params) { p.OwnershipSchedule.Periods = types.MaxSchedulePeriods }, ""},
		{"ownership periods without a weight", func(p *types.Params) { p.OwnershipSchedule.PeriodWeight = 0 }, ownership + furtherBound},
		{"ownership period weight past the bound", func(p *types.Params) { p.OwnershipSchedule.PeriodWeight = types.MaxWeight + 1 }, ownership + furtherBound},
		{"ownership period weight at the bound", func(p *types.Params) { p.OwnershipSchedule.PeriodWeight = types.MaxWeight }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := types.DefaultParams()
			tc.mutate(&p)
			err := p.Validate()
			if tc.err == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.err)
		})
	}
}

func TestDefaultSchedules(t *testing.T) {
	member := types.DefaultMemberSchedule()
	require.EqualValues(t, 13, member.Periods, "thirteen four-week periods after the hold")
	require.Equal(t, types.MinFirstPeriod, member.FirstLength, "the tenth is held two weeks")
	require.Equal(t, types.MinFirstPeriod+uint64(364*86_400), member.Duration(), "a 52-week year after the hold")
	first, each, _, err := member.Shares(math.NewInt(130))
	require.NoError(t, err)
	require.Equal(t, math.NewInt(13), first, "a tenth after the hold")
	require.Equal(t, math.NewInt(9), each)

	ownership := types.DefaultOwnershipSchedule()
	require.NoError(t, ownership.Validate())
	require.EqualValues(t, 78, ownership.Periods, "three years of fortnights after the first")
	require.Equal(t, uint64(26*14*86_400), ownership.FirstLength, "a cliff of 26 fortnights")
	require.Equal(t, uint64(104*14*86_400), ownership.Duration(), "104 fortnights at one weight each")
	first, each, _, err = ownership.Shares(math.NewInt(104))
	require.NoError(t, err)
	require.Equal(t, math.NewInt(26), first, "the first period pays a quarter")
	require.Equal(t, math.NewInt(1), each)
}
