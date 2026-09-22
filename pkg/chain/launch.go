package chain

import "cosmossdk.io/math"

// BootstrapNoahPerUSD is the one-NOAH-per-dollar placeholder shared by sidecar bootstrap and
// Treasury's gas-factor seed. Launch genesis and sidecar configuration supply actual opening
// prices; live observations replace the seed.
const BootstrapNoahPerUSD = "1"

// Validator seat policy: every founding seat holds the same permanently locked grant and the same
// liquid float, both granted from the community pool at assembly, and each adds one seat's share to
// both reward targets. Entry after launch is open and grants nothing; the targets then move only by
// governance policy (D84).
const (
	// SeatGrantNoah is the locked, delegated stake of one seat, in whole NOAH.
	SeatGrantNoah = 5_000_000
	// SeatFloatNoah is the liquid balance beside it, in whole NOAH: locked coins cannot pay fees.
	SeatFloatNoah = 300_000
)

// SeatValidatorShare and SeatOracleShare are one seat's reward targets per block in base units:
// 0.0175 and 0.0075 NOAH, 70/30 of 0.025, which is 131,400 NOAH a year.
var (
	SeatValidatorShare = math.NewInt(17_500_000_000_000_000)
	SeatOracleShare    = math.NewInt(7_500_000_000_000_000)
)
