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
			name:      "slash window zero",
			mutate:    func(p *types.Params) { p.SlashWindow = 0 },
			expectErr: "SlashWindow must be > 0",
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
		// TobinTaxes
		{
			name: "tobin tax empty denom",
			mutate: func(p *types.Params) {
				p.TobinTaxes = types.TobinTaxes{{Denom: "", TobinTax: math.LegacyNewDecWithPrec(25, 4)}}
			},
			expectErr: "TobinTaxes denom must be a micro denom beginning with u",
		},
		{
			name: "tobin tax negative",
			mutate: func(p *types.Params) {
				p.TobinTaxes = types.TobinTaxes{{Denom: "uusd", TobinTax: math.LegacyNewDec(-1)}}
			},
			expectErr: "TobinTaxes must have TobinTax between [0, 1]",
		},
		{
			name: "tobin tax above one",
			mutate: func(p *types.Params) {
				p.TobinTaxes = types.TobinTaxes{{Denom: "uusd", TobinTax: math.LegacyNewDecWithPrec(101, 2)}}
			},
			expectErr: "TobinTaxes must have TobinTax between [0, 1]",
		},
		{
			name: "empty tobin taxes is valid",
			mutate: func(p *types.Params) {
				p.TobinTaxes = types.TobinTaxes{}
			},
		},
		{
			name: "duplicate tobin tax denom",
			mutate: func(p *types.Params) {
				p.TobinTaxes = types.TobinTaxes{
					{Denom: "uusd", TobinTax: math.LegacyNewDecWithPrec(25, 4)},
					{Denom: "uusd", TobinTax: math.LegacyNewDecWithPrec(50, 4)},
				}
			},
			expectErr: "TobinTaxes contains duplicate denom: uusd",
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
