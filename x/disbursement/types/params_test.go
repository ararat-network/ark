package types_test

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/x/disbursement/types"
)

func TestParamsValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*types.Params)
		valid  bool
	}{
		{"defaults", func(*types.Params) {}, true},
		{"zero amount", func(p *types.Params) { p.MemberAmount = math.ZeroInt() }, false},
		{"nil amount", func(p *types.Params) { p.MemberAmount = math.Int{} }, false},
		{"excess amount", func(p *types.Params) { p.MemberAmount = math.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 128)) }, false},
		{"dust period", func(p *types.Params) { p.MemberAmount = math.OneInt() }, false},
		{"member shape past the period limit", func(p *types.Params) { p.MemberSchedule.Periods = types.MaxSchedulePeriods + 1 }, false},
		{"zero length", func(p *types.Params) { p.MemberSchedule.FirstLength = 0 }, false},
		{"zero weight", func(p *types.Params) { p.MemberSchedule.FirstWeight = 0 }, false},
		{"zero cap", func(p *types.Params) { p.CompensationCap = math.ZeroInt() }, false},
		{"excess cap", func(p *types.Params) { p.CompensationCap = math.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 128)) }, false},
		{"zero window", func(p *types.Params) { p.CompensationWindow = 0 }, false},
		{"century window", func(p *types.Params) { p.CompensationWindow = types.MaxPeriodLength }, true},
		{"window over a century", func(p *types.Params) { p.CompensationWindow = types.MaxPeriodLength + 1 }, false},
		{"period under two weeks", func(p *types.Params) { p.CompensationPeriod = types.MinFirstPeriod - 1 }, false},
		{"member hold under two weeks", func(p *types.Params) { p.MemberSchedule.FirstLength = types.MinFirstPeriod - 1 }, false},
		{"member hold past the floor", func(p *types.Params) { p.MemberSchedule.FirstLength = types.MinFirstPeriod + 1 }, true},
		{"ownership cliff under two weeks", func(p *types.Params) { p.OwnershipSchedule.FirstLength = types.MinFirstPeriod - 1 }, false},
		{"ownership cliff at the floor", func(p *types.Params) { p.OwnershipSchedule.FirstLength = types.MinFirstPeriod }, true},
		{"ten-year period", func(p *types.Params) { p.CompensationPeriod = types.MaxCompensationDuration }, true},
		{"period over ten years", func(p *types.Params) { p.CompensationPeriod = types.MaxCompensationDuration + 1 }, false},
		{"zero ownership cap", func(p *types.Params) { p.OwnershipCap = math.ZeroInt() }, false},
		{"ownership shape without a first length", func(p *types.Params) { p.OwnershipSchedule.FirstLength = 0 }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := types.DefaultParams()
			tc.mutate(&p)
			err := p.Validate()
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
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
