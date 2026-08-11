package simulation

// DONTCOVER

import (
	"encoding/json"
	"fmt"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/types/module"

	"ark/x/security/types"
)

// Simulation parameter constants
const securityMandateKey = "security_mandate"

// GenSecurityMandate randomises the committee appointment, disabled most of
// the time as at launch. The enabled minority exists so genesis import, export,
// and the store decoder see a populated appointment; the committee itself never
// acts in simulation.
func GenSecurityMandate(r *rand.Rand, accounts []string) types.SecurityMandate {
	if len(accounts) == 0 || r.Intn(10) != 0 {
		return types.DefaultSecurityMandate()
	}

	activationHeight := uint64(1 + r.Intn(1000))
	appointment := types.NewDisabledSecurityMandate(uint64(1 + r.Intn(5)))
	appointment.Committee = accounts[r.Intn(len(accounts))]
	appointment.ActivationHeight = activationHeight
	appointment.ExpiryHeight = activationHeight + uint64(1+r.Intn(100000))

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
