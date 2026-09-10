package simulation

// DONTCOVER

import (
	"encoding/json"
	"fmt"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/security/types"
)

// Simulation parameter constants
const securityMandateKey = "security_mandate"

// GenSecurityMandate appoints a simulation-owned signer with a window covering the expected run,
// yielding a valid reachable appointment.
func GenSecurityMandate(r *rand.Rand, accounts []string) types.SecurityMandate {
	if len(accounts) == 0 {
		return types.DefaultSecurityMandate()
	}

	appointment := types.NewDisabledSecurityMandate(1)
	appointment.Committee = accounts[r.Intn(len(accounts))]
	appointment.ActivationHeight = 1
	appointment.ExpiryHeight = chain.BlocksPerYear

	return appointment
}

// RandomisedGenState generates a random GenesisState for the security module.
func RandomisedGenState(simState *module.SimulationState) {
	accounts := make([]string, 0, len(simState.Accounts))
	for _, account := range simState.Accounts {
		accounts = append(accounts, account.Address.String())
	}

	var securityMandate types.SecurityMandate
	simState.AppParams.GetOrGenerate(
		securityMandateKey,
		&securityMandate,
		simState.Rand,
		func(r *rand.Rand) { securityMandate = GenSecurityMandate(r, accounts) },
	)

	// The plan record is always empty: one naming a plan that does not exist is
	// the stale state the module already distrusts.
	genesisState := types.NewGenesisState(securityMandate, types.CommitteePlan{})

	bz, err := json.MarshalIndent(&genesisState, "", " ")
	if err != nil {
		panic(err)
	}
	fmt.Printf("Selected randomly generated security parameters:\n%s\n", bz)

	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(genesisState)
}
