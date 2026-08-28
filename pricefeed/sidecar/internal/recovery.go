package internal

import (
	"errors"
	"fmt"
)

// PanicError marks a recovered panic so an owning lifecycle can coordinate
// sibling cleanup and return the original failure as its cancellation cause.
type PanicError struct {
	// Component identifies the lifecycle or RPC boundary that recovered the panic.
	Component string
	// Recovered is the original value supplied to panic.
	Recovered any
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("%s panicked: %v", e.Component, e.Recovered)
}

// IsPanic reports whether err came from a recovered panic.
func IsPanic(err error) bool {
	var panicErr *PanicError
	return errors.As(err, &panicErr)
}

// RunRecovering runs fn and converts a panic in the current goroutine into an
// error. Goroutines started by fn must install their own recovery boundary.
func RunRecovering(component string, fn func() error) (err error) {
	defer func() {
		recErr := recover()
		if recErr == nil {
			return
		}

		err = panicError(component, recErr)
	}()

	return fn()
}

// HandlePanic converts a panic in the current goroutine and passes it to handler.
// It is intended for deferred use at RPC and worker boundaries.
func HandlePanic(component string, handler func(error)) {
	recErr := recover()
	if recErr == nil {
		return
	}

	handler(panicError(component, recErr))
}

func panicError(component string, recovered any) error {
	return &PanicError{Component: component, Recovered: recovered}
}
