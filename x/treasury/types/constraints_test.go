package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/x/treasury/types"
)

func TestClamp(t *testing.T) {
	pc := types.PolicyConstraints{
		RateMin:       math.LegacyNewDecWithPrec(5, 4),  // 0.05%
		RateMax:       math.LegacyNewDecWithPrec(1, 2),  // 1%
		ChangeRateMax: math.LegacyNewDecWithPrec(25, 5), // 0.025%
		Cap:           sdk.NewCoin("usdr", math.ZeroInt()),
	}

	tests := []struct {
		name     string
		pc       types.PolicyConstraints
		prevRate math.LegacyDec
		newRate  math.LegacyDec
		expected math.LegacyDec
	}{
		{
			name:     "no change",
			pc:       pc,
			prevRate: math.LegacyNewDecWithPrec(5, 3),
			newRate:  math.LegacyNewDecWithPrec(5, 3),
			expected: math.LegacyNewDecWithPrec(5, 3),
		},
		{
			name:     "increase within change rate max",
			pc:       pc,
			prevRate: math.LegacyNewDecWithPrec(5, 3),   // 0.5%
			newRate:  math.LegacyNewDecWithPrec(510, 5), // 0.51%
			expected: math.LegacyNewDecWithPrec(510, 5), // 0.51%
		},
		{
			name:     "increase capped by change rate max",
			pc:       pc,
			prevRate: math.LegacyNewDecWithPrec(5, 3),   // 0.5%
			newRate:  math.LegacyNewDecWithPrec(1, 2),   // 1%
			expected: math.LegacyNewDecWithPrec(525, 5), // 0.5% + 0.025% = 0.525%
		},
		{
			name:     "decrease within change rate max",
			pc:       pc,
			prevRate: math.LegacyNewDecWithPrec(5, 3),   // 0.5%
			newRate:  math.LegacyNewDecWithPrec(490, 5), // 0.49%
			expected: math.LegacyNewDecWithPrec(490, 5), // 0.49%
		},
		{
			name:     "decrease capped by change rate max",
			pc:       pc,
			prevRate: math.LegacyNewDecWithPrec(5, 3),   // 0.5%
			newRate:  math.LegacyNewDecWithPrec(1, 3),   // 0.1%
			expected: math.LegacyNewDecWithPrec(475, 5), // 0.5% - 0.025% = 0.475%
		},
		{
			name: "clamped to rate max",
			pc: types.PolicyConstraints{
				RateMin:       math.LegacyNewDecWithPrec(5, 4),
				RateMax:       math.LegacyNewDecWithPrec(1, 2), // 1%
				ChangeRateMax: math.LegacyNewDecWithPrec(5, 1), // 50%
				Cap:           sdk.NewCoin("usdr", math.ZeroInt()),
			},
			prevRate: math.LegacyNewDecWithPrec(9, 3), // 0.9%
			newRate:  math.LegacyNewDecWithPrec(2, 2), // 2%
			expected: math.LegacyNewDecWithPrec(1, 2), // 1%
		},
		{
			name: "clamped to rate min",
			pc: types.PolicyConstraints{
				RateMin:       math.LegacyNewDecWithPrec(5, 4), // 0.05%
				RateMax:       math.LegacyNewDecWithPrec(1, 2),
				ChangeRateMax: math.LegacyNewDecWithPrec(5, 1), // 50%
				Cap:           sdk.NewCoin("usdr", math.ZeroInt()),
			},
			prevRate: math.LegacyNewDecWithPrec(1, 3), // 0.1%
			newRate:  math.LegacyZeroDec(),            // 0%
			expected: math.LegacyNewDecWithPrec(5, 4), // 0.05%
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.pc.Clamp(tc.prevRate, tc.newRate)
			require.True(t, result.Equal(tc.expected), "expected %s, got %s", tc.expected, result)
		})
	}
}
