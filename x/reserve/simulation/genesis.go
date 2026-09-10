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

// GenReserveMandate chooses a committee and deployment destination from simulation-owned accounts,
// with an active window covering the expected run and a positive deployment allowance.
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

// RandomisedGenState randomises only the appointment. Eligibility depends on Oracle state and
// recognition feeds Treasury, so other defaults remain independent of module-generation order.
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
