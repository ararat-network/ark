package types

import "cosmossdk.io/math"

// ExpansionAllocation is the execution-local result of routing one realised
// NOAH-to-stable expansion. All amounts are implicit micro-NOAH.
type ExpansionAllocation struct {
	EligiblePrincipalNoah   math.Int
	RedemptionBufferCredit  math.Int
	StrategicReserveCredit  math.Int
	InsuranceCredit         math.Int
	SpreadAndDustBurn       math.Int
	OverflowBurn            math.Int
	TargetValuationComplete bool
}

// TotalBurn returns the sum of the separately audited burn causes.
func (a ExpansionAllocation) TotalBurn() math.Int {
	return a.SpreadAndDustBurn.Add(a.OverflowBurn)
}

// BufferDraw is the execution-local result of one proportional redemption
// buffer calculation. NOAH valuations remain decimal until the final payment.
type BufferDraw struct {
	AggregateLiabilityNoah math.LegacyDec
	RedeemedLiabilityNoah  math.LegacyDec
	BufferPaid             math.Int
	ValuationComplete      bool
}
