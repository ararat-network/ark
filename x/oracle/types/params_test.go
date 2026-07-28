package types_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"ark/pkg/chain"
	"ark/x/oracle/types"
)

func TestParamsValidate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*types.Params)
		expectErr string
		check     func(*testing.T, types.Params)
	}{
		{
			name:   "default is valid",
			mutate: func(p *types.Params) {},
			check: func(t *testing.T, p types.Params) {
				require.Equal(t, math.LegacyMustNewDecFromStr("0.666666666666666667"), p.VoteThreshold)
				require.Equal(t, time.Minute, p.MaxExchangeRateAge)
			},
		},
		// VoteThreshold
		{
			name:      "vote threshold missing",
			mutate:    func(p *types.Params) { p.VoteThreshold = math.LegacyDec{} },
			expectErr: "VoteThreshold must be set",
		},
		{
			name:      "vote threshold below 50%",
			mutate:    func(p *types.Params) { p.VoteThreshold = math.LegacyNewDecWithPrec(499, 3) },
			expectErr: "VoteThreshold must be at least 50 percent",
		},
		{
			name:      "vote threshold zero",
			mutate:    func(p *types.Params) { p.VoteThreshold = math.LegacyZeroDec() },
			expectErr: "VoteThreshold must be at least 50 percent",
		},
		{
			name:   "vote threshold at 50%",
			mutate: func(p *types.Params) { p.VoteThreshold = types.MinVoteThreshold },
		},
		{
			name:   "vote threshold at 100%",
			mutate: func(p *types.Params) { p.VoteThreshold = math.LegacyOneDec() },
		},
		{
			name:      "vote threshold above 100%",
			mutate:    func(p *types.Params) { p.VoteThreshold = math.LegacyNewDecWithPrec(1001, 3) },
			expectErr: "VoteThreshold must not exceed 100 percent",
		},
		// RewardBand
		{
			name:      "reward band missing",
			mutate:    func(p *types.Params) { p.RewardBand = math.LegacyDec{} },
			expectErr: "RewardBand must be set",
		},
		{
			name:      "reward band negative",
			mutate:    func(p *types.Params) { p.RewardBand = math.LegacyNewDecWithPrec(-1, 2) },
			expectErr: "RewardBand must be between [0, 1]",
		},
		{
			name:      "reward band above one",
			mutate:    func(p *types.Params) { p.RewardBand = math.LegacyNewDecWithPrec(101, 2) },
			expectErr: "RewardBand must be between [0, 1]",
		},
		{
			name:   "reward band at one",
			mutate: func(p *types.Params) { p.RewardBand = math.LegacyOneDec() },
		},
		{
			name:   "reward band at zero",
			mutate: func(p *types.Params) { p.RewardBand = math.LegacyZeroDec() },
		},
		// RewardWindow
		{
			name:      "reward window zero",
			mutate:    func(p *types.Params) { p.RewardWindow = 0 },
			expectErr: "RewardWindow must be > 0",
		},
		// RewardDistributionWindow
		{
			name:      "reward distribution window less than reward window",
			mutate:    func(p *types.Params) { p.RewardDistributionWindow = p.RewardWindow - 1 },
			expectErr: "RewardDistributionWindow must be greater than or equal with RewardWindow",
		},
		{
			name:   "reward distribution window equal to reward window",
			mutate: func(p *types.Params) { p.RewardDistributionWindow = p.RewardWindow },
		},
		// AttendanceWindow
		{
			name:      "attendance window zero",
			mutate:    func(p *types.Params) { p.AttendanceWindow = 0 },
			expectErr: "AttendanceWindow must be > 0",
		},
		{
			name:   "attendance window at one",
			mutate: func(p *types.Params) { p.AttendanceWindow = 1 },
		},
		// MinAttendancePerWindow
		{
			name:      "min attendance per window missing",
			mutate:    func(p *types.Params) { p.MinAttendancePerWindow = math.LegacyDec{} },
			expectErr: "MinAttendancePerWindow must be set",
		},
		{
			name:      "min attendance per window negative",
			mutate:    func(p *types.Params) { p.MinAttendancePerWindow = math.LegacyNewDec(-1) },
			expectErr: "MinAttendancePerWindow must be between [0, 1]",
		},
		{
			name:      "min attendance per window above one",
			mutate:    func(p *types.Params) { p.MinAttendancePerWindow = math.LegacyNewDecWithPrec(101, 2) },
			expectErr: "MinAttendancePerWindow must be between [0, 1]",
		},
		{
			name:   "min attendance per window at zero",
			mutate: func(p *types.Params) { p.MinAttendancePerWindow = math.LegacyZeroDec() },
		},
		{
			name:   "min attendance per window at one",
			mutate: func(p *types.Params) { p.MinAttendancePerWindow = math.LegacyOneDec() },
		},
		// FunctioningBlockThreshold
		{
			name:      "functioning block threshold missing",
			mutate:    func(p *types.Params) { p.FunctioningBlockThreshold = math.LegacyDec{} },
			expectErr: "FunctioningBlockThreshold must be set",
		},
		{
			name:      "functioning block threshold below 50%",
			mutate:    func(p *types.Params) { p.FunctioningBlockThreshold = math.LegacyNewDecWithPrec(49, 2) },
			expectErr: "FunctioningBlockThreshold must be at least 50 percent",
		},
		{
			name:      "functioning block threshold zero",
			mutate:    func(p *types.Params) { p.FunctioningBlockThreshold = math.LegacyZeroDec() },
			expectErr: "FunctioningBlockThreshold must be at least 50 percent",
		},
		{
			name:      "functioning block threshold negative",
			mutate:    func(p *types.Params) { p.FunctioningBlockThreshold = math.LegacyNewDec(-1) },
			expectErr: "FunctioningBlockThreshold must be at least 50 percent",
		},
		{
			name:   "functioning block threshold at 50%",
			mutate: func(p *types.Params) { p.FunctioningBlockThreshold = types.MinFunctioningBlockThreshold },
		},
		{
			name:   "functioning block threshold at 100%",
			mutate: func(p *types.Params) { p.FunctioningBlockThreshold = math.LegacyOneDec() },
		},
		{
			name:      "functioning block threshold above 100%",
			mutate:    func(p *types.Params) { p.FunctioningBlockThreshold = math.LegacyNewDecWithPrec(101, 2) },
			expectErr: "FunctioningBlockThreshold must not exceed 100 percent",
		},
		// MaxExchangeRateAge
		{
			name:      "max exchange rate age zero",
			mutate:    func(p *types.Params) { p.MaxExchangeRateAge = 0 },
			expectErr: "MaxExchangeRateAge must be greater than zero",
		},
		{
			name:      "max exchange rate age negative",
			mutate:    func(p *types.Params) { p.MaxExchangeRateAge = -time.Second },
			expectErr: "MaxExchangeRateAge must be greater than zero",
		},
		// TobinTaxes
		{
			name: "tobin tax missing",
			mutate: func(p *types.Params) {
				p.TobinTaxes = []types.TobinTax{{Denom: "ausd", TobinTax: math.LegacyDec{}}}
			},
			expectErr: "TobinTaxes must have TobinTax set",
		},
		{
			name: "tobin tax empty denom",
			mutate: func(p *types.Params) {
				p.TobinTaxes = []types.TobinTax{{Denom: "", TobinTax: math.LegacyNewDecWithPrec(25, 4)}}
			},
			expectErr: "TobinTaxes denom must be an Ark-native base denom beginning with a",
		},
		{
			name: "tobin tax denom must be canonical lowercase",
			mutate: func(p *types.Params) {
				p.TobinTaxes = []types.TobinTax{{Denom: "aUSD", TobinTax: math.LegacyNewDecWithPrec(25, 4)}}
			},
			expectErr: "canonical lowercase Ark-native base denom",
		},
		{
			name: "tobin tax denom cannot contain path separators",
			mutate: func(p *types.Params) {
				p.TobinTaxes = []types.TobinTax{{Denom: "afoo/bar", TobinTax: math.LegacyNewDecWithPrec(25, 4)}}
			},
			expectErr: "canonical lowercase Ark-native base denom",
		},
		{
			name: "tobin tax native denom",
			mutate: func(p *types.Params) {
				p.TobinTaxes = []types.TobinTax{{Denom: chain.NoahBaseDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)}}
			},
			expectErr: "TobinTaxes must not contain native denom anoah",
		},
		{
			name: "tobin tax negative",
			mutate: func(p *types.Params) {
				p.TobinTaxes = []types.TobinTax{{Denom: "ausd", TobinTax: math.LegacyNewDec(-1)}}
			},
			expectErr: "TobinTaxes must have TobinTax between [0, 1]",
		},
		{
			name: "tobin tax above one",
			mutate: func(p *types.Params) {
				p.TobinTaxes = []types.TobinTax{{Denom: "ausd", TobinTax: math.LegacyNewDecWithPrec(101, 2)}}
			},
			expectErr: "TobinTaxes must have TobinTax between [0, 1]",
		},
		{
			name: "empty tobin taxes is valid",
			mutate: func(p *types.Params) {
				p.TobinTaxes = []types.TobinTax{}
			},
		},
		{
			name: "maximum vote targets is valid",
			mutate: func(p *types.Params) {
				p.TobinTaxes = makeTestTobinTaxes(types.MaxVoteTargets)
			},
		},
		{
			name: "too many vote targets",
			mutate: func(p *types.Params) {
				p.TobinTaxes = makeTestTobinTaxes(types.MaxVoteTargets + 1)
			},
			expectErr: "exceeds maximum vote targets",
		},
		{
			name: "sorted tobin taxes are valid",
			mutate: func(p *types.Params) {
				p.TobinTaxes = []types.TobinTax{
					{Denom: "akrw", TobinTax: math.LegacyNewDecWithPrec(25, 4)},
					{Denom: "ausd", TobinTax: math.LegacyNewDecWithPrec(50, 4)},
				}
			},
		},
		{
			name: "unsorted tobin taxes",
			mutate: func(p *types.Params) {
				p.TobinTaxes = []types.TobinTax{
					{Denom: "ausd", TobinTax: math.LegacyNewDecWithPrec(25, 4)},
					{Denom: "akrw", TobinTax: math.LegacyNewDecWithPrec(50, 4)},
				}
			},
			expectErr: "TobinTaxes must be sorted by unique denom",
		},
		{
			name: "duplicate tobin tax denom",
			mutate: func(p *types.Params) {
				p.TobinTaxes = []types.TobinTax{
					{Denom: "ausd", TobinTax: math.LegacyNewDecWithPrec(25, 4)},
					{Denom: "ausd", TobinTax: math.LegacyNewDecWithPrec(50, 4)},
				}
			},
			expectErr: "TobinTaxes must be sorted by unique denom",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := types.DefaultParams()
			tc.mutate(&p)
			err := p.Validate()
			if tc.expectErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.ErrorContains(t, err, tc.expectErr)
			}
			if tc.check != nil {
				tc.check(t, p)
			}
		})
	}
}

func TestDefaultParamsClonesTobinTaxes(t *testing.T) {
	params := types.DefaultParams()
	params.TobinTaxes[0].Denom = "amutated"

	fresh := types.DefaultParams()
	require.Equal(t, types.DefaultTobinTaxes, fresh.TobinTaxes)
	require.NotEqual(t, params.TobinTaxes[0].Denom, fresh.TobinTaxes[0].Denom)
}

func makeTestTobinTaxes(count int) []types.TobinTax {
	tobinTaxes := make([]types.TobinTax, count)
	for i := range count {
		tobinTaxes[i] = types.TobinTax{
			Denom:    fmt.Sprintf("a%03d", i),
			TobinTax: math.LegacyNewDecWithPrec(25, 4),
		}
	}

	return tobinTaxes
}
