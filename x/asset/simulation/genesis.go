package simulation

// DONTCOVER

import (
	"encoding/json"
	"fmt"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/asset/types"
)

// GenEmergencyMandate appoints an account the simulation can sign for, its window open for the whole
// run.
func GenEmergencyMandate(r *rand.Rand, accounts []string) types.EmergencyMandate {
	if len(accounts) == 0 {
		return types.DefaultEmergencyMandate()
	}

	appointment := types.NewDisabledEmergencyMandate(1)
	appointment.Committee = accounts[r.Intn(len(accounts))]
	// Open from the epoch to the header's last instant, so the window spans whatever genesis time the
	// simulation draws.
	appointment.ActivationTime = 1
	appointment.ExpiryTime = chain.MaxBlockSeconds

	return appointment
}

// RandomisedGenState keeps the launch asset registry and randomises only the
// emergency appointment. The registry itself stays as shipped: an asset's
// status is coupled to Bank supply and to feeds this generator does not
// control, so the lifecycle is driven by messages during the run instead.
func RandomisedGenState(simState *module.SimulationState) {
	accounts := make([]string, 0, len(simState.Accounts))
	for _, account := range simState.Accounts {
		accounts = append(accounts, account.Address.String())
	}

	assetGenesis := types.DefaultGenesisState()
	assetGenesis.EmergencyMandate = GenEmergencyMandate(simState.Rand, accounts)

	bz, err := json.MarshalIndent(assetGenesis, "", " ")
	if err != nil {
		panic(err)
	}

	fmt.Printf("Selected randomly generated Asset genesis:\n%s\n", bz)
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(assetGenesis)
}
