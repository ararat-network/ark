package simulation

// DONTCOVER

import (
	"encoding/json"
	"fmt"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/ararat-network/ark/x/claims/types"
)

const claimsParamsKey = "claims_params"

// RandomisedParams returns valid governance-owned launch parameters.
func RandomisedParams(r *rand.Rand) types.Params {
	params := types.DefaultParams()
	params.ClaimCancellationPeriodBlocks = uint64(
		r.Int63n(int64(types.DefaultClaimCancellationPeriodBlocks)) + 1,
	)
	return params
}

// RandomisedGenState generates a valid, launch-only Claims genesis state. The
// mandate stays disabled and no claims are seeded: every claim carries an
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

	claimsGenesis := types.DefaultGenesisState()
	claimsGenesis.Params = params

	bz, err := json.MarshalIndent(claimsGenesis, "", " ")
	if err != nil {
		panic(err)
	}

	fmt.Printf("Selected randomly generated Claims genesis:\n%s\n", bz)
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(claimsGenesis)
}
