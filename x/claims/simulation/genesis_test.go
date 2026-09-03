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

	"github.com/ararat-network/ark/x/claims/simulation"
	"github.com/ararat-network/ark/x/claims/types"
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

	accountAddresses := make([]string, 0, len(simState.Accounts))
	for _, account := range simState.Accounts {
		accountAddresses = append(accountAddresses, account.Address.String())
	}

	simulation.RandomisedGenState(&simState)

	var claimsGenesis types.GenesisState
	simState.Cdc.MustUnmarshalJSON(simState.GenState[types.ModuleName], &claimsGenesis)

	require.Positive(t, claimsGenesis.Params.ClaimCancellationPeriodBlocks)
	require.LessOrEqual(
		t,
		claimsGenesis.Params.ClaimCancellationPeriodBlocks,
		types.DefaultClaimCancellationPeriodBlocks,
	)

	// The committee is appointed from the run's own accounts so the simulation
	// can sign for it. No claim is seeded: every claim carries an Insurance
	// reservation that must be backed by a Bank balance the generator does not
	// control, which is what keeps the generated genesis launchable as produced.
	require.False(t, claimsGenesis.ClaimsMandate.IsDisabled())
	require.Contains(t, accountAddresses, claimsGenesis.ClaimsMandate.Committee)
	require.True(t, claimsGenesis.ClaimsMandate.CommitteeClaimLimit.IsPositive())
	require.Empty(t, claimsGenesis.Claims)
	require.True(t, claimsGenesis.ClaimsAllowanceUsed.IsZero())
	require.True(t, claimsGenesis.InsuranceReserved.IsZero())
	require.NoError(t, claimsGenesis.Validate())
}

// TestRandomisedParamsStayInRange draws repeatedly rather than once, because a
// single seeded draw cannot show that the generator's whole range validates.
func TestRandomisedParamsStayInRange(t *testing.T) {
	r := rand.New(rand.NewSource(1))

	for range 100 {
		params := simulation.RandomisedParams(r)
		require.Positive(t, params.ClaimCancellationPeriodBlocks)
		require.LessOrEqual(
			t,
			params.ClaimCancellationPeriodBlocks,
			types.DefaultClaimCancellationPeriodBlocks,
		)
		require.NoError(t, params.Validate())
	}
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
