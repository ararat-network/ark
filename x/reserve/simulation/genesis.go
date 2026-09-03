package simulation

// DONTCOVER

import (
	"encoding/json"
	"fmt"
	"math/rand"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/reserve/types"
)

const (
	// simulatedDeploymentAllowance is what the seeded committee may deploy.
	simulatedDeploymentAllowance = int64(1_000_000_000_000)
	// simulatedMinimumNoahBalance is the floor the mandate holds the Reserve to.
	simulatedMinimumNoahBalance = int64(0)
)

// GenReserveMandate appoints the Reserve committee from the run's own accounts
// and opens the window at the first block for longer than any run lasts, so the
// committee surface is signable for the whole run. A configured mandate must
// allow a deployment and name somewhere to send it, so both come from the same
// account set.
func GenReserveMandate(r *rand.Rand, accounts []string) types.ReserveMandate {
	if len(accounts) < 2 {
		return types.DefaultReserveMandate()
	}

	committee := r.Intn(len(accounts))
	destination := (committee + 1) % len(accounts)

	appointment := types.NewDisabledReserveMandate(1)
	appointment.Committee = accounts[committee]
	appointment.ActivationHeight = 1
	appointment.ExpiryHeight = chain.BlocksPerYear
	appointment.DeploymentAllowance = chain.NoahCoin(math.NewInt(simulatedDeploymentAllowance))
	appointment.MinimumNoahBalance = chain.NoahCoin(math.NewInt(simulatedMinimumNoahBalance))
	appointment.Destinations = []string{accounts[destination]}

	return appointment
}

// RandomisedGenState keeps the valid default Reserve state and randomises only
// the appointment. The rest stays as shipped for the reason the module states:
// eligibility entries reference oracle feeds and recognised capital is read by
// Treasury, which the sorted per-module generation order cannot provide. An
// appointment references neither, so it is safe to draw here.
func RandomisedGenState(simState *module.SimulationState) {
	accounts := make([]string, 0, len(simState.Accounts))
	for _, account := range simState.Accounts {
		accounts = append(accounts, account.Address.String())
	}

	reserveGenesis := types.DefaultGenesisState()
	reserveGenesis.Mandate = GenReserveMandate(simState.Rand, accounts)

	bz, err := json.MarshalIndent(reserveGenesis, "", " ")
	if err != nil {
		panic(err)
	}

	fmt.Printf("Selected randomly generated Reserve genesis:\n%s\n", bz)
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(reserveGenesis)
}
