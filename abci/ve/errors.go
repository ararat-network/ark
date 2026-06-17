package ve

import (
	"fmt"
)

// PreBlockError is an error that is returned when the pre-block simulation fails.
type PreBlockError struct {
	Err error
}

func (e PreBlockError) Error() string {
	return fmt.Sprintf("finalise block error: %s", e.Err.Error())
}

func (e PreBlockError) Label() string {
	return "PreBlockError"
}

// ErrPanic is an error that is returned when a panic occurs in the ABCI handler.
type ErrPanic struct {
	Err error
}

func (e ErrPanic) Error() string {
	return fmt.Sprintf("panic: %s", e.Err.Error())
}

func (e ErrPanic) Label() string {
	return "Panic"
}

// OracleClientError is an error that is returned when the oracle client's response is invalid.
type OracleClientError struct {
	Err error
}

func (e OracleClientError) Error() string {
	return fmt.Sprintf("oracle client error: %s", e.Err.Error())
}

func (e OracleClientError) Label() string {
	return "OracleClientError"
}

// InvalidOraclePricesError is returned when the oracle service returns prices
// that cannot be used to build a valid oracle vote extension.
type InvalidOraclePricesError struct {
	Err error
}

func (e InvalidOraclePricesError) Error() string {
	return fmt.Sprintf("invalid oracle prices: %s", e.Err.Error())
}

func (e InvalidOraclePricesError) Label() string {
	return "InvalidOraclePricesError"
}

// ValidateVoteExtensionError is an error that is returned when there is a failure in validating a vote extension.
type ValidateVoteExtensionError struct {
	Err error
}

func (e ValidateVoteExtensionError) Error() string {
	return fmt.Sprintf("validate vote extension error: %s", e.Err.Error())
}

func (e ValidateVoteExtensionError) Label() string {
	return "ValidateVoteExtensionError"
}
