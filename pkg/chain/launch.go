package chain

import "cosmossdk.io/math"

// BootstrapNoahPerUSD is the one-NOAH-per-dollar placeholder shared by sidecar bootstrap and
// Treasury's gas-factor seed. Launch genesis and sidecar configuration supply actual opening
// prices; live observations replace the seed.
const BootstrapNoahPerUSD = "1"

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
	// Testnets seat any count (D112, D115).
	MinLaunchSeats = 7
	MaxLaunchSeats = 13
	// SeatFloatNoah is the liquid balance beside each seat, in whole NOAH: unvested coins cannot
	// pay fees, and the float carries the week before seat income starts (D95).
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
