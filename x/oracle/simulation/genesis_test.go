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

	"ark/x/oracle/simulation"
	"ark/x/oracle/types"
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

	var oracleGenesis types.GenesisState
	simState.Cdc.MustUnmarshalJSON(simState.GenState[types.ModuleName], &oracleGenesis)

	require.True(t, oracleGenesis.Params.VoteThreshold.GTE(types.MinVoteThreshold))
	require.True(t, oracleGenesis.Params.VoteThreshold.LTE(math.LegacyOneDec()))
	require.False(t, oracleGenesis.Params.RewardBand.IsNegative())
	require.True(t, oracleGenesis.Params.RewardWindow > 0)
	require.True(t, oracleGenesis.Params.RewardDistributionWindow >= 100)
	require.True(t, oracleGenesis.Params.AttendanceWindow >= 100)
	require.True(t, oracleGenesis.Params.MinAttendancePerWindow.GTE(math.LegacyZeroDec()))
	require.True(t, oracleGenesis.Params.MinAttendancePerWindow.LTE(math.LegacyOneDec()))
	require.True(t, oracleGenesis.Params.FunctioningBlockThreshold.GTE(types.MinFunctioningBlockThreshold))
	require.True(t, oracleGenesis.Params.FunctioningBlockThreshold.LTE(math.LegacyOneDec()))
	require.NotEmpty(t, oracleGenesis.Params.TobinTaxes)
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
