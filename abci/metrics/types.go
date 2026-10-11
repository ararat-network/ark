// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, service/metrics/types.go.
// Modified for Ark: ABCI method and outcome labels.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package metrics

import "errors"

const notImplemented = "not_implemented"

// Status is a stable, low-cardinality ABCI request outcome used by metrics.
type Status string

const (
	StatusSuccess                  Status = "Success"
	StatusRejected                 Status = "Rejected"
	StatusFailure                  Status = "Failure"
	StatusNilRequest               Status = "NilRequestError"
	StatusWrappedHandler           Status = "WrappedHandlerError"
	StatusCodec                    Status = "CodecError"
	StatusMissingCommitInfo        Status = "MissingCommitInfoError"
	StatusExtendedCommitValidation Status = "ExtendedCommitValidationError"
	StatusPanic                    Status = "Panic"
	StatusPriceFeedClient          Status = "PriceFeedClientError"
	StatusInvalidPrices            Status = "InvalidPricesError"
	StatusVoteExtensionValidation  Status = "VoteExtensionValidationError"
	StatusOracleKeeper             Status = "OracleKeeperError"
	StatusCommitOnly               Status = "CommitOnly"
)

func (s Status) String() string {
	if s == "" {
		return string(StatusFailure)
	}
	return string(s)
}

// Error is a boundary failure that carries the status it records under, so the label travels with
// the error through every wrap and no handler keeps a table.
type Error struct {
	status Status
	text   string
}

// NewError makes a sentinel that records under status.
func NewError(status Status, text string) *Error {
	return &Error{status: status, text: text}
}

func (e *Error) Error() string { return e.text }

// StatusOf is the status a handler outcome records under: success for no error, the status of the
// outermost labelled error in the chain, failure for anything else.
func StatusOf(err error) Status {
	if err == nil {
		return StatusSuccess
	}
	var labelled *Error
	if errors.As(err, &labelled) {
		return labelled.status
	}
	return StatusFailure
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
