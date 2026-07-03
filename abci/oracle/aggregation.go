package oracle

import (
	"sort"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// denomVote is a convenience wrapper to reduce redundant lookup cost.
type denomVote struct {
	Denom        string
	ExchangeRate math.LegacyDec
	Voter        sdk.ConsAddress
	power        uint64
}

// newDenomVote returns a new denomVote.
func newDenomVote(rate math.LegacyDec, denom string, voter sdk.ConsAddress, power uint64) denomVote {
	return denomVote{
		ExchangeRate: rate,
		Denom:        denom,
		Voter:        voter,
		power:        power,
	}
}

// denomVotes is a convenience wrapper around a denomVote slice.
type denomVotes []denomVote

// Len implements sort.Interface.
func (dv denomVotes) Len() int {
	return len(dv)
}

// Less reports whether the element with index i should sort before the element with index j.
func (dv denomVotes) Less(i, j int) bool {
	return dv[i].ExchangeRate.LT(dv[j].ExchangeRate)
}

// Swap implements sort.Interface.
func (dv denomVotes) Swap(i, j int) {
	dv[i], dv[j] = dv[j], dv[i]
}

// validatorMap returns a map of validators to respective positive exchange rate votes.
func (dv denomVotes) validatorMap() map[string]math.LegacyDec {
	validatorMap := make(map[string]math.LegacyDec)
	for _, vote := range dv {
		if vote.ExchangeRate.IsPositive() {
			validatorMap[string(vote.Voter)] = vote.ExchangeRate
		}
	}

	return validatorMap
}

// crossRate returns cross-rate ballots as referenceRate/exchangeRate.
func (dv denomVotes) crossRate(referenceRates map[string]math.LegacyDec) (cb denomVotes) {
	for i := range dv {
		vote := dv[i]

		if referenceRate, ok := referenceRates[string(vote.Voter)]; ok && vote.ExchangeRate.IsPositive() {
			vote.ExchangeRate = referenceRate.Quo(vote.ExchangeRate)
		} else {
			// If there is no reference exchange rate, convert the vote to an abstain vote.
			vote.ExchangeRate = math.LegacyZeroDec()
			vote.power = 0
		}

		cb = append(cb, vote)
	}

	return cb
}

// overlapPower returns the voting power that can be converted through the
// provided reference rates.
func (dv denomVotes) overlapPower(referenceRates map[string]math.LegacyDec) uint64 {
	totalPower := uint64(0)
	for _, vote := range dv {
		if !vote.ExchangeRate.IsPositive() {
			continue
		}
		if _, ok := referenceRates[string(vote.Voter)]; ok {
			totalPower += vote.power
		}
	}

	return totalPower
}

// power returns the total amount of voting power in the denom votes.
func (dv denomVotes) power() uint64 {
	totalPower := uint64(0)
	for _, vote := range dv {
		totalPower += vote.power
	}

	return totalPower
}

// weightedMedian returns the median weighted by vote power.
func (dv denomVotes) weightedMedian() math.LegacyDec {
	sort.Sort(dv)

	totalPower := dv.power()
	if dv.Len() > 0 {
		pivot := uint64(0)
		for _, v := range dv {
			votePower := v.power

			pivot += votePower
			if pivot >= (totalPower / 2) {
				return v.ExchangeRate
			}
		}
	}
	return math.LegacyZeroDec()
}

// standardDeviation returns the unweighted standard deviation of the votes.
func (dv denomVotes) standardDeviation(median math.LegacyDec) (standardDeviation math.LegacyDec) {
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

// validatorScore directs oracle rewards and miss accounting to an attached validator.
type validatorScore struct {
	Weight    uint64
	WinCount  uint64
	Recipient sdk.ConsAddress
}

// newValidatorScore returns a validator score.
func newValidatorScore(weight, winCount uint64, recipient sdk.ConsAddress) validatorScore {
	return validatorScore{
		Weight:    weight,
		WinCount:  winCount,
		Recipient: recipient,
	}
}
