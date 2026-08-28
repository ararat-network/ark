package simulation

// DONTCOVER

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/types/module"

	chain "github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/oracle/types"
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

// GenExchangeRates prices every launch feed as of the genesis timestamp.
//
// The simulation has to seed these because it never runs the vote-extension
// pipeline that produces them on a live chain. Without a rate the protocol
// reference denomination is unpriced, so every Market conversion the simulation
// generates fails on an unknown denom and Market goes effectively untested.
func GenExchangeRates(r *rand.Rand, genTime time.Time) []types.ExchangeRate {
	rates := make([]types.ExchangeRate, 0, len(types.DefaultFeedDenoms))
	for _, denom := range types.DefaultFeedDenoms {
		rates = append(rates, types.ExchangeRate{
			Denom: denom,
			// Positive and spread over four orders of magnitude, so conversion
			// arithmetic meets realistically dissimilar rates rather than a set
			// clustered around one.
			Rate:           math.LegacyNewDecWithPrec(int64(r.Intn(1_000_000)+1), 3),
			BlockTimestamp: genTime,
			BlockHeight:    0,
		})
	}
	return rates
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
		VoteThreshold:            voteThreshold,
		RewardBand:               rewardBand,
		RewardWindow:             rewardWindow,
		RewardDistributionWindow: rewardDistributionWindow,
		AttendanceWindow:         attendanceWindow,
		MinAttendancePerWindow:   minAttendancePerWindow,
		// Nothing refreshes rates during a simulation, so the one-minute default
		// would expire them a few blocks in and fail every conversion after
		// that. The ceiling keeps the seeded rates readable for the whole run;
		// the staleness path itself is covered by keeper tests.
		MaxExchangeRateAge:        types.MaxAllowedExchangeRateAge,
		FunctioningBlockThreshold: functioningBlockThreshold,
		ParticipationThreshold:    participationThreshold,
	}
	oracleGenesis := types.NewGenesisState(
		params,
		GenExchangeRates(simState.Rand, simState.GenTimestamp),
		[]types.RewardWeight{},
		[]types.AttendanceRecord{},
		types.NewAccounting(params),
		types.DefaultFeeds(),
		chain.SDRBaseDenom,
	)

	bz, err := json.MarshalIndent(&oracleGenesis.Params, "", " ")
	if err != nil {
		panic(err)
	}
	fmt.Printf("Selected randomly generated oracle parameters:\n%s\n", bz)
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(oracleGenesis)
}
