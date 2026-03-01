package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"noah/x/treasury/types"
)

func TestParams_Validate_Default(t *testing.T) {
	params := types.DefaultParams()
	require.NoError(t, params.Validate())
}

func TestParams_Validate_TaxRateMaxLTMin(t *testing.T) {
	params := types.DefaultParams()
	params.TaxPolicy.RateMax = math.LegacyNewDecWithPrec(1, 4)  // 0.01%
	params.TaxPolicy.RateMin = math.LegacyNewDecWithPrec(1, 2)  // 1%
	err := params.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "TaxPolicy.RateMax")
	require.ErrorContains(t, err, "must be greater than")
}

func TestParams_Validate_NegativeTaxRateMin(t *testing.T) {
	params := types.DefaultParams()
	params.TaxPolicy.RateMin = math.LegacyNewDec(-1)
	err := params.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "TaxPolicy.RateMin must be zero or positive")
}

func TestParams_Validate_InvalidTaxCap(t *testing.T) {
	params := types.DefaultParams()
	params.TaxPolicy.Cap = sdk.Coin{Denom: "", Amount: math.NewInt(-1)}
	err := params.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "TaxPolicy.Cap is invalid")
}

func TestParams_Validate_NegativeTaxChangeRateMax(t *testing.T) {
	params := types.DefaultParams()
	params.TaxPolicy.ChangeRateMax = math.LegacyNewDec(-1)
	err := params.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "TaxPolicy.ChangeRateMax must be positive")
}

func TestParams_Validate_RewardRateMaxLTMin(t *testing.T) {
	params := types.DefaultParams()
	params.RewardPolicy.RateMax = math.LegacyNewDecWithPrec(1, 2)  // 1%
	params.RewardPolicy.RateMin = math.LegacyNewDecWithPrec(10, 2) // 10%
	err := params.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "RewardPolicy.RateMax")
	require.ErrorContains(t, err, "must be greater than")
}

func TestParams_Validate_NegativeRewardRateMin(t *testing.T) {
	params := types.DefaultParams()
	params.RewardPolicy.RateMin = math.LegacyNewDec(-1)
	err := params.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "RewardPolicy.RateMin")
}

func TestParams_Validate_NegativeRewardChangeRateMax(t *testing.T) {
	params := types.DefaultParams()
	params.RewardPolicy.ChangeRateMax = math.LegacyNewDec(-1)
	err := params.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "RewardPolicy.ChangeRateMax must be positive")
}

func TestParams_Validate_NegativeSeigniorageBurdenTarget(t *testing.T) {
	params := types.DefaultParams()
	params.SeigniorageBurdenTarget = math.LegacyNewDec(-1)
	err := params.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "SeigniorageBurdenTarget must be positive")
}

func TestParams_Validate_NegativeMiningIncrement(t *testing.T) {
	params := types.DefaultParams()
	params.MiningIncrement = math.LegacyNewDec(-1)
	err := params.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "MiningIncrement must be positive")
}

func TestParams_Validate_WindowLongNotBiggerThanShort(t *testing.T) {
	params := types.DefaultParams()
	params.WindowLong = 4
	params.WindowShort = 4
	err := params.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "WindowLong must be bigger than WindowShort")
}

func TestParams_Validate_WindowLongSmallerThanShort(t *testing.T) {
	params := types.DefaultParams()
	params.WindowLong = 2
	params.WindowShort = 4
	err := params.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "WindowLong must be bigger than WindowShort")
}
