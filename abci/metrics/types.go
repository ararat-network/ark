package metrics

import "errors"

const notImplemented = "not_implemented"

type Labeller interface {
	Label() string
}

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
