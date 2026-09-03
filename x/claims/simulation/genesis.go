package simulation

// DONTCOVER

import (
	"encoding/json"
	"fmt"
	"math/rand"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/claims/types"
)

const claimsParamsKey = "claims_params"

// maxSimulatedClaimLimit is the total the seeded committee may pay out.
const maxSimulatedClaimLimit = int64(1_000_000_000_000)

// RandomisedParams returns valid governance-owned launch parameters.
func RandomisedParams(r *rand.Rand) types.Params {
	params := types.DefaultParams()
	params.ClaimCancellationPeriodBlocks = uint64(
		r.Int63n(int64(types.DefaultClaimCancellationPeriodBlocks)) + 1,
	)
	return params
}

// GenClaimsMandate appoints the Claims committee from the run's own accounts
// and opens the window at the first block for longer than any run lasts, so
// the committee surface is signable for the whole run. The limit is what the
// committee may pay out in total.
func GenClaimsMandate(r *rand.Rand, accounts []string) types.ClaimsMandate {
	if len(accounts) == 0 {
		return types.DefaultClaimsMandate()
	}

	appointment := types.NewDisabledClaimsMandate(1)
	appointment.Committee = accounts[r.Intn(len(accounts))]
	appointment.ActivationHeight = 1
	appointment.ExpiryHeight = chain.BlocksPerYear
	appointment.CommitteeClaimLimit = chain.NoahCoin(math.NewInt(maxSimulatedClaimLimit))

	return appointment
}

// RandomisedGenState generates a valid, launch-only Claims genesis state. The
// committee is appointed but no claims are seeded: every claim carries an
// Insurance reservation that must be backed by a Bank balance this generator
// does not control, so a random claim would produce genesis that InitGenesis
// correctly refuses.
func RandomisedGenState(simState *module.SimulationState) {
	var params types.Params
	simState.AppParams.GetOrGenerate(
		claimsParamsKey,
		&params,
		simState.Rand,
		func(r *rand.Rand) { params = RandomisedParams(r) },
	)

	accounts := make([]string, 0, len(simState.Accounts))
	for _, account := range simState.Accounts {
		accounts = append(accounts, account.Address.String())
	}

	claimsGenesis := types.DefaultGenesisState()
	claimsGenesis.Params = params
	claimsGenesis.ClaimsMandate = GenClaimsMandate(simState.Rand, accounts)

	bz, err := json.MarshalIndent(claimsGenesis, "", " ")
	if err != nil {
		panic(err)
	}

	fmt.Printf("Selected randomly generated Claims genesis:\n%s\n", bz)
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(claimsGenesis)
}
