package voteextension

import abcimetrics "github.com/ararat-network/ark/abci/metrics"

var (
	// errPanic identifies a panic recovered from an ABCI vote-extension handler.
	errPanic = abcimetrics.NewError(abcimetrics.StatusPanic, "panic")
	// errPriceFeedClient identifies an invalid response from the oracle client.
	errPriceFeedClient = abcimetrics.NewError(abcimetrics.StatusPriceFeedClient, "oracle client error")
	// errInvalidPrices identifies prices that cannot form a valid vote extension.
	errInvalidPrices = abcimetrics.NewError(abcimetrics.StatusInvalidPrices, "invalid oracle prices")
	// errVoteExtensionValidation identifies a vote extension that fails validation.
	errVoteExtensionValidation = abcimetrics.NewError(abcimetrics.StatusVoteExtensionValidation, "vote extension validation failed")
)
