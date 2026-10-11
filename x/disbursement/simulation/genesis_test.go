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
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/disbursement/simulation"
	"github.com/ararat-network/ark/x/disbursement/types"
)

func newSimState(t *testing.T, memberCustody int64) (module.SimulationState, []string) {
	t.Helper()

	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
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
	// Bank's generator runs first in a real run; this stands in for its seeded member custody.
	if memberCustody > 0 {
		simState.GenState[banktypes.ModuleName] = cdc.MustMarshalJSON(&banktypes.GenesisState{
			Balances: []banktypes.Balance{{
				Address: authtypes.NewModuleAddress(types.MembersName).String(),
				Coins:   sdk.NewCoins(chain.NoahCoin(math.NewInt(memberCustody))),
			}},
		})
	}

	addresses := make([]string, 0, len(simState.Accounts))
	for _, account := range simState.Accounts {
		addresses = append(addresses, account.Address.String())
	}
	return simState, addresses
}

func TestRandomisedGenState(t *testing.T) {
	const memberCustody = int64(1_000_000_000)
	simState, addresses := newSimState(t, memberCustody)

	simulation.RandomisedGenState(&simState)

	var gs types.GenesisState
	simState.Cdc.MustUnmarshalJSON(simState.GenState[types.ModuleName], &gs)
	require.NoError(t, gs.Validate())

	// Both appointments come from the run's accounts so the simulation can sign for them, and the
	// registrar holds the committee's whole window.
	require.False(t, gs.GrantsMandate.IsDisabled())
	require.Contains(t, addresses, gs.GrantsMandate.Committee)
	require.True(t, gs.GrantsMandate.CompensationAllowance.IsPositive())
	require.False(t, gs.RegistrarMandate.IsDisabled())
	require.Contains(t, addresses, gs.RegistrarMandate.Committee)
	require.Equal(t, gs.GrantsMandate.ActivationTime, gs.RegistrarMandate.ActivationTime)
	require.Equal(t, gs.GrantsMandate.ExpiryTime, gs.RegistrarMandate.ExpiryTime)

	// The tranche is drawn from the seeded member custody, which import checks it against.
	require.Empty(t, gs.Books)
	require.True(t, gs.MemberTranche.IsPositive())
	require.True(t, gs.MemberTranche.LTE(math.NewInt(memberCustody)))

	// No grant is seeded: its reservation must match Bank custody the generator does not control.
	require.Empty(t, gs.Grants)
	require.True(t, gs.CompensationUsed.IsZero())
}

// Without seeded member custody the generator opens no tranche, which import would refuse as
// unbacked.
func TestRandomisedGenStateWithoutMemberCustody(t *testing.T) {
	simState, _ := newSimState(t, 0)

	simulation.RandomisedGenState(&simState)

	var gs types.GenesisState
	simState.Cdc.MustUnmarshalJSON(simState.GenState[types.ModuleName], &gs)
	require.NoError(t, gs.Validate())
	require.True(t, gs.MemberTranche.IsZero())
}

// TestRandomisedParamsStayInRange draws repeatedly, because one seeded draw cannot show the whole
// range validates.
func TestRandomisedParamsStayInRange(t *testing.T) {
	r := rand.New(rand.NewSource(1))

	for range 100 {
		params := simulation.RandomisedParams(r)
		require.NoError(t, params.Validate())
		// The factories award up to twelve periods.
		_, err := types.CompensationSchedule(params.CompensationPeriod, 12)
		require.NoError(t, err)
		_, _, _, err = params.OwnershipSchedule.Shares(math.NewInt(1_000_000))
		require.NoError(t, err)
	}
}
