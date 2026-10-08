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
		{"period under a week", func(p *types.Params) { p.CompensationPeriod = types.MinCompensationPeriod - 1 }, false},
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
	require.EqualValues(t, 12, member.Periods, "twelve months after registration")
	first, each, _, err := member.Shares(math.NewInt(40))
	require.NoError(t, err)
	require.Equal(t, math.NewInt(4), first, "a tenth at registration")
	require.Equal(t, math.NewInt(3), each)

	ownership := types.DefaultOwnershipSchedule()
	require.NoError(t, ownership.Validate())
	require.EqualValues(t, 78, ownership.Periods, "three years of fortnights after the first")
	require.Equal(t, uint64(365*86_400), ownership.FirstLength)
	first, each, _, err = ownership.Shares(math.NewInt(104))
	require.NoError(t, err)
	require.Equal(t, math.NewInt(26), first, "the first period pays a quarter")
	require.Equal(t, math.NewInt(1), each)
}
