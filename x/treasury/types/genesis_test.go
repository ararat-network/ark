package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"noah/x/treasury/types"
)

func TestValidateGenesis_Default(t *testing.T) {
	genesis := types.DefaultGenesisState()
	require.NoError(t, genesis.Validate())
}

func TestValidateGenesis_TaxRateBelowMin(t *testing.T) {
	genesis := types.DefaultGenesisState()
	genesis.TaxRate = math.LegacyNewDecWithPrec(1, 5) // 0.001%, below RateMin 0.05%
	err := genesis.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "tax_rate must less than RateMax")
}

func TestValidateGenesis_TaxRateAboveMax(t *testing.T) {
	genesis := types.DefaultGenesisState()
	genesis.TaxRate = math.LegacyNewDecWithPrec(5, 1) // 50%, above RateMax 1%
	err := genesis.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "tax_rate must less than RateMax")
}

func TestValidateGenesis_RewardWeightBelowMin(t *testing.T) {
	genesis := types.DefaultGenesisState()
	genesis.RewardWeight = math.LegacyNewDecWithPrec(1, 2) // 1%, below RewardPolicy.RateMin 5%
	err := genesis.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "reward_weight must less than WeightMax")
}

func TestValidateGenesis_RewardWeightAboveMax(t *testing.T) {
	genesis := types.DefaultGenesisState()
	genesis.RewardWeight = math.LegacyNewDecWithPrec(60, 2) // 60%, above RewardPolicy.RateMax 50%
	err := genesis.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "reward_weight must less than WeightMax")
}

func TestValidateGenesis_InvalidParams(t *testing.T) {
	genesis := types.DefaultGenesisState()
	genesis.Params.TaxPolicy.RateMin = math.LegacyNewDec(-1)
	err := genesis.Validate()
	require.Error(t, err)
}

func TestValidateGenesis_Custom(t *testing.T) {
	genesis := types.NewGenesisState(
		types.DefaultParams(),
		math.LegacyNewDecWithPrec(5, 3),  // 0.5%
		math.LegacyNewDecWithPrec(10, 2), // 10%
		[]types.TaxCap{{Denom: "uusd", TaxCap: math.NewInt(1000000)}},
		sdk.NewCoins(sdk.NewCoin("uusd", math.NewInt(5000))),
		sdk.NewCoins(),
		[]types.EpochState{},
	)
	require.NoError(t, genesis.Validate())
}
