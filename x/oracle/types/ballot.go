package types

import (
	context "context"
	"fmt"
	stdMath "math"
	"sort"
	"strconv"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// NOTE: we don't need to implement proto interface on this file
// these are not used in store or rpc response

// VoteForTally is a convenience wrapper to reduce redundant lookup cost
type VoteForTally struct {
	Denom        string
	ExchangeRate math.LegacyDec
	Voter        sdk.ValAddress
	Power        int64
}

// NewVoteForTally returns a new VoteForTally instance
func NewVoteForTally(rate math.LegacyDec, denom string, voter sdk.ValAddress, power int64) VoteForTally {
	return VoteForTally{
		ExchangeRate: rate,
		Denom:        denom,
		Voter:        voter,
		Power:        power,
	}
}

// ExchangeRateBallot is a convenience wrapper around a ExchangeRateVote slice
type ExchangeRateBallot []VoteForTally

// ToMap return organized exchange rate map by validator
func (pb ExchangeRateBallot) ToMap() map[string]math.LegacyDec {
	exchangeRateMap := make(map[string]math.LegacyDec)
	for _, vote := range pb {
		if vote.ExchangeRate.IsPositive() {
			exchangeRateMap[string(vote.Voter)] = vote.ExchangeRate
		}
	}

	return exchangeRateMap
}

// ToCrossRate return cross_rate(base/exchange_rate) ballot
func (pb ExchangeRateBallot) ToCrossRate(bases map[string]math.LegacyDec) (cb ExchangeRateBallot) {
	for i := range pb {
		vote := pb[i]

		if exchangeRateRT, ok := bases[string(vote.Voter)]; ok && vote.ExchangeRate.IsPositive() {
			vote.ExchangeRate = exchangeRateRT.Quo(vote.ExchangeRate)
		} else {
			// If we can't get reference noah exchange rate, we just convert the vote as abstain vote
			vote.ExchangeRate = math.LegacyZeroDec()
			vote.Power = 0
		}

		cb = append(cb, vote)
	}

	return cb
}

// ToCrossRateWithSort return cross_rate(base/exchange_rate) ballot
func (pb ExchangeRateBallot) ToCrossRateWithSort(bases map[string]math.LegacyDec) (cb ExchangeRateBallot) {
	for i := range pb {
		vote := pb[i]

		if exchangeRateRT, ok := bases[string(vote.Voter)]; ok && vote.ExchangeRate.IsPositive() {
			vote.ExchangeRate = exchangeRateRT.Quo(vote.ExchangeRate)
		} else {
			// If we can't get reference noah exchange rate, we just convert the vote as abstain vote
			vote.ExchangeRate = math.LegacyZeroDec()
			vote.Power = 0
		}

		cb = append(cb, vote)
	}

	sort.Sort(cb)
	return cb
}

// Power returns the total amount of voting power in the ballot
func (pb ExchangeRateBallot) Power() int64 {
	totalPower := int64(0)
	for _, vote := range pb {
		totalPower += vote.Power
	}

	return totalPower
}

// WeightedMedian returns the median weighted by the power of the ExchangeRateVote.
// CONTRACT: ballot must be sorted
func (pb ExchangeRateBallot) WeightedMedian() math.LegacyDec {
	totalPower := pb.Power()
	if pb.Len() > 0 {
		pivot := int64(0)
		for _, v := range pb {
			votePower := v.Power

			pivot += votePower
			if pivot >= (totalPower / 2) {
				return v.ExchangeRate
			}
		}
	}
	return math.LegacyZeroDec()
}

// WeightedMedianWithAssertion returns the median weighted by the power of the ExchangeRateVote.
// CONTRACT: ballot must be sorted
func (pb ExchangeRateBallot) WeightedMedianWithAssertion() math.LegacyDec {
	if !sort.IsSorted(pb) {
		panic("ballot must be sorted")
	}

	totalPower := pb.Power()
	if pb.Len() > 0 {
		pivot := int64(0)
		for _, v := range pb {
			votePower := v.Power

			pivot += votePower
			if pivot >= (totalPower / 2) {
				return v.ExchangeRate
			}
		}
	}
	return math.LegacyZeroDec()
}

// StandardDeviation returns the standard deviation by the power of the ExchangeRateVote.
func (pb ExchangeRateBallot) StandardDeviation(median math.LegacyDec) (standardDeviation math.LegacyDec) {
	if len(pb) == 0 {
		return math.LegacyZeroDec()
	}

	defer func() {
		if e := recover(); e != nil {
			standardDeviation = math.LegacyZeroDec()
		}
	}()

	sum := math.LegacyZeroDec()
	for _, v := range pb {
		deviation := v.ExchangeRate.Sub(median)
		sum = sum.Add(deviation.Mul(deviation))
	}

	variance := sum.QuoInt64(int64(len(pb)))

	floatNum, _ := strconv.ParseFloat(variance.String(), 64)
	floatNum = stdMath.Sqrt(floatNum)
	standardDeviation, _ = math.LegacyNewDecFromStr(fmt.Sprintf("%f", floatNum))

	return standardDeviation
}

// Len implements sort.Interface
func (pb ExchangeRateBallot) Len() int {
	return len(pb)
}

// Less reports whether the element with
// index i should sort before the element with index j.
func (pb ExchangeRateBallot) Less(i, j int) bool {
	return pb[i].ExchangeRate.LT(pb[j].ExchangeRate)
}

// Swap implements sort.Interface.
func (pb ExchangeRateBallot) Swap(i, j int) {
	pb[i], pb[j] = pb[j], pb[i]
}

// BallotIsPassing returns the total voting power of the ballot and whether it meets the
// threshold required for the ballot to pass.
func (pb ExchangeRateBallot) BallotIsPassing(thresholdVotes math.Int) (math.Int, bool) {
	ballotPower := math.NewInt(pb.Power())
	return ballotPower, !ballotPower.IsZero() && ballotPower.GTE(thresholdVotes)
}

// Tally calculates the median and returns it. Sets the set of voters to be rewarded, i.e. voted within
// a reasonable spread from the weighted median to the store
func (pb ExchangeRateBallot) Tally(ctx context.Context, rewardBand math.LegacyDec, validatorClaimMap map[string]Claim) (weightedMedian math.LegacyDec) {
	sort.Sort(pb)
	weightedMedian = pb.WeightedMedianWithAssertion()

	standardDeviation := pb.StandardDeviation(weightedMedian)
	rewardSpread := weightedMedian.Mul(rewardBand.QuoInt64(2))

	if standardDeviation.GT(rewardSpread) {
		rewardSpread = standardDeviation
	}

	for _, vote := range pb {
		// Filter ballot winners & abstain voters
		if (vote.ExchangeRate.GTE(weightedMedian.Sub(rewardSpread)) &&
			vote.ExchangeRate.LTE(weightedMedian.Add(rewardSpread))) ||
			!vote.ExchangeRate.IsPositive() {

			key := vote.Voter.String()
			claim := validatorClaimMap[key]
			claim.Weight += vote.Power
			claim.WinCount++
			validatorClaimMap[key] = claim
		}
	}

	return weightedMedian
}

// Claim is an interface that directs its rewards to an attached bank account.
type Claim struct {
	Power     int64
	Weight    int64
	WinCount  int64
	Recipient sdk.ValAddress
}

// NewClaim generates a Claim instance.
func NewClaim(power, weight, winCount int64, recipient sdk.ValAddress) Claim {
	return Claim{
		Power:     power,
		Weight:    weight,
		WinCount:  winCount,
		Recipient: recipient,
	}
}
