package metrics

import "errors"

const notImplemented = "not_implemented"

// Labeller is implemented by errors and statuses that can label metrics.
type Labeller interface {
	Label() string
}

// StatusFromError returns a label for metrics based on whether an error occurred.
func StatusFromError(err error) Labeller {
	if err == nil {
		return Success{}
	}

	var labeller Labeller
	if errors.As(err, &labeller) {
		return labeller
	}

	return Failure{}
}

type Success struct{}

func (Success) Label() string {
	return "Success"
}

type Failure struct{}

func (Failure) Label() string {
	return "Failure"
}

// MessageType identifies the type of oracle data transmitted between validators.
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

// ABCIMethod identifies the ABCI method being measured.
type ABCIMethod int

const (
	PrepareProposal ABCIMethod = iota
	ProcessProposal
	ExtendVote
	VerifyVoteExtension
	PreBlock
	EndBlock
)

func (m ABCIMethod) String() string {
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
	case EndBlock:
		return "end_blocker"
	default:
		return notImplemented
	}
}

// ReportStatus identifies the report status for a validator and ticker.
type ReportStatus int

const (
	Absent ReportStatus = iota
	MissingPrice
	WithPrice
)

func (rs ReportStatus) String() string {
	switch rs {
	case Absent:
		return "absent"
	case MissingPrice:
		return "missing_price"
	case WithPrice:
		return "with_price"
	default:
		return notImplemented
	}
}
