package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"noah/x/oracle/types"
)

func TestParamsValidate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*types.Params)
		expectErr string
	}{
		{
			name:   "default is valid",
			mutate: func(p *types.Params) {},
		},
		// VotePeriod
		{
			name:      "vote period zero",
			mutate:    func(p *types.Params) { p.VotePeriod = 0 },
			expectErr: "VotePeriod must be > 0",
		},
		// VoteThreshold
		{
			name:      "vote threshold at 33%",
			mutate:    func(p *types.Params) { p.VoteThreshold = math.LegacyNewDecWithPrec(33, 2) },
			expectErr: "VoteThreshold must be greater than 33 percent",
		},
		{
			name:      "vote threshold zero",
			mutate:    func(p *types.Params) { p.VoteThreshold = math.LegacyZeroDec() },
			expectErr: "VoteThreshold must be greater than 33 percent",
		},
		{
			name:   "vote threshold above 33%",
			mutate: func(p *types.Params) { p.VoteThreshold = math.LegacyNewDecWithPrec(34, 2) },
		},
		// RewardBand
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
		// RewardDistributionWindow
		{
			name:      "reward distribution window less than vote period",
			mutate:    func(p *types.Params) { p.RewardDistributionWindow = p.VotePeriod - 1 },
			expectErr: "RewardDistributionWindow must be greater than or equal with VotePeriod",
		},
		{
			name:   "reward distribution window equal to vote period",
			mutate: func(p *types.Params) { p.RewardDistributionWindow = p.VotePeriod },
		},
		// SlashFraction
		{
			name:      "slash fraction negative",
			mutate:    func(p *types.Params) { p.SlashFraction = math.LegacyNewDec(-1) },
			expectErr: "SlashFraction must be between [0, 1]",
		},
		{
			name:      "slash fraction above one",
			mutate:    func(p *types.Params) { p.SlashFraction = math.LegacyNewDecWithPrec(101, 2) },
			expectErr: "SlashFraction must be between [0, 1]",
		},
		{
			name:   "slash fraction at zero",
			mutate: func(p *types.Params) { p.SlashFraction = math.LegacyZeroDec() },
		},
		// SlashWindow
		{
			name:      "slash window less than vote period",
			mutate:    func(p *types.Params) { p.SlashWindow = p.VotePeriod - 1 },
			expectErr: "SlashWindow must be greater than or equal with VotePeriod",
		},
		{
			name:   "slash window equal to vote period",
			mutate: func(p *types.Params) { p.SlashWindow = p.VotePeriod },
		},
		// MinValidPerWindow
		{
			name:      "min valid per window negative",
			mutate:    func(p *types.Params) { p.MinValidPerWindow = math.LegacyNewDec(-1) },
			expectErr: "MinValidPerWindow must be between [0, 1]",
		},
		{
			name:      "min valid per window above one",
			mutate:    func(p *types.Params) { p.MinValidPerWindow = math.LegacyNewDecWithPrec(101, 2) },
			expectErr: "MinValidPerWindow must be between [0, 1]",
		},
		{
			name:   "min valid per window at zero",
			mutate: func(p *types.Params) { p.MinValidPerWindow = math.LegacyZeroDec() },
		},
		// Whitelist
		{
			name: "whitelist empty denom name",
			mutate: func(p *types.Params) {
				p.Whitelist = types.DenomList{{Name: "", TobinTax: math.LegacyNewDecWithPrec(25, 4)}}
			},
			expectErr: "Whitelist Denom must have name",
		},
		{
			name: "whitelist tobin tax negative",
			mutate: func(p *types.Params) {
				p.Whitelist = types.DenomList{{Name: "uusd", TobinTax: math.LegacyNewDec(-1)}}
			},
			expectErr: "Whitelist Denom must have TobinTax between [0, 1]",
		},
		{
			name: "whitelist tobin tax above one",
			mutate: func(p *types.Params) {
				p.Whitelist = types.DenomList{{Name: "uusd", TobinTax: math.LegacyNewDecWithPrec(101, 2)}}
			},
			expectErr: "Whitelist Denom must have TobinTax between [0, 1]",
		},
		{
			name: "empty whitelist is valid",
			mutate: func(p *types.Params) {
				p.Whitelist = types.DenomList{}
			},
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
		})
	}
}
