package types

import (
	"sort"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DenomVote is a convenience wrapper to reduce redundant lookup cost
type DenomVote struct {
	Denom        string
	ExchangeRate math.LegacyDec
	Voter        sdk.ValAddress
	Power        uint64
}

// NewDenomVote returns a new VoteForTally instance
func NewDenomVote(rate math.LegacyDec, denom string, voter sdk.ValAddress, power uint64) DenomVote {
	return DenomVote{
		ExchangeRate: rate,
		Denom:        denom,
		Voter:        voter,
		Power:        power,
	}
}

// DenomVotes is a convenience wrapper around a Vote slice
type DenomVotes []DenomVote

// Len implements sort.Interface
func (dv DenomVotes) Len() int {
	return len(dv)
}

// Less reports whether the element with index i should sort before the element with index j
func (dv DenomVotes) Less(i, j int) bool {
	return dv[i].ExchangeRate.LT(dv[j].ExchangeRate)
}

// Swap implements sort.Interface.
func (dv DenomVotes) Swap(i, j int) {
	dv[i], dv[j] = dv[j], dv[i]
}

// ValidatorMap returns a map of validators to respective exchange rate votes
func (dv DenomVotes) ValidatorMap() map[string]math.LegacyDec {
	validatorMap := make(map[string]math.LegacyDec)
	for _, vote := range dv {
		if vote.ExchangeRate.IsPositive() {
			validatorMap[string(vote.Voter)] = vote.ExchangeRate
		}
	}

	return validatorMap
}

// CrossRate returns cross_rate (referenceRates/exchangeRate) ballot
func (dv DenomVotes) CrossRate(referenceRates map[string]math.LegacyDec) (cb DenomVotes) {
	for i := range dv {
		vote := dv[i]

		if referenceRate, ok := referenceRates[string(vote.Voter)]; ok && vote.ExchangeRate.IsPositive() {
			vote.ExchangeRate = referenceRate.Quo(vote.ExchangeRate)
		} else {
			// If we can't get reference noah exchange rate, we just convert the vote as abstain vote
			vote.ExchangeRate = math.LegacyZeroDec()
			vote.Power = 0
		}

		cb = append(cb, vote)
	}

	return cb
}

// Power returns the total amount of voting power in the denom votes
func (dv DenomVotes) Power() uint64 {
	totalPower := uint64(0)
	for _, vote := range dv {
		totalPower += vote.Power
	}

	return totalPower
}

// WeightedMedian returns the median weighted by the power of the Vote
func (dv DenomVotes) WeightedMedian() math.LegacyDec {
	sort.Sort(dv)

	totalPower := dv.Power()
	if dv.Len() > 0 {
		pivot := uint64(0)
		for _, v := range dv {
			votePower := v.Power

			pivot += votePower
			if pivot >= (totalPower / 2) {
				return v.ExchangeRate
			}
		}
	}
	return math.LegacyZeroDec()
}

// StandardDeviation returns the standard deviation by the power of the votes
func (dv DenomVotes) StandardDeviation(median math.LegacyDec) (standardDeviation math.LegacyDec) {
	if len(dv) == 0 {
		return math.LegacyZeroDec()
	}

	defer func() {
		if e := recover(); e != nil {
			standardDeviation = math.LegacyZeroDec()
		}
	}()

	sum := math.LegacyZeroDec()
	for _, v := range dv {
		deviation := v.ExchangeRate.Sub(median)
		sum = sum.Add(deviation.Mul(deviation))
	}

	variance := sum.QuoInt64(int64(len(dv)))
	standardDeviation, err := variance.ApproxSqrt()
	if err != nil {
		return math.LegacyZeroDec()
	}

	return standardDeviation
}
