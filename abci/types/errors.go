package types

import "errors"

var (
	// ErrNilRequest is returned when an ABCI handler receives a nil request.
	ErrNilRequest = errors.New("nil request")
	// ErrWrappedHandler identifies a failure returned by a wrapped SDK handler.
	ErrWrappedHandler = errors.New("wrapped handler failed")
	// ErrCodec identifies an ABCI protocol encoding or decoding failure.
	ErrCodec = errors.New("codec error")
	// ErrMissingCommitInfo is returned when a proposal omits the previous height's commit information.
	ErrMissingCommitInfo = errors.New("missing commit info")
	// ErrOracleKeeper identifies an oracle keeper state access or mutation failure.
	ErrOracleKeeper = errors.New("oracle keeper error")
	// ErrTreasuryKeeper identifies a treasury keeper state access or mutation failure.
	ErrTreasuryKeeper = errors.New("treasury keeper error")
	// ErrAssetKeeper identifies an asset keeper state access or mutation failure.
	ErrAssetKeeper = errors.New("asset keeper error")
)
