package types

import abcimetrics "github.com/ararat-network/ark/abci/metrics"

// Boundary errors shared by the hooks. Each carries the status it records under.
var (
	// ErrNilRequest is returned when an ABCI handler receives a nil request.
	ErrNilRequest = abcimetrics.NewError(abcimetrics.StatusNilRequest, "nil request")
	// ErrWrappedHandler identifies a failure returned by a wrapped SDK handler.
	ErrWrappedHandler = abcimetrics.NewError(abcimetrics.StatusWrappedHandler, "wrapped handler failed")
	// ErrCodec identifies an ABCI protocol encoding or decoding failure.
	ErrCodec = abcimetrics.NewError(abcimetrics.StatusCodec, "codec error")
	// ErrMissingCommitInfo is returned when a proposal omits the previous height's commit information.
	ErrMissingCommitInfo = abcimetrics.NewError(abcimetrics.StatusMissingCommitInfo, "missing commit info")
	// ErrOracleKeeper identifies an oracle keeper state access or mutation failure.
	ErrOracleKeeper = abcimetrics.NewError(abcimetrics.StatusOracleKeeper, "oracle keeper error")
)
