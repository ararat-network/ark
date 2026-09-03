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

	"github.com/ararat-network/ark/x/security/simulation"
	"github.com/ararat-network/ark/x/security/types"
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

	var securityGenesis types.GenesisState
	simState.Cdc.MustUnmarshalJSON(simState.GenState[types.ModuleName], &securityGenesis)

	// The plan record is always empty: one naming a plan that does not exist is
	// the stale state the module already distrusts.
	require.True(t, securityGenesis.CommitteePlan.IsZero())
	require.NoError(t, securityGenesis.Validate())
}

// TestGenSecurityMandateWithoutAccounts pins the branch a simulation with no
// accounts takes: there is no address to appoint, so the mandate must come back
// disabled rather than naming an account that does not exist.
func TestGenSecurityMandateWithoutAccounts(t *testing.T) {
	r := rand.New(rand.NewSource(1))

	for range 100 {
		mandate := simulation.GenSecurityMandate(r, nil)
		require.True(t, mandate.IsDisabled())
		require.NoError(t, mandate.Validate())
	}
}

// TestGenSecurityMandateAppointments draws repeatedly because the enabled
// appointment is a one-in-ten branch: a single seeded draw would usually miss
// it, and it is the branch that has fields to get wrong.
func TestGenSecurityMandateAppointments(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	accounts := make([]string, 0, 3)
	for _, account := range simtypes.RandomAccounts(r, 3) {
		accounts = append(accounts, account.Address.String())
	}

	// Every run appoints: a run without a committee exercises none of the
	// committee surface, and the disabled shape is round-tripped by the keeper
	// genesis tests rather than by chance here.
	for range 500 {
		mandate := simulation.GenSecurityMandate(r, accounts)

		require.NoError(t, mandate.Validate())
		require.False(t, mandate.IsDisabled())
		require.Contains(t, accounts, mandate.Committee)
		require.Positive(t, mandate.ActivationHeight)
		require.Greater(t, mandate.ExpiryHeight, mandate.ActivationHeight)
	}

	// Without accounts there is nobody to appoint, which is the empty case.
	require.True(t, simulation.GenSecurityMandate(r, nil).IsDisabled())
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
