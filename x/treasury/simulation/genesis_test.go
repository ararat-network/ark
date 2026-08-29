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
	require.True(t, treasuryGenesis.MonetaryPolicy.StabilityTaxRate.IsZero())
	require.Equal(t, math.OneInt(), treasuryGenesis.Params.ReferenceTaxCap.Amount)
	require.False(t, treasuryGenesis.MonetaryPolicy.ValidatorBlockRewardTarget.IsNegative())
	require.False(t, treasuryGenesis.MonetaryPolicy.OracleBlockRewardTarget.IsNegative())
	require.True(
		t,
		treasuryGenesis.MonetaryPolicy.ValidatorBlockRewardTarget.LTE(chain.NativeBaseAmount(1)),
	)
	require.True(
		t,
		treasuryGenesis.MonetaryPolicy.OracleBlockRewardTarget.LTE(chain.NativeBaseAmount(1)),
	)
	require.False(t, treasuryGenesis.MonetaryPolicy.RedemptionBufferTargetRatio.IsNegative())
	require.False(t, treasuryGenesis.MonetaryPolicy.RedemptionBufferTargetRatio.GT(math.LegacyOneDec()))
	require.False(t, treasuryGenesis.MonetaryPolicy.StrategicReserveTargetRatio.IsNegative())
	require.False(t, treasuryGenesis.MonetaryPolicy.StrategicReserveTargetRatio.GT(math.LegacyOneDec()))
	require.False(t, treasuryGenesis.MonetaryPolicy.InsuranceTargetRatio.IsNegative())
	require.False(t, treasuryGenesis.MonetaryPolicy.InsuranceTargetRatio.GT(math.LegacyOneDec()))

	require.Empty(t, treasuryGenesis.ConversionFactors)
}

func TestRandomisedMonetaryPolicyDeterministic(t *testing.T) {
	first := simulation.RandomisedMonetaryPolicy(rand.New(rand.NewSource(1)))
	second := simulation.RandomisedMonetaryPolicy(rand.New(rand.NewSource(1)))

	require.True(t, first.Equal(second))
	require.NoError(t, first.Validate())
}

func TestRandomisedParamsDeterministic(t *testing.T) {
	first := simulation.RandomisedParams(rand.New(rand.NewSource(1)))
	second := simulation.RandomisedParams(rand.New(rand.NewSource(1)))

	require.Equal(t, first, second)
	require.NoError(t, first.Validate())
}
