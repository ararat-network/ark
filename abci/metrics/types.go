package metrics

const notImplemented = "not_implemented"

// Status is a stable, low-cardinality ABCI request outcome used by metrics.
type Status string

const (
	StatusSuccess                  Status = "Success"
	StatusFailure                  Status = "Failure"
	StatusNilRequest               Status = "NilRequestError"
	StatusWrappedHandler           Status = "WrappedHandlerError"
	StatusCodec                    Status = "CodecError"
	StatusMissingCommitInfo        Status = "MissingCommitInfoError"
	StatusExtendedCommitValidation Status = "ExtendedCommitValidationError"
	StatusPanic                    Status = "Panic"
	StatusOracleClient             Status = "OracleClientError"
	StatusInvalidOraclePrices      Status = "InvalidOraclePricesError"
	StatusVoteExtensionValidation  Status = "VoteExtensionValidationError"
	StatusOracleKeeper             Status = "OracleKeeperError"
	StatusAssetKeeper              Status = "AssetKeeperError"
	StatusTreasuryKeeper           Status = "TreasuryKeeperError"
)

func (s Status) String() string {
	if s == "" {
		return string(StatusFailure)
	}
	return string(s)
}

type MessageType int

const (
	ExtendedCommit MessageType = iota
	VoteExtension
)

func (m MessageType) String() string {
	switch m {
	case ExtendedCommit:
		return "extended_commit"
	case VoteExtension:
		return "vote_extension"
	default:
		return notImplemented
	}
}

type Method int

const (
	PrepareProposal Method = iota
	ProcessProposal
	ExtendVote
	VerifyVoteExtension
	PreBlock
)

func (m Method) String() string {
	switch m {
	case PrepareProposal:
		return "prepare_proposal"
	case ProcessProposal:
		return "process_proposal"
	case ExtendVote:
		return "extend_vote"
	case VerifyVoteExtension:
		return "verify_vote_extension"
	case PreBlock:
		return "pre_blocker"
	default:
		return notImplemented
	}
}
