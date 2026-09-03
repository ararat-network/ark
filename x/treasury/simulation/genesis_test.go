package simulation_test

import (
	"encoding/json"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/treasury/simulation"
	"github.com/ararat-network/ark/x/treasury/types"
)

func TestRandomisedGenState(t *testing.T) {
	interfaceRegistry := codectypes.NewInterfaceRegistry()
	types.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)
	r := rand.New(rand.NewSource(1))

	simState := module.SimulationState{
		AppParams:    make(simtypes.AppParams),
		Cdc:          cdc,
		Rand:         r,
		NumBonded:    3,
		BondDenom:    sdk.DefaultBondDenom,
		Accounts:     simtypes.RandomAccounts(r, 3),
		InitialStake: math.NewInt(1000),
		GenState:     make(map[string]json.RawMessage),
	}

	simulation.RandomisedGenState(&simState)

	var treasuryGenesis types.GenesisState
	simState.Cdc.MustUnmarshalJSON(simState.GenState[types.ModuleName], &treasuryGenesis)

	require.NoError(t, treasuryGenesis.Validate())
	require.True(t, treasuryGenesis.Params.TransferTaxRate.IsZero())
	require.Equal(t, math.OneInt(), treasuryGenesis.Params.ReferenceTaxCap)
	require.False(t, treasuryGenesis.EconomicPolicy.ValidatorBlockRewardTarget.IsNegative())
	require.False(t, treasuryGenesis.EconomicPolicy.OracleBlockRewardTarget.IsNegative())
	require.True(
		t,
		treasuryGenesis.EconomicPolicy.ValidatorBlockRewardTarget.LTE(chain.NativeBaseAmount(1)),
	)
	require.True(
		t,
		treasuryGenesis.EconomicPolicy.OracleBlockRewardTarget.LTE(chain.NativeBaseAmount(1)),
	)
	require.False(t, treasuryGenesis.EconomicPolicy.RedemptionBufferTargetRatio.IsNegative())
	require.False(t, treasuryGenesis.EconomicPolicy.RedemptionBufferTargetRatio.GT(math.LegacyOneDec()))
	require.False(t, treasuryGenesis.EconomicPolicy.StrategicReserveTargetRatio.IsNegative())
	require.False(t, treasuryGenesis.EconomicPolicy.StrategicReserveTargetRatio.GT(math.LegacyOneDec()))
	require.False(t, treasuryGenesis.EconomicPolicy.InsuranceTargetRatio.IsNegative())
	require.False(t, treasuryGenesis.EconomicPolicy.InsuranceTargetRatio.GT(math.LegacyOneDec()))

	// Only the NOAH seed the default carries; member factors derive at runtime.
	require.Equal(t, []types.ConversionFactor{
		{Denom: chain.NoahBaseDenom, Factor: types.DefaultNoahConversionFactor},
	}, treasuryGenesis.ConversionFactors)
}

func TestRandomisedEconomicPolicyDeterministic(t *testing.T) {
	first := simulation.RandomisedEconomicPolicy(rand.New(rand.NewSource(1)))
	second := simulation.RandomisedEconomicPolicy(rand.New(rand.NewSource(1)))

	require.True(t, first.Equal(second))
	require.NoError(t, first.Validate())
}

func TestRandomisedParamsDeterministic(t *testing.T) {
	first := simulation.RandomisedParams(rand.New(rand.NewSource(1)))
	second := simulation.RandomisedParams(rand.New(rand.NewSource(1)))

	require.Equal(t, first, second)
	require.NoError(t, first.Validate())
}
