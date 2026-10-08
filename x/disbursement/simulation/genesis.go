package simulation

// DONTCOVER

import (
	"encoding/json"
	"fmt"
	"math/rand"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/disbursement/types"
)

const disbursementParamsKey = "disbursement_params"

// maxSimulatedAllowance bounds what the seeded committee may award in a term, in the reference unit.
const maxSimulatedAllowance = int64(1_000_000_000_000_000)

// simulatedSchedule draws a shape whose periods last hours, so a run's blocks, each 5,000 to 10,000
// seconds apart, cross its boundaries and EndBlock pays.
func simulatedSchedule(r *rand.Rand, maxPeriods int) types.Schedule {
	return types.Schedule{
		FirstLength:  uint64(r.Int63n(86_400) + 1),
		FirstWeight:  uint64(r.Int63n(10) + 1),
		PeriodLength: uint64(r.Int63n(86_400) + 3_600),
		PeriodWeight: uint64(r.Int63n(10) + 1),
		Periods:      uint32(r.Intn(maxPeriods + 1)),
	}
}

// RandomisedParams returns valid parameters on simulation scale: amounts small against the seeded
// custody, schedules short against the run. The compensation period keeps its one-week floor.
func RandomisedParams(r *rand.Rand) types.Params {
	params := types.DefaultParams()
	params.MemberSchedule = simulatedSchedule(r, 24)
	// Each period's share floors, so an amount of at least the total weight pays every period.
	params.MemberAmount = math.NewIntFromUint64(params.MemberSchedule.TotalWeight()).MulRaw(r.Int63n(1_000_000) + 1_000)
	params.CompensationCap = math.NewInt(r.Int63n(maxSimulatedAllowance) + 1)
	params.CompensationPeriod = types.MinCompensationPeriod + uint64(r.Int63n(int64(3*types.MinCompensationPeriod)))
	params.OwnershipCap = math.NewInt(r.Int63n(maxSimulatedAllowance) + 1)
	params.OwnershipSchedule = simulatedSchedule(r, 78)
	return params
}

// GenGrantsMandate appoints the committee from the run's own accounts for longer than any run lasts,
// so the committee surface is signable throughout.
func GenGrantsMandate(r *rand.Rand, accounts []string) types.GrantsMandate {
	if len(accounts) == 0 {
		return types.DefaultGrantsMandate()
	}
	appointment := types.NewDisabledGrantsMandate(1)
	appointment.Committee = accounts[r.Intn(len(accounts))]
	appointment.ActivationHeight = 1
	appointment.ExpiryHeight = chain.BlocksPerYear
	appointment.CompensationAllowance = math.NewInt(r.Int63n(maxSimulatedAllowance) + 1)
	return appointment
}

// GenRegistrarMandate appoints a registrar from the run's accounts over the committee's whole window,
// the widest the committee could grant.
func GenRegistrarMandate(r *rand.Rand, accounts []string, committee types.GrantsMandate) types.RegistrarMandate {
	if len(accounts) == 0 || committee.IsDisabled() {
		return types.DefaultRegistrarMandate()
	}
	appointment := types.NewDisabledRegistrarMandate(1)
	appointment.Committee = accounts[r.Intn(len(accounts))]
	appointment.ActivationHeight = committee.ActivationHeight
	appointment.ExpiryHeight = committee.ExpiryHeight
	return appointment
}

// genMemberTranche opens part of the member account's genesis balance, which bank's generator, sorted
// ahead of this one, has already written. Without it registration waits on a tranche proposal that a
// short run rarely sees pass.
func genMemberTranche(r *rand.Rand, simState *module.SimulationState) math.Int {
	raw, ok := simState.GenState[banktypes.ModuleName]
	if !ok {
		return math.ZeroInt()
	}
	var bankGenesis banktypes.GenesisState
	simState.Cdc.MustUnmarshalJSON(raw, &bankGenesis)
	members := authtypes.NewModuleAddress(types.MembersName).String()
	for _, balance := range bankGenesis.Balances {
		if balance.Address != members {
			continue
		}
		held := balance.Coins.AmountOf(chain.NoahBaseDenom)
		if !held.IsPositive() {
			return math.ZeroInt()
		}
		open, err := simtypes.RandPositiveInt(r, held)
		if err != nil {
			panic(err)
		}
		return open
	}
	return math.ZeroInt()
}

// RandomisedGenState appoints a committee and a registrar and opens a member tranche, but seeds no
// grants: a grant's reservation must match Bank custody this generator does not control.
func RandomisedGenState(simState *module.SimulationState) {
	var params types.Params
	simState.AppParams.GetOrGenerate(
		disbursementParamsKey,
		&params,
		simState.Rand,
		func(r *rand.Rand) { params = RandomisedParams(r) },
	)

	accounts := make([]string, 0, len(simState.Accounts))
	for _, account := range simState.Accounts {
		accounts = append(accounts, account.Address.String())
	}

	disbursementGenesis := types.DefaultGenesisState()
	disbursementGenesis.Params = params
	disbursementGenesis.GrantsMandate = GenGrantsMandate(simState.Rand, accounts)
	disbursementGenesis.RegistrarMandate = GenRegistrarMandate(simState.Rand, accounts, disbursementGenesis.GrantsMandate)
	disbursementGenesis.MemberTranche = genMemberTranche(simState.Rand, simState)

	bz, err := json.MarshalIndent(disbursementGenesis, "", " ")
	if err != nil {
		panic(err)
	}

	fmt.Printf("Selected randomly generated Disbursement genesis:\n%s\n", bz)
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(disbursementGenesis)
}
