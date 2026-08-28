package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/treasury/types"
)

const (
	// defaultValidCase names the unmutated fixture every table in the package
	// opens with.
	defaultValidCase = "default is valid"
	// The step's lower and upper bounds are one condition, so both cases assert
	// the same message and the string lives once.
	stepOutOfRange = "ExposureMultiplierMaxStep must be greater than zero and at most"
)

func TestParamsValidate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*types.Params)
		expectErr string
	}{
		{name: defaultValidCase, mutate: func(*types.Params) {}},
		{
			name:      "zero reward funding window",
			mutate:    func(p *types.Params) { p.RewardFundingWindow = 0 },
			expectErr: "RewardFundingWindow must be between one and",
		},
		{
			name:   "reward funding window at the domain cap",
			mutate: func(p *types.Params) { p.RewardFundingWindow = types.MaxRewardFundingWindow },
		},
		{
			name:      "reward funding window above the domain cap",
			mutate:    func(p *types.Params) { p.RewardFundingWindow = types.MaxRewardFundingWindow + 1 },
			expectErr: "RewardFundingWindow must be between one and",
		},
		{
			// Zero would divide by zero in the modular cadence check.
			name:      "zero tax cap refresh period",
			mutate:    func(p *types.Params) { p.TaxCapRefreshPeriodBlocks = 0 },
			expectErr: "TaxCapRefreshPeriodBlocks must be between one and",
		},
		{
			name:   "single-block tax cap refresh period is valid",
			mutate: func(p *types.Params) { p.TaxCapRefreshPeriodBlocks = 1 },
		},
		{
			name:   "tax cap refresh period at the domain cap",
			mutate: func(p *types.Params) { p.TaxCapRefreshPeriodBlocks = types.MaxTaxCapRefreshPeriodBlocks },
		},
		{
			// A cadence no chain reaches retires the drift true-up silently.
			// Disabling it is a zero reference cap, not an unreachable period.
			name:      "tax cap refresh period above the domain cap",
			mutate:    func(p *types.Params) { p.TaxCapRefreshPeriodBlocks = types.MaxTaxCapRefreshPeriodBlocks + 1 },
			expectErr: "TaxCapRefreshPeriodBlocks must be between one and",
		},
		{
			name:      "reference cap denom must be canonical micro denom",
			mutate:    func(p *types.Params) { p.ReferenceTaxCap.Denom = "USDR" },
			expectErr: "ReferenceTaxCap denom is invalid",
		},
		{
			name:      "reference cap denom cannot be ibc path",
			mutate:    func(p *types.Params) { p.ReferenceTaxCap.Denom = "afoo/bar" },
			expectErr: "ReferenceTaxCap denom is invalid",
		},
		{
			name:      "reference cap must be set",
			mutate:    func(p *types.Params) { p.ReferenceTaxCap = sdk.Coin{} },
			expectErr: "ReferenceTaxCap is invalid",
		},
		{
			name:      "reference cap cannot be negative",
			mutate:    func(p *types.Params) { p.ReferenceTaxCap.Amount = math.NewInt(-1) },
			expectErr: "ReferenceTaxCap is invalid",
		},
		{
			name:   "zero reference cap is uncapped",
			mutate: func(p *types.Params) { p.ReferenceTaxCap.Amount = math.ZeroInt() },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			params := types.DefaultParams()
			tc.mutate(&params)

			err := params.Validate()
			if tc.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.expectErr)
		})
	}
}

// TestExposureMachineryValidate covers the exposure half of Params: the decays,
// the multiplier cap and step, and the refresh cadence. The indicator weights
// these size live on MonetaryPolicy and are covered beside it.
func TestExposureMachineryValidate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*types.Params)
		expectErr string
	}{
		{name: defaultValidCase, mutate: func(*types.Params) {}},
		{
			// Legitimate: the series becomes the latest sample alone.
			name:   "zero decay",
			mutate: func(p *types.Params) { p.VolatilityDecay = math.LegacyZeroDec() },
		},
		{
			// A retention of one never forgets, so the series would carry its
			// first observation forever.
			name:      "decay of exactly one",
			mutate:    func(p *types.Params) { p.VolatilityDecay = math.LegacyOneDec() },
			expectErr: "ExposureVolatilityDecay must be at least zero and below one",
		},
		{
			name:      "flow decay above one",
			mutate:    func(p *types.Params) { p.FlowDecay = math.LegacyNewDec(2) },
			expectErr: "ExposureFlowDecay must be at least zero and below one",
		},
		{
			name:      "negative decay",
			mutate:    func(p *types.Params) { p.FlowDecay = math.LegacyMustNewDecFromStr("-0.5") },
			expectErr: "ExposureFlowDecay must be at least zero and below one",
		},
		{
			// The boundary-valid cap: scaling by exactly one is the unscaled
			// rule, which every consumer must be able to express.
			name:   "cap of exactly one",
			mutate: func(p *types.Params) { p.MultiplierCap = math.LegacyOneDec() },
		},
		{
			// Below one the model would demand less capital than the unscaled
			// rule, turning a risk model into a discount.
			name: "cap below one",
			mutate: func(p *types.Params) {
				p.MultiplierCap = math.LegacyMustNewDecFromStr("0.5")
			},
			expectErr: "ExposureMultiplierCap must be between one and",
		},
		{
			name:   "cap at the domain cap",
			mutate: func(p *types.Params) { p.MultiplierCap = types.MaxExposureMultiplierCap },
		},
		{
			name: "cap above the domain cap",
			mutate: func(p *types.Params) {
				p.MultiplierCap = types.MaxExposureMultiplierCap.Add(math.LegacyOneDec())
			},
			expectErr: "ExposureMultiplierCap must be between one and",
		},
		{
			// A zero step admits no movement, which freezes the multiplier and
			// makes every weight dead configuration.
			name:      "zero step",
			mutate:    func(p *types.Params) { p.MultiplierMaxStep = math.LegacyZeroDec() },
			expectErr: stepOutOfRange,
		},
		{
			name:      "negative step",
			mutate:    func(p *types.Params) { p.MultiplierMaxStep = math.LegacyNewDec(-1) },
			expectErr: stepOutOfRange,
		},
		{
			name: "step above the domain cap",
			mutate: func(p *types.Params) {
				p.MultiplierMaxStep = types.MaxExposureMultiplierCap.Add(math.LegacyOneDec())
			},
			expectErr: stepOutOfRange,
		},
		{
			name:      "zero refresh period",
			mutate:    func(p *types.Params) { p.ExposureRefreshPeriodBlocks = 0 },
			expectErr: "ExposureRefreshPeriodBlocks must be between one and",
		},
		{
			name:   "refresh period at the domain cap",
			mutate: func(p *types.Params) { p.ExposureRefreshPeriodBlocks = chain.BlocksPerYear },
		},
		{
			name:      "refresh period above the domain cap",
			mutate:    func(p *types.Params) { p.ExposureRefreshPeriodBlocks = chain.BlocksPerYear + 1 },
			expectErr: "ExposureRefreshPeriodBlocks must be between one and",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			params := types.DefaultParams()
			test.mutate(&params)

			err := params.Validate()
			if test.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.expectErr)
		})
	}
}
