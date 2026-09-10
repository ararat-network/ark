package types

import (
	"math/big"

	"cosmossdk.io/math"
)

const (
	// MaxFeeds bounds the number of price feeds accepted by the oracle module
	// and represented in a validator's vote extension.
	MaxFeeds = 256

	// MaxEncodedVoteRateBytes caps minimal big-endian price*10^18 magnitudes at 16 bytes, admitting
	// values below 2^128. Vote-extension capacity derives from this consensus bound.
	MaxEncodedVoteRateBytes = 16

	// InitialFeedVersion identifies the genesis feed epoch.
	InitialFeedVersion uint64 = 1

	// FeedActivationDelayBlocks leaves one fully committed height between
	// scheduling a feed transition and using it in ExtendVote.
	FeedActivationDelayBlocks int64 = 2
)

// MaxExchangeRate bounds stored and derived NOAH-per-unit prices to the vote encoding range.
// Multiplying a 2^128-capped quantity by this rate stays well inside LegacyDec range; see
// x/oracle/README.md, "Rate orientation: NOAH per unit".
var MaxExchangeRate = math.LegacyNewDecFromBigIntWithPrec(
	new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 8*MaxEncodedVoteRateBytes), big.NewInt(1)),
	math.LegacyPrecision,
)
