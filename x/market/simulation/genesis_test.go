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

	chain "github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/market/simulation"
	"github.com/ararat-network/ark/x/market/types"
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

	var marketGenesis types.GenesisState
	simState.Cdc.MustUnmarshalJSON(simState.GenState[types.ModuleName], &marketGenesis)

	require.Equal(t, chain.SDRBaseDenom, marketGenesis.ConversionPolicy.BasePool.Denom)
	require.True(
		t,
		marketGenesis.ConversionPolicy.BasePool.Amount.GTE(
			math.LegacyNewDecFromInt(chain.NativeBaseAmount(50_000_000)),
		),
	)
	require.True(
		t,
		marketGenesis.ConversionPolicy.BasePool.Amount.LT(
			math.LegacyNewDecFromInt(chain.NativeBaseAmount(50_010_000)),
		),
	)
	require.True(t, marketGenesis.ConversionPolicy.PoolRecoveryPeriod > 0)
	require.True(t, marketGenesis.ConversionPolicy.MinStabilitySpread.GT(math.LegacyZeroDec()))
	require.True(t, marketGenesis.ArkPoolDelta.IsZero())
	// Simulation never appoints a committee, and the generated genesis must be
	// launchable as produced.
	require.True(t, marketGenesis.ConversionMandate.IsDisabled())
	require.NoError(t, marketGenesis.Validate())
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
