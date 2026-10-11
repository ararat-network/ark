package chain

import "cosmossdk.io/math"

// Bootstrap pricing: NOAH opens at one XDR, the neutral unit the protocol references (D128).
// BootstrapUSDPerXDR is the upward-rounded dollar/XDR cross; the sidecar's USD/NOAH bootstrap leg
// and Treasury's gas-factor seed both derive from the pair. Launch genesis and sidecar
// configuration supply actual opening prices; live observations replace the seed.
const (
	BootstrapNoahPerXDR = "1"
	BootstrapUSDPerXDR  = "1.371"
)

// BootstrapNoahPerUSD is the sidecar's USD/NOAH bootstrap leg, NOAH per dollar at one NOAH a XDR:
// 0.729394602479941648 at the 1.371 cross. Until NOAH trades it drifts with XDR/USD.
var BootstrapNoahPerUSD = math.LegacyMustNewDecFromStr(BootstrapNoahPerXDR).
	Quo(math.LegacyMustNewDecFromStr(BootstrapUSDPerXDR)).String()

// Validator seat policy: every seat assembly grants is SeatGrantNoah, a continuous vesting
// account from SeatVestingCliffYears to SeatVestingEndYears after genesis time with the same
// liquid float beside it, both granted from the community pool, and each adding one seat's
// share to both reward targets. The founder's seat alone is larger, FounderSeat, from the same
// pool. The count sets the pool's cost, never the seat. Entry after launch is open and grants
// nothing; the targets then move only by governance policy (D84, D111, D112, D115).
const (
	// SeatGrantNoah is one founding seat's grant in whole NOAH, the same at any count.
	SeatGrantNoah = 5_000_000
	// MinLaunchSeats and MaxLaunchSeats bound the count launch assembly accepts. Under seven,
	// genesis bonded stake falls under the 40M a buyer of the first year's liquid NOAH could
	// otherwise take a third against; over thirteen, the founder's seat passes 3% of supply.
	// Testnets seat any count the community pool funds (D112, D115).
	MinLaunchSeats = 7
	MaxLaunchSeats = 13
	// SeatFloatNoah is the liquid balance beside each seat, in whole NOAH: unvested coins cannot
	// pay fees, and the float carries the week before seat income starts (D96).
	SeatFloatNoah = 50_000
	// SeatVestingCliffYears and SeatVestingEndYears are the seat's vesting window in calendar
	// years after genesis time: nothing vests before the cliff, all of it by the end. The cliff
	// covers the launch window; the tail finishes about when public bonded stake passes the
	// seats, so the founding stake is fully for sale only once the public outweighs it (D114).
	SeatVestingCliffYears = 4
	SeatVestingEndYears   = 8
)

// SeatValidatorShare and SeatOracleShare are one seat's reward targets per block in base units:
// 0.0175 and 0.0075 NOAH, 70/30 of 0.025, which is 131,400 NOAH a year.
var (
	SeatValidatorShare = math.NewInt(17_500_000_000_000_000)
	SeatOracleShare    = math.NewInt(7_500_000_000_000_000)
)

// SeatGrant is one seat's grant in base units.
var SeatGrant = NativeBaseAmount(SeatGrantNoah)

// FounderSeat is the founder's seat in base units at a seat count: half the other seats' sum,
// a third of genesis bonded stake at any count, and never under one seat, so at three seats
// or fewer it is the seat grant (D112).
func FounderSeat(count int) math.Int {
	if count < 1 {
		return SeatGrant
	}
	founder := SeatGrant.MulRaw(int64(count - 1)).QuoRaw(2)
	if founder.LT(SeatGrant) {
		return SeatGrant
	}
	return founder
}

// The founder's awards, which seat assembly writes as governance grants from the contributor
// pool rather than leaving to a vote (D136): the grant for the work before launch, at the
// ownership cap on the ownership schedule, and the compensation stream for the work after it,
// FounderStreamPeriods compensation periods valued at the bootstrap price (D115, D129).
const (
	FounderGrantNoah     = 15_000_000
	FounderStreamNoah    = 10_000_000
	FounderStreamPeriods = 260

	FounderGrantReference  = "Founder's grant for the work before launch, written at genesis: DISBURSEMENT_PLAN.md §4 (D115, D136)"
	FounderStreamReference = "Founder's compensation stream for the work after launch, written at genesis: DISBURSEMENT_PLAN.md §4 (D129, D136)"
)
