package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"noah/x/treasury/types"
)

func TestClamp_NoChange(t *testing.T) {
	pc := types.PolicyConstraints{
		RateMin:       math.LegacyNewDecWithPrec(5, 4),  // 0.05%
		RateMax:       math.LegacyNewDecWithPrec(1, 2),  // 1%
		ChangeRateMax: math.LegacyNewDecWithPrec(25, 5), // 0.025%
		Cap:           sdk.NewCoin("usdr", math.ZeroInt()),
	}

	prevRate := math.LegacyNewDecWithPrec(5, 3) // 0.5%
	newRate := math.LegacyNewDecWithPrec(5, 3)  // 0.5%

	result := pc.Clamp(prevRate, newRate)
	require.True(t, result.Equal(prevRate))
}

func TestClamp_IncreaseWithinMax(t *testing.T) {
	pc := types.PolicyConstraints{
		RateMin:       math.LegacyNewDecWithPrec(5, 4),
		RateMax:       math.LegacyNewDecWithPrec(1, 2),
		ChangeRateMax: math.LegacyNewDecWithPrec(25, 5), // 0.025%
		Cap:           sdk.NewCoin("usdr", math.ZeroInt()),
	}

	prevRate := math.LegacyNewDecWithPrec(5, 3)   // 0.5%
	newRate := math.LegacyNewDecWithPrec(510, 5)   // 0.51% (delta = 0.01%, within 0.025%)

	result := pc.Clamp(prevRate, newRate)
	require.True(t, result.Equal(newRate))
}

func TestClamp_IncreaseCappedByChangeRateMax(t *testing.T) {
	pc := types.PolicyConstraints{
		RateMin:       math.LegacyNewDecWithPrec(5, 4),
		RateMax:       math.LegacyNewDecWithPrec(1, 2),
		ChangeRateMax: math.LegacyNewDecWithPrec(25, 5), // 0.025%
		Cap:           sdk.NewCoin("usdr", math.ZeroInt()),
	}

	prevRate := math.LegacyNewDecWithPrec(5, 3) // 0.5%
	newRate := math.LegacyNewDecWithPrec(1, 2)  // 1% (delta = 0.5%, exceeds 0.025%)

	result := pc.Clamp(prevRate, newRate)
	expected := prevRate.Add(pc.ChangeRateMax) // 0.5% + 0.025% = 0.525%
	require.True(t, result.Equal(expected))
}

func TestClamp_DecreaseCappedByChangeRateMax(t *testing.T) {
	pc := types.PolicyConstraints{
		RateMin:       math.LegacyNewDecWithPrec(5, 4),
		RateMax:       math.LegacyNewDecWithPrec(1, 2),
		ChangeRateMax: math.LegacyNewDecWithPrec(25, 5), // 0.025%
		Cap:           sdk.NewCoin("usdr", math.ZeroInt()),
	}

	prevRate := math.LegacyNewDecWithPrec(5, 3)   // 0.5%
	newRate := math.LegacyNewDecWithPrec(1, 3)     // 0.1% (delta = -0.4%, exceeds 0.025%)

	result := pc.Clamp(prevRate, newRate)
	expected := prevRate.Sub(pc.ChangeRateMax) // 0.5% - 0.025% = 0.475%
	require.True(t, result.Equal(expected))
}

func TestClamp_ClampedToRateMax(t *testing.T) {
	pc := types.PolicyConstraints{
		RateMin:       math.LegacyNewDecWithPrec(5, 4),
		RateMax:       math.LegacyNewDecWithPrec(1, 2),  // 1%
		ChangeRateMax: math.LegacyNewDecWithPrec(5, 1),  // 50% change allowed
		Cap:           sdk.NewCoin("usdr", math.ZeroInt()),
	}

	prevRate := math.LegacyNewDecWithPrec(9, 3) // 0.9%
	newRate := math.LegacyNewDecWithPrec(2, 2)  // 2% (exceeds RateMax 1%)

	result := pc.Clamp(prevRate, newRate)
	require.True(t, result.Equal(pc.RateMax))
}

func TestClamp_ClampedToRateMin(t *testing.T) {
	pc := types.PolicyConstraints{
		RateMin:       math.LegacyNewDecWithPrec(5, 4),  // 0.05%
		RateMax:       math.LegacyNewDecWithPrec(1, 2),
		ChangeRateMax: math.LegacyNewDecWithPrec(5, 1),  // 50% change allowed
		Cap:           sdk.NewCoin("usdr", math.ZeroInt()),
	}

	prevRate := math.LegacyNewDecWithPrec(1, 3) // 0.1%
	newRate := math.LegacyZeroDec()              // 0% (below RateMin 0.05%)

	result := pc.Clamp(prevRate, newRate)
	require.True(t, result.Equal(pc.RateMin))
}
