package types

const (
	// MaxVoteTargets bounds the number of exchange-rate targets accepted by the
	// oracle module and represented in a validator's vote extension.
	MaxVoteTargets = 256

	// InitialVoteTargetVersion identifies the genesis vote-target epoch.
	InitialVoteTargetVersion uint64 = 1

	// VoteTargetActivationDelayBlocks leaves one fully committed height between
	// scheduling a target update and using it in ExtendVote.
	VoteTargetActivationDelayBlocks int64 = 2
)
