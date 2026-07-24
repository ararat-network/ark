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
				require.Equal(t, math.LegacyNewDecWithPrec(667, 3), p.VoteThreshold)
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
		// SlashFraction
		{
			name:      "slash fraction missing",
			mutate:    func(p *types.Params) { p.SlashFraction = math.LegacyDec{} },
			expectErr: "SlashFraction must be set",
		},
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
			name:      "min valid per window missing",
			mutate:    func(p *types.Params) { p.MinValidPerWindow = math.LegacyDec{} },
			expectErr: "MinValidPerWindow must be set",
		},
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
				p.TobinTaxes = []types.TobinTax{{Denom: "uusd", TobinTax: math.LegacyDec{}}}
			},
			expectErr: "TobinTaxes must have TobinTax set",
		},
		{
			name: "tobin tax empty denom",
			mutate: func(p *types.Params) {
				p.TobinTaxes = []types.TobinTax{{Denom: "", TobinTax: math.LegacyNewDecWithPrec(25, 4)}}
			},
			expectErr: "TobinTaxes denom must be a micro denom beginning with u",
		},
		{
			name: "tobin tax denom must be canonical lowercase",
			mutate: func(p *types.Params) {
				p.TobinTaxes = []types.TobinTax{{Denom: "uUSD", TobinTax: math.LegacyNewDecWithPrec(25, 4)}}
			},
			expectErr: "canonical lowercase micro denom",
		},
		{
			name: "tobin tax denom cannot contain path separators",
			mutate: func(p *types.Params) {
				p.TobinTaxes = []types.TobinTax{{Denom: "ufoo/bar", TobinTax: math.LegacyNewDecWithPrec(25, 4)}}
			},
			expectErr: "canonical lowercase micro denom",
		},
		{
			name: "tobin tax native denom",
			mutate: func(p *types.Params) {
				p.TobinTaxes = []types.TobinTax{{Denom: chain.MicroNoahDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)}}
			},
			expectErr: "TobinTaxes must not contain native denom unoah",
		},
		{
			name: "tobin tax negative",
			mutate: func(p *types.Params) {
				p.TobinTaxes = []types.TobinTax{{Denom: "uusd", TobinTax: math.LegacyNewDec(-1)}}
			},
			expectErr: "TobinTaxes must have TobinTax between [0, 1]",
		},
		{
			name: "tobin tax above one",
			mutate: func(p *types.Params) {
				p.TobinTaxes = []types.TobinTax{{Denom: "uusd", TobinTax: math.LegacyNewDecWithPrec(101, 2)}}
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
			name: "duplicate tobin tax denom",
			mutate: func(p *types.Params) {
				p.TobinTaxes = []types.TobinTax{
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
			if tc.check != nil {
				tc.check(t, p)
			}
		})
	}
}

func TestDefaultParamsClonesTobinTaxes(t *testing.T) {
	params := types.DefaultParams()
	params.TobinTaxes[0].Denom = "umutated"

	fresh := types.DefaultParams()
	require.Equal(t, types.DefaultTobinTaxes, fresh.TobinTaxes)
	require.NotEqual(t, params.TobinTaxes[0].Denom, fresh.TobinTaxes[0].Denom)
}

func makeTestTobinTaxes(count int) []types.TobinTax {
	tobinTaxes := make([]types.TobinTax, count)
	for i := range count {
		tobinTaxes[i] = types.TobinTax{
			Denom:    fmt.Sprintf("u%03d", i),
			TobinTax: math.LegacyNewDecWithPrec(25, 4),
		}
	}

	return tobinTaxes
}
