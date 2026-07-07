package sidecar

import "errors"

var (
	// ErrNilRequest is returned when the sidecar receives a nil price request.
	ErrNilRequest = errors.New("request cannot be nil")
	// ErrOracleNotRunning is returned when prices are requested before the runtime is running.
	ErrOracleNotRunning = errors.New("oracle is not running")

	errOracleAlreadyStarted = errors.New("oracle already started")
	errOracleClosed         = errors.New("oracle is closed")
)
