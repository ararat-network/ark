package simulation

// DONTCOVER

import (
	"encoding/json"
	"fmt"
	"math/rand"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/types/module"

	"ark/x/treasury/types"
)

const (
	treasuryParamsKey         = "treasury_params"
	treasuryMonetaryPolicyKey = "treasury_monetary_policy"
	maxSimulatedRewardTarget  = int64(1_000_000_000_000_000_000)
)

// GenUnitIntervalDec returns a representable decimal in [0, 1].
func GenUnitIntervalDec(r *rand.Rand) math.LegacyDec {
	return math.LegacyNewDecWithPrec(int64(r.Intn(10_001)), 4)
}

// GenRewardTarget returns a nonnegative base-unit NOAH block reward target.
func GenRewardTarget(r *rand.Rand) math.Int {
	return math.NewInt(r.Int63n(maxSimulatedRewardTarget + 1))
}

// RandomisedParams returns valid governance-owned launch parameters.
func RandomisedParams(r *rand.Rand) types.Params {
	params := types.DefaultParams()
	params.RewardFundingWindow = uint64(r.Int63n(int64(types.DefaultRewardFundingWindow)) + 1)
	return params
}

// RandomisedMonetaryPolicy returns valid launch policy. Stability tax remains
// disabled; keeper InitGenesis seeds every member's cap at the reference
// amount, so no exchange rates are needed regardless of the cap.
func RandomisedMonetaryPolicy(r *rand.Rand) types.MonetaryPolicy {
	policy := types.DefaultMonetaryPolicy()
	policy.ValidatorBlockRewardTarget = GenRewardTarget(r)
	policy.OracleBlockRewardTarget = GenRewardTarget(r)
	policy.RedemptionBufferTargetRatio = GenUnitIntervalDec(r)
	policy.StrategicReserveTargetRatio = GenUnitIntervalDec(r)
	policy.InsuranceTargetRatio = GenUnitIntervalDec(r)
	return policy
}

// RandomisedGenState generates a valid, launch-only Treasury genesis state.
func RandomisedGenState(simState *module.SimulationState) {
	var params types.Params
	simState.AppParams.GetOrGenerate(
		treasuryParamsKey,
		&params,
		simState.Rand,
		func(r *rand.Rand) { params = RandomisedParams(r) },
	)
	var policy types.MonetaryPolicy
	simState.AppParams.GetOrGenerate(
		treasuryMonetaryPolicyKey,
		&policy,
		simState.Rand,
		func(r *rand.Rand) { policy = RandomisedMonetaryPolicy(r) },
	)

	treasuryGenesis := types.DefaultGenesisState()
	treasuryGenesis.Params = params
	treasuryGenesis.MonetaryPolicy = policy

	bz, err := json.MarshalIndent(treasuryGenesis, "", " ")
	if err != nil {
		panic(err)
	}

	fmt.Printf("Selected randomly generated Treasury genesis:\n%s\n", bz)
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(treasuryGenesis)
}
