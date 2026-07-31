package types

const (
	// MaxFeeds bounds the number of price feeds accepted by the oracle module
	// and represented in a validator's vote extension.
	MaxFeeds = 256

	// MaxEncodedVoteRateBytes bounds one vote-extension rate encoding.
	// LegacyDec marshals the raw integer price*10^18 as decimal text, so 40
	// bytes admits every price below 10^22 while denying the 97-digit range
	// headroom that state encodings must keep. Tiny prices encode shorter, so
	// the bound needs no floor. Vote-extension capacity limits derive from
	// this bound, so changing it changes consensus acceptance.
	MaxEncodedVoteRateBytes = 40

	// InitialFeedVersion identifies the genesis feed epoch.
	InitialFeedVersion uint64 = 1

	// FeedActivationDelayBlocks leaves one fully committed height between
	// scheduling a feed transition and using it in ExtendVote.
	FeedActivationDelayBlocks int64 = 2
)
