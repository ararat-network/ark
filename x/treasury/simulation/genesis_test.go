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

	"ark/x/treasury/simulation"
	"ark/x/treasury/types"
)

func TestRandomisedGenState(t *testing.T) {
	interfaceRegistry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(interfaceRegistry)

	s := rand.NewSource(1)
	r := rand.New(s)

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

	// Params
	require.True(t, treasuryGenesis.Params.TaxPolicy.RateMin.GT(math.LegacyZeroDec()))
	require.True(t, treasuryGenesis.Params.TaxPolicy.RateMax.GT(treasuryGenesis.Params.TaxPolicy.RateMin))
	require.True(t, treasuryGenesis.Params.TaxPolicy.ChangeRateMax.GT(math.LegacyZeroDec()))
	require.True(t, treasuryGenesis.Params.RewardPolicy.RateMin.GT(math.LegacyZeroDec()))
	require.True(t, treasuryGenesis.Params.RewardPolicy.RateMax.GT(treasuryGenesis.Params.RewardPolicy.RateMin))
	require.True(t, treasuryGenesis.Params.RewardPolicy.ChangeRateMax.GT(math.LegacyZeroDec()))
	require.True(t, treasuryGenesis.Params.SeigniorageBurdenTarget.GTE(math.LegacyZeroDec()))
	require.False(t, treasuryGenesis.Params.BurnWeight.IsNil())
	require.True(t, treasuryGenesis.Params.BurnWeight.GTE(math.LegacyZeroDec()))
	require.True(t, treasuryGenesis.Params.BurnWeight.Add(treasuryGenesis.Params.RewardPolicy.RateMax).LTE(math.LegacyOneDec()))
	require.True(t, treasuryGenesis.Params.MiningIncrement.GT(math.LegacyZeroDec()))
	require.True(t, treasuryGenesis.Params.WindowShort > 0)
	require.True(t, treasuryGenesis.Params.WindowLong > 0)
	require.True(t, treasuryGenesis.Params.WindowProbation > 0)

	// Initial rates match policy minimums
	require.True(t, treasuryGenesis.TaxRate.Equal(treasuryGenesis.Params.TaxPolicy.RateMin))
	require.True(t, treasuryGenesis.RewardWeight.Equal(treasuryGenesis.Params.RewardPolicy.RateMin))

	// Empty initial state
	require.Empty(t, treasuryGenesis.TaxCaps)
	require.Empty(t, treasuryGenesis.EpochTaxProceeds)
	require.Empty(t, treasuryGenesis.EpochStates)
}

func TestRandomisedGenState_InvalidSimState(t *testing.T) {
	interfaceRegistry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(interfaceRegistry)

	s := rand.NewSource(1)
	r := rand.New(s)

	tests := []struct {
		simState module.SimulationState
		panicMsg string
	}{
		{module.SimulationState{}, "invalid memory address or nil pointer dereference"},
		{
			module.SimulationState{
				AppParams: make(simtypes.AppParams),
				Cdc:       cdc,
				Rand:      r,
			}, "assignment to entry in nil map",
		},
	}

	for _, tt := range tests {
		require.Panicsf(t, func() { simulation.RandomisedGenState(&tt.simState) }, tt.panicMsg)
	}
}
