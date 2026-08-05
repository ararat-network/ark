package simulation

// DONTCOVER

import (
	"encoding/json"
	"fmt"
	"math/rand"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/types/module"

	chain "ark/pkg/chain"
	"ark/x/oracle/types"
)

// Simulation parameter constants
const (
	voteThresholdKey            = "vote_threshold"
	rewardBandKey               = "reward_band"
	rewardWindowKey             = "reward_window"
	rewardDistributionWindowKey = "reward_distribution_window"
	attendanceWindowKey         = "attendance_window"
	minAttendancePerWindowKey   = "min_attendance_per_window"

	functioningBlockThresholdKey = "functioning_block_threshold"
	participationThresholdKey    = "participation_threshold"
)

// GenVoteThreshold randomised VoteThreshold
func GenVoteThreshold(r *rand.Rand) math.LegacyDec {
	return types.MinVoteThreshold.Add(math.LegacyNewDecWithPrec(int64(r.Intn(501)), 3))
}

// GenRewardBand randomised RewardBand
func GenRewardBand(r *rand.Rand) math.LegacyDec {
	return math.LegacyZeroDec().Add(math.LegacyNewDecWithPrec(int64(r.Intn(100)), 3))
}

// GenRewardWindow randomised VotePeriod
func GenRewardWindow(r *rand.Rand) uint64 {
	return uint64(1 + r.Intn(100))
}

// GenRewardDistributionWindow randomised RewardDistributionWindow
func GenRewardDistributionWindow(r *rand.Rand) uint64 {
	return uint64(100 + r.Intn(100000))
}

// GenAttendanceWindow randomised AttendanceWindow
func GenAttendanceWindow(r *rand.Rand) uint64 {
	return uint64(100 + r.Intn(100000))
}

// GenMinAttendancePerWindow randomised MinAttendancePerWindow
func GenMinAttendancePerWindow(r *rand.Rand) math.LegacyDec {
	return math.LegacyZeroDec().Add(math.LegacyNewDecWithPrec(int64(r.Intn(500)), 3))
}

// GenFunctioningBlockThreshold randomised FunctioningBlockThreshold across its
// full legal range of [50%, 100%].
func GenFunctioningBlockThreshold(r *rand.Rand) math.LegacyDec {
	return types.MinFunctioningBlockThreshold.Add(math.LegacyNewDecWithPrec(int64(r.Intn(501)), 3))
}

// GenParticipationThreshold randomised ParticipationThreshold across its full
// legal range of [0%, 50%].
func GenParticipationThreshold(r *rand.Rand) math.LegacyDec {
	return math.LegacyNewDecWithPrec(int64(r.Intn(501)), 3)
}

// RandomisedGenState generates a random GenesisState for oracle
func RandomisedGenState(simState *module.SimulationState) {
	var voteThreshold math.LegacyDec
	simState.AppParams.GetOrGenerate(
		voteThresholdKey, &voteThreshold, simState.Rand,
		func(r *rand.Rand) { voteThreshold = GenVoteThreshold(r) },
	)

	var rewardBand math.LegacyDec
	simState.AppParams.GetOrGenerate(
		rewardBandKey, &rewardBand, simState.Rand,
		func(r *rand.Rand) { rewardBand = GenRewardBand(r) },
	)

	var rewardWindow uint64
	simState.AppParams.GetOrGenerate(
		rewardWindowKey, &rewardWindow, simState.Rand,
		func(r *rand.Rand) { rewardWindow = GenRewardWindow(r) },
	)

	var rewardDistributionWindow uint64
	simState.AppParams.GetOrGenerate(
		rewardDistributionWindowKey, &rewardDistributionWindow, simState.Rand,
		func(r *rand.Rand) { rewardDistributionWindow = GenRewardDistributionWindow(r) },
	)

	var attendanceWindow uint64
	simState.AppParams.GetOrGenerate(
		attendanceWindowKey, &attendanceWindow, simState.Rand,
		func(r *rand.Rand) { attendanceWindow = GenAttendanceWindow(r) },
	)

	var minAttendancePerWindow math.LegacyDec
	simState.AppParams.GetOrGenerate(
		minAttendancePerWindowKey, &minAttendancePerWindow, simState.Rand,
		func(r *rand.Rand) { minAttendancePerWindow = GenMinAttendancePerWindow(r) },
	)

	var functioningBlockThreshold math.LegacyDec
	simState.AppParams.GetOrGenerate(
		functioningBlockThresholdKey, &functioningBlockThreshold, simState.Rand,
		func(r *rand.Rand) { functioningBlockThreshold = GenFunctioningBlockThreshold(r) },
	)

	var participationThreshold math.LegacyDec
	simState.AppParams.GetOrGenerate(
		participationThresholdKey, &participationThreshold, simState.Rand,
		func(r *rand.Rand) { participationThreshold = GenParticipationThreshold(r) },
	)

	params := types.Params{
		VoteThreshold:             voteThreshold,
		RewardBand:                rewardBand,
		RewardWindow:              rewardWindow,
		RewardDistributionWindow:  rewardDistributionWindow,
		AttendanceWindow:          attendanceWindow,
		MinAttendancePerWindow:    minAttendancePerWindow,
		MaxExchangeRateAge:        types.DefaultMaxExchangeRateAge,
		FunctioningBlockThreshold: functioningBlockThreshold,
		ParticipationThreshold:    participationThreshold,
	}
	oracleGenesis := types.NewGenesisState(
		params,
		[]types.ExchangeRate{},
		[]types.RewardWeight{},
		[]types.AttendanceRecord{},
		types.NewAccounting(params),
		types.DefaultFeeds(),
		chain.SDRBaseDenom,
		[]types.ExchangeRateAgeOverride{},
	)

	bz, err := json.MarshalIndent(&oracleGenesis.Params, "", " ")
	if err != nil {
		panic(err)
	}
	fmt.Printf("Selected randomly generated oracle parameters:\n%s\n", bz)
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(oracleGenesis)
}
