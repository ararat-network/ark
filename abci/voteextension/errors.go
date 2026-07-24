package voteextension

import "errors"

var (
	// errPanic identifies a panic recovered from an ABCI vote-extension handler.
	errPanic = errors.New("panic")
	// errOracleClient identifies an invalid response from the oracle client.
	errOracleClient = errors.New("oracle client error")
	// errInvalidOraclePrices identifies prices that cannot form a valid vote extension.
	errInvalidOraclePrices = errors.New("invalid oracle prices")
	// errVoteExtensionValidation identifies a vote extension that fails validation.
	errVoteExtensionValidation = errors.New("vote extension validation failed")
)
