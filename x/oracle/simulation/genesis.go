package simulation

// DONTCOVER

import (
	"encoding/json"
	"fmt"
	"math/rand"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/types/module"

	core "noah/types"
	"noah/x/oracle/types"
)

// Simulation parameter constants
const (
	voteThresholdKey            = "vote_threshold"
	rewardBandKey               = "reward_band"
	rewardWindowKey             = "reward_window"
	rewardDistributionWindowKey = "reward_distribution_window"
	slashFractionKey            = "slash_fraction"
	slashWindowKey              = "slash_window"
	minValidPerWindowKey        = "min_valid_per_window"
)

// GenVoteThreshold randomised VoteThreshold
func GenVoteThreshold(r *rand.Rand) math.LegacyDec {
	return math.LegacyNewDecWithPrec(333, 3).Add(math.LegacyNewDecWithPrec(int64(r.Intn(333)), 3))
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

// GenSlashFraction randomised SlashFraction
func GenSlashFraction(r *rand.Rand) math.LegacyDec {
	return math.LegacyZeroDec().Add(math.LegacyNewDecWithPrec(int64(r.Intn(100)), 3))
}

// GenSlashWindow randomised SlashWindow
func GenSlashWindow(r *rand.Rand) uint64 {
	return uint64(100 + r.Intn(100000))
}

// GenMinValidPerWindow randomised MinValidPerWindow
func GenMinValidPerWindow(r *rand.Rand) math.LegacyDec {
	return math.LegacyZeroDec().Add(math.LegacyNewDecWithPrec(int64(r.Intn(500)), 3))
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

	var slashFraction math.LegacyDec
	simState.AppParams.GetOrGenerate(
		slashFractionKey, &slashFraction, simState.Rand,
		func(r *rand.Rand) { slashFraction = GenSlashFraction(r) },
	)

	var slashWindow uint64
	simState.AppParams.GetOrGenerate(
		slashWindowKey, &slashWindow, simState.Rand,
		func(r *rand.Rand) { slashWindow = GenSlashWindow(r) },
	)

	var minValidPerWindow math.LegacyDec
	simState.AppParams.GetOrGenerate(
		minValidPerWindowKey, &minValidPerWindow, simState.Rand,
		func(r *rand.Rand) { minValidPerWindow = GenMinValidPerWindow(r) },
	)

	oracleGenesis := types.NewGenesisState(
		types.Params{
			VoteThreshold:            voteThreshold,
			RewardBand:               rewardBand,
			RewardWindow:             rewardWindow,
			RewardDistributionWindow: rewardDistributionWindow,
			TobinTaxes: types.TobinTaxes{
				{Denom: core.MicroKRWDenom, TobinTax: types.DefaultTobinTax},
				{Denom: core.MicroSDRDenom, TobinTax: types.DefaultTobinTax},
				{Denom: core.MicroUSDDenom, TobinTax: types.DefaultTobinTax},
				{Denom: core.MicroMNTDenom, TobinTax: math.LegacyNewDecWithPrec(2, 2)},
			},
			SlashFraction:     slashFraction,
			SlashWindow:       slashWindow,
			MinValidPerWindow: minValidPerWindow,
		},
		[]types.ExchangeRate{},
		[]types.ScoreWeight{},
		[]types.MissCount{},
		[]types.TobinTax{},
	)

	bz, err := json.MarshalIndent(&oracleGenesis.Params, "", " ")
	if err != nil {
		panic(err)
	}
	fmt.Printf("Selected randomly generated oracle parameters:\n%s\n", bz)
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(oracleGenesis)
}
