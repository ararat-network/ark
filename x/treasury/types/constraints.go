package types

import (
	"cosmossdk.io/math"
)

// Clamp constrains a policy variable update within the policy constraints
func (pc PolicyConstraints) Clamp(prevRate, newRate math.LegacyDec) math.LegacyDec {
	delta := newRate.Sub(prevRate)
	if newRate.GT(prevRate) {
		if delta.GT(pc.ChangeRateMax) {
			newRate = prevRate.Add(pc.ChangeRateMax)
		}
	} else {
		if delta.Abs().GT(pc.ChangeRateMax) {
			newRate = prevRate.Sub(pc.ChangeRateMax)
		}
	}

	if newRate.LT(pc.RateMin) {
		newRate = pc.RateMin
	} else if newRate.GT(pc.RateMax) {
		newRate = pc.RateMax
	}

	return newRate
}
