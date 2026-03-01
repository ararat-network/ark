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

	"noah/x/market/simulation"
	"noah/x/market/types"
)

func TestRandomizedGenState(t *testing.T) {
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

	simulation.RandomizedGenState(&simState)

	var marketGenesis types.GenesisState
	simState.Cdc.MustUnmarshalJSON(simState.GenState[types.ModuleName], &marketGenesis)

	require.True(t, marketGenesis.Params.BasePool.GT(math.LegacyZeroDec()))
	require.True(t, marketGenesis.Params.PoolRecoveryPeriod > 0)
	require.True(t, marketGenesis.Params.MinStabilitySpread.GT(math.LegacyZeroDec()))
	require.True(t, marketGenesis.NoahPoolDelta.IsZero())
}

func TestRandomizedGenState_InvalidSimState(t *testing.T) {
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
		require.Panicsf(t, func() { simulation.RandomizedGenState(&tt.simState) }, tt.panicMsg)
	}
}
