package types

import (
	"math/big"

	"cosmossdk.io/math"
)

const (
	// MaxFeeds bounds the number of price feeds accepted by the oracle module
	// and represented in a validator's vote extension.
	MaxFeeds = 256

	// MaxEncodedVoteRateBytes bounds one vote-extension rate encoding: the
	// minimal big-endian bytes of the raw price*10^18 magnitude. Sixteen
	// bytes admits every raw value below 2^128 — prices to ~3.4*10^20 —
	// matching the chain's other 2^128 domain caps while denying the range
	// headroom the full LegacyDec encoding keeps. Vote-extension capacity
	// limits derive from this bound, so changing it changes consensus
	// acceptance.
	MaxEncodedVoteRateBytes = 16

	// InitialFeedVersion identifies the genesis feed epoch.
	InitialFeedVersion uint64 = 1

	// FeedActivationDelayBlocks leaves one fully committed height between
	// scheduling a feed transition and using it in ExtendVote.
	FeedActivationDelayBlocks int64 = 2
)

// MaxExchangeRate is the largest rate the store holds, NOAH per one unit: the
// magnitude MaxEncodedVoteRateBytes admits a direct report, held against
// derived prices at the tally as well. Rates are what the halt-class folds
// multiply a 2^128-capped quantity by — Reserve recognition, Treasury
// liability — and a quantity at that cap times a rate at this one is ~10^59,
// sixteen orders inside LegacyDec's ~10^77 ceiling, so those products stay
// representable by construction rather than by the shape of a vote.
var MaxExchangeRate = math.LegacyNewDecFromBigIntWithPrec(
	new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 8*MaxEncodedVoteRateBytes), big.NewInt(1)),
	math.LegacyPrecision,
)
