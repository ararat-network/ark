// SPDX-License-Identifier: Apache-2.0
// Originates from Ark's Terra Classic port of x/treasury/simulation/genesis.go.
// Modified for Ark: genesis construction, validation, and state integration.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package simulation

// DONTCOVER

import (
	"encoding/json"
	"fmt"
	"math/rand"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/treasury/types"
)

const (
	treasuryParamsKey         = "treasury_params"
	treasuryEconomicPolicyKey = "treasury_economic_policy"
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

// RandomisedParams returns valid governance-owned launch parameters. The
// transfer tax stays disabled; keeper InitGenesis seeds every member's cap at
// the reference amount, so no exchange rates are needed regardless of the cap.
func RandomisedParams(r *rand.Rand) types.Params {
	params := types.DefaultParams()
	params.RewardFundingWindow = uint64(r.Int63n(int64(types.DefaultRewardFundingWindow)) + 1)
	return params
}

// RandomisedEconomicPolicy returns valid launch policy.
func RandomisedEconomicPolicy(r *rand.Rand) types.EconomicPolicy {
	policy := types.DefaultEconomicPolicy()
	policy.ValidatorBlockRewardTarget = GenRewardTarget(r)
	policy.OracleBlockRewardTarget = GenRewardTarget(r)
	policy.RedemptionBufferTargetRatio = GenUnitIntervalDec(r)
	policy.StrategicReserveTargetRatio = GenUnitIntervalDec(r)
	policy.InsuranceTargetRatio = GenUnitIntervalDec(r)
	return policy
}

// GenEconomicMandate appoints a simulation-owned committee for the expected run, with a corridor
// from zero to a generated policy so delegated updates have room to vary.
func GenEconomicMandate(r *rand.Rand, accounts []string) types.EconomicMandate {
	if len(accounts) == 0 {
		return types.DefaultEconomicMandate()
	}

	appointment := types.NewDisabledEconomicMandate(1)
	appointment.Committee = accounts[r.Intn(len(accounts))]
	appointment.ActivationHeight = 1
	appointment.ExpiryHeight = chain.BlocksPerYear
	appointment.MinimumPolicy = types.DefaultEconomicPolicy()
	appointment.MaximumPolicy = RandomisedEconomicPolicy(r)

	return appointment
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
	var policy types.EconomicPolicy
	simState.AppParams.GetOrGenerate(
		treasuryEconomicPolicyKey,
		&policy,
		simState.Rand,
		func(r *rand.Rand) { policy = RandomisedEconomicPolicy(r) },
	)

	accounts := make([]string, 0, len(simState.Accounts))
	for _, account := range simState.Accounts {
		accounts = append(accounts, account.Address.String())
	}

	treasuryGenesis := types.DefaultGenesisState()
	treasuryGenesis.Params = params
	treasuryGenesis.EconomicMandate = GenEconomicMandate(simState.Rand, accounts)
	treasuryGenesis.EconomicPolicy = policy
	// The controller starts at whatever floor the params drew, keeping the
	// genesis invariant that the price never sits below it.
	treasuryGenesis.BaseGasPrice = params.MinBaseGasPrice

	bz, err := json.MarshalIndent(treasuryGenesis, "", " ")
	if err != nil {
		panic(err)
	}

	fmt.Printf("Selected randomly generated Treasury genesis:\n%s\n", bz)
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(treasuryGenesis)
}
